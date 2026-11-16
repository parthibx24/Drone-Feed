package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeSpeaker is the upstream speaker API (dji-bridge's, in production).
type fakeSpeaker struct {
	srv *httptest.Server

	mu    sync.Mutex
	calls []string // "METHOD path auth body"
	audio []byte   // live frames received
	ended bool     // live {"type":"end"} received
}

func newFakeSpeaker(t *testing.T) *fakeSpeaker {
	f := &fakeSpeaker{}
	mux := http.NewServeMux()
	record := func(r *http.Request) string {
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls = append(f.calls, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization")+" "+string(b))
		return string(b)
	}
	const base = "/api/v1/aircraft/SN1/speaker"
	mux.HandleFunc("GET "+base, func(w http.ResponseWriter, r *http.Request) {
		record(r)
		writeJSON(w, 200, map[string]any{"present": true, "status": "idle", "volume": 40, "languages": []string{"en", "bn"}, "can_play": true})
	})
	mux.HandleFunc("POST "+base+"/say", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(record(r), "refuse me") {
			writeJSON(w, 409, map[string]any{"ok": false, "error": "The pilot hasn't allowed StarWatch control", "result": "refused"})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("POST "+base+"/stop", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		writeJSON(w, 503, map[string]any{"ok": false, "error": "pilot app not connected"})
	})
	mux.HandleFunc("POST "+base+"/volume", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		writeJSON(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("GET "+base+"/live", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		if r.Header.Get("Authorization") != "Bearer bridge-token" {
			writeJSON(w, 401, map[string]any{"error": "bad token"})
			return
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		for {
			typ, data, err := c.Read(r.Context())
			if err != nil {
				return
			}
			f.mu.Lock()
			if typ == websocket.MessageBinary {
				f.audio = append(f.audio, data...)
			} else if strings.Contains(string(data), `"end"`) {
				f.ended = true
			}
			n := len(f.audio)
			f.mu.Unlock()
			if typ == websocket.MessageText {
				b, _ := json.Marshal(map[string]any{"type": "result", "outcome": "done", "reason": "", "bytes": n})
				_ = c.Write(r.Context(), websocket.MessageText, b)
				c.Close(websocket.StatusNormalClosure, "")
				return
			}
		}
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeSpeaker) callLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func speakerGateway(t *testing.T, f *fakeSpeaker) *httptest.Server {
	_, srv := testGateway(t, func(c *Config) {
		c.Drones = append(c.Drones, DroneConfig{
			ID: "m3t", MAVLink: "udp:127.0.0.1:9",
			Speaker: &SpeakerConfig{API: f.srv.URL + "/api/v1/aircraft/SN1/speaker/", Token: "bridge-token"},
		})
	})
	return srv
}

func login(t *testing.T, base, user, pass string) *client {
	c := newClient(t, base)
	if code, _, _ := c.do("POST", "/api/auth/login", `{"username":"`+user+`","password":"`+pass+`"}`, nil); code != 200 {
		t.Fatalf("login %s: %d", user, code)
	}
	return c
}

func TestSpeakerConfig(t *testing.T) {
	for name, body := range map[string]string{
		"no token":  "drones: [{id: a, mavlink: 'udp:h:1', speaker: {api: 'http://b:8090/x'}}]\n",
		"env unset": "drones: [{id: a, mavlink: 'udp:h:1', speaker: {api: 'http://b:8090/x', token: '${NOT_SET_ANYWHERE}'}}]\n",
		"bad url":   "drones: [{id: a, mavlink: 'udp:h:1', speaker: {api: 'b:8090/x', token: t}}]\n",
		"ftp":       "drones: [{id: a, mavlink: 'udp:h:1', speaker: {api: 'ftp://b/x', token: t}}]\n",
	} {
		if _, err := loadYAML(t, body); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	t.Setenv("GW_TEST_BRIDGE", "bridge-token")
	cfg, err := loadYAML(t, "drones: [{id: a, mavlink: 'udp:h:1', speaker: {api: 'http://b:8090/x/', token: '${GW_TEST_BRIDGE}'}}]\n")
	if err != nil {
		t.Fatal(err)
	}
	if sp := cfg.Drones[0].Speaker; sp.API != "http://b:8090/x" || sp.Token != "bridge-token" {
		t.Fatalf("speaker config: %+v", sp)
	}
}

func TestSpeakerProxy(t *testing.T) {
	f := newFakeSpeaker(t)
	srv := speakerGateway(t, f)
	op := login(t, srv.URL, "pilot", "operator-pass")
	view := login(t, srv.URL, "watcher", "viewer-pass")

	// A drone without a speaker: 404, so the dashboard hides the tab.
	if code, body, _ := view.do("GET", "/api/v1/drones/kotowali/speaker", "", nil); code != 404 || body["error"] != "no speaker" {
		t.Fatalf("no speaker: %d %v", code, body)
	}
	if code, _, _ := op.do("POST", "/api/v1/drones/kotowali/speaker/say", `{"text":"hi","lang":"en"}`, csrf); code != 404 {
		t.Fatalf("no speaker say: %d", code)
	}

	// Status: viewers may read; JSON passes through, the token is added.
	code, body, _ := view.do("GET", "/api/v1/drones/m3t/speaker", "", nil)
	if code != 200 || body["status"] != "idle" || body["can_play"] != true {
		t.Fatalf("status: %d %v", code, body)
	}
	if got := f.callLog()[0]; got != "GET /api/v1/aircraft/SN1/speaker Bearer bridge-token " {
		t.Fatalf("upstream call: %q", got)
	}

	// Viewers can't act.
	for _, p := range []string{"say", "stop", "volume"} {
		if code, _, _ := view.do("POST", "/api/v1/drones/m3t/speaker/"+p, `{}`, csrf); code != 403 {
			t.Fatalf("viewer %s: %d", p, code)
		}
	}

	// Say: validated here, then forwarded.
	for body, want := range map[string]int{
		`{"text":"  ","lang":"en"}`:                               422,
		`{"text":"hi","lang":"fr"}`:                               422,
		`{"text":"` + strings.Repeat("a", 501) + `","lang":"en"}`: 422,
		`{"text":"hi","lang":"en","loud":true}`:                   400,
	} {
		if code, _, _ := op.do("POST", "/api/v1/drones/m3t/speaker/say", body, csrf); code != want {
			t.Errorf("say %s: %d, want %d", body[:min(len(body), 40)], code, want)
		}
	}
	if code, body, _ := op.do("POST", "/api/v1/drones/m3t/speaker/say", `{"text":" এলাকা ছেড়ে যান ","lang":"bn","loop":true}`, csrf); code != 200 || body["ok"] != true {
		t.Fatalf("say: %d %v", code, body)
	}
	calls := f.callLog()
	if got := calls[len(calls)-1]; !strings.HasPrefix(got, "POST /api/v1/aircraft/SN1/speaker/say Bearer bridge-token ") ||
		!strings.Contains(got, `"text":"এলাকা ছেড়ে যান"`) || !strings.Contains(got, `"loop":true`) {
		t.Fatalf("say upstream: %q", got)
	}
	// Upstream refusals pass through with their status and reason.
	code, body, _ = op.do("POST", "/api/v1/drones/m3t/speaker/say", `{"text":"refuse me","lang":"en"}`, csrf)
	if code != 409 || body["result"] != "refused" || !strings.Contains(body["error"].(string), "StarWatch control") {
		t.Fatalf("refused say: %d %v", code, body)
	}
	if code, body, _ := op.do("POST", "/api/v1/drones/m3t/speaker/stop", "", csrf); code != 503 || body["error"] != "pilot app not connected" {
		t.Fatalf("stop: %d %v", code, body)
	}

	// Volume: 0..100 only.
	if code, _, _ := op.do("POST", "/api/v1/drones/m3t/speaker/volume", `{"volume":101}`, csrf); code != 422 {
		t.Fatalf("volume 101: %d", code)
	}
	if code, _, _ := op.do("POST", "/api/v1/drones/m3t/speaker/volume", `{"volume":0}`, csrf); code != 200 {
		t.Fatalf("volume 0: %d", code)
	}
	calls = f.callLog()
	if got := calls[len(calls)-1]; !strings.HasSuffix(got, `{"volume":0}`) {
		t.Fatalf("volume upstream: %q", got)
	}
}

func TestSpeakerUnreachable(t *testing.T) {
	f := newFakeSpeaker(t)
	srv := speakerGateway(t, f)
	f.srv.Close()
	op := login(t, srv.URL, "pilot", "operator-pass")
	if code, body, _ := op.do("GET", "/api/v1/drones/m3t/speaker", "", nil); code != 502 || body["error"] != "speaker service unreachable" {
		t.Fatalf("unreachable: %d %v", code, body)
	}
}

func dialLive(t *testing.T, srv *httptest.Server, c *client) (*websocket.Conn, *http.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/v1/drones/m3t/speaker/live",
		&websocket.DialOptions{HTTPClient: c.http})
}

func TestSpeakerLive(t *testing.T) {
	f := newFakeSpeaker(t)
	srv := speakerGateway(t, f)

	// Viewers can't talk.
	if _, resp, err := dialLive(t, srv, login(t, srv.URL, "watcher", "viewer-pass")); err == nil || resp == nil || resp.StatusCode != 403 {
		t.Fatalf("viewer live: %v %v", err, resp)
	}

	ws, _, err := dialLive(t, srv, login(t, srv.URL, "pilot", "operator-pass"))
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	frame := make([]byte, 1280) // 40 ms of 16 kHz s16le
	for i := range frame {
		frame[i] = byte(i)
	}
	for i := 0; i < 3; i++ {
		if err := ws.Write(ctx, websocket.MessageBinary, frame); err != nil {
			t.Fatal(err)
		}
	}
	if err := ws.Write(ctx, websocket.MessageText, []byte(`{"type":"end"}`)); err != nil {
		t.Fatal(err)
	}
	typ, data, err := ws.Read(ctx)
	if err != nil || typ != websocket.MessageText {
		t.Fatalf("result: %v %v", typ, err)
	}
	var res map[string]any
	_ = json.Unmarshal(data, &res)
	if res["type"] != "result" || res["outcome"] != "done" || res["bytes"] != float64(3*1280) {
		t.Fatalf("result: %s", data)
	}
	f.mu.Lock()
	ended, n := f.ended, len(f.audio)
	f.mu.Unlock()
	if !ended || n != 3*1280 {
		t.Fatalf("upstream got end=%v, %d bytes", ended, n)
	}
	// The gateway closes the browser side after the result.
	if _, _, err := ws.Read(ctx); websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		t.Fatalf("after result: %v", err)
	}
}

func TestSpeakerLiveUpstreamRefuses(t *testing.T) {
	f := newFakeSpeaker(t)
	_, srv := testGateway(t, func(c *Config) {
		c.Drones = append(c.Drones, DroneConfig{
			ID: "m3t", MAVLink: "udp:127.0.0.1:9",
			Speaker: &SpeakerConfig{API: f.srv.URL + "/api/v1/aircraft/SN1/speaker", Token: "wrong"},
		})
	})
	ws, _, err := dialLive(t, srv, login(t, srv.URL, "pilot", "operator-pass"))
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := ws.Read(ctx)
	if err != nil || !strings.Contains(string(data), `"outcome":"failed"`) || !strings.Contains(string(data), "bad token") {
		t.Fatalf("refused upstream: %s %v", data, err)
	}
}
