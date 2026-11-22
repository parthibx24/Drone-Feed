package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeAircraft is the upstream aircraft API (dji-bridge's, in production).
type fakeAircraft struct {
	srv *httptest.Server

	mu    sync.Mutex
	calls []string // "METHOD path auth body"
}

func newFakeAircraft(t *testing.T) *fakeAircraft {
	f := &fakeAircraft{}
	mux := http.NewServeMux()
	const base = "/api/v1/aircraft/SN1"
	record := func(r *http.Request) string {
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls = append(f.calls, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization")+" "+string(b))
		return string(b)
	}
	authorized := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer bridge-token" {
			writeJSON(w, 401, map[string]any{"error": "bad token"})
			return false
		}
		return true
	}
	mux.HandleFunc("GET "+base+"/features", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		if !authorized(w, r) {
			return
		}
		writeJSON(w, 200, map[string]any{
			"features": []string{"thermal_measure", "thermal_palette", "obstacle_avoidance", "rtk"},
			"thermal": map[string]any{"palette": "white_hot", "palettes": []string{"white_hot", "iron_red"}, "gain": "high",
				"isotherm": nil, "measure": "spot", "spot": map[string]any{"x": 0.5, "y": 0.5, "temp_c": 36.4}, "area": nil},
			"avoidance": map[string]any{"mode": "brake", "horizontal": true, "up": true, "down": true},
			"rtk":       map[string]any{"present": true, "enabled": false, "fix": "none", "sats": 0},
			"online":    true,
		})
	})
	mux.HandleFunc("POST "+base+"/commands", func(w http.ResponseWriter, r *http.Request) {
		body := record(r)
		if !authorized(w, r) {
			return
		}
		var cmd map[string]any
		_ = json.Unmarshal([]byte(body), &cmd)
		if cmd["kind"] == "rtk_enable" {
			writeJSON(w, 409, map[string]any{"ok": false, "error": "no RTK module on the aircraft"})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "kind": cmd["kind"]})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAircraft) callLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func aircraftGateway(t *testing.T, f *fakeAircraft, token string) *httptest.Server {
	_, srv := testGateway(t, func(c *Config) {
		c.Drones = append(c.Drones, DroneConfig{
			ID: "m3t", MAVLink: "udp:127.0.0.1:9",
			Bridge: &BridgeConfig{API: f.srv.URL + "/api/v1/aircraft/SN1/", Token: token},
		})
	})
	return srv
}

func TestBridgeConfig(t *testing.T) {
	for name, body := range map[string]string{
		"no token": "drones: [{id: a, mavlink: 'udp:h:1', bridge: {api: 'http://b:8090/x'}}]\n",
		"bad url":  "drones: [{id: a, mavlink: 'udp:h:1', bridge: {api: 'b:8090/x', token: t}}]\n",
		"bad speaker override": "drones: [{id: a, mavlink: 'udp:h:1', bridge: {api: 'http://b/x', token: t}, " +
			"speaker: {api: 'nope', token: t}}]\n",
	} {
		if _, err := loadYAML(t, body); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}

	t.Setenv("GW_TEST_BRIDGE", "bridge-token")
	cfg, err := loadYAML(t, "drones: [{id: a, mavlink: 'udp:h:1', bridge: {api: ' http://b:8090/api/v1/aircraft/SN1/ ', token: '${GW_TEST_BRIDGE}'}}]\n")
	if err != nil {
		t.Fatal(err)
	}
	d := cfg.Drones[0]
	if d.Bridge.API != "http://b:8090/api/v1/aircraft/SN1" || d.Bridge.Token != "bridge-token" {
		t.Fatalf("bridge: %+v", d.Bridge)
	}
	// The speaker follows the bridge unless it is set.
	if d.Speaker == nil || d.Speaker.API != "http://b:8090/api/v1/aircraft/SN1/speaker" || d.Speaker.Token != "bridge-token" {
		t.Fatalf("derived speaker: %+v", d.Speaker)
	}

	cfg, err = loadYAML(t, "drones: [{id: a, mavlink: 'udp:h:1', bridge: {api: 'http://b/x', token: t}, speaker: {api: 'http://s/y', token: u}}]\n")
	if err != nil {
		t.Fatal(err)
	}
	if sp := cfg.Drones[0].Speaker; sp.API != "http://s/y" || sp.Token != "u" {
		t.Fatalf("speaker override: %+v", sp)
	}

	// The old form, speaker alone, still works and gives no aircraft features.
	cfg, err = loadYAML(t, "drones: [{id: a, mavlink: 'udp:h:1', speaker: {api: 'http://b/x/speaker', token: t}}]\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Drones[0].Bridge != nil || cfg.Drones[0].Speaker.API != "http://b/x/speaker" {
		t.Fatalf("speaker only: %+v %+v", cfg.Drones[0].Bridge, cfg.Drones[0].Speaker)
	}
}

func TestAircraftFeatures(t *testing.T) {
	f := newFakeAircraft(t)
	srv := aircraftGateway(t, f, "bridge-token")
	view := login(t, srv.URL, "watcher", "viewer-pass")

	code, body, _ := view.do("GET", "/api/v1/drones/m3t/features", "", nil)
	if code != 200 || body["online"] != true {
		t.Fatalf("features: %d %v", code, body)
	}
	if spot := body["thermal"].(map[string]any)["spot"].(map[string]any); spot["temp_c"] != 36.4 {
		t.Fatalf("thermal passed through: %v", body["thermal"])
	}
	if calls := f.callLog(); len(calls) != 1 || calls[0] != "GET /api/v1/aircraft/SN1/features Bearer bridge-token " {
		t.Fatalf("upstream calls: %q", calls)
	}

	// A drone without an aircraft API (an ArduPilot drone) answers 404.
	if code, body, _ := view.do("GET", "/api/v1/drones/kotowali/features", "", nil); code != 404 || body["error"] != "no aircraft features" {
		t.Fatalf("kotowali features: %d %v", code, body)
	}
	if code, _, _ := view.do("GET", "/api/v1/drones/nope/features", "", nil); code != 404 {
		t.Fatalf("unknown drone: %d", code)
	}
	if code, _, _ := newClient(t, srv.URL).do("GET", "/api/v1/drones/m3t/features", "", nil); code != 401 {
		t.Fatalf("signed out: %d", code)
	}
}

func TestAircraftCommand(t *testing.T) {
	f := newFakeAircraft(t)
	srv := aircraftGateway(t, f, "bridge-token")
	op := login(t, srv.URL, "pilot", "operator-pass")
	view := login(t, srv.URL, "watcher", "viewer-pass")
	const path = "/api/v1/drones/m3t/aircraft-command"

	code, body, _ := op.do("POST", path, `{"kind":"meter_spot","x":0.25,"y":0.75}`, csrf)
	if code != 200 || body["ok"] != true || body["kind"] != "meter_spot" {
		t.Fatalf("meter_spot: %d %v", code, body)
	}
	calls := f.callLog()
	if len(calls) != 1 || !strings.HasPrefix(calls[0], "POST /api/v1/aircraft/SN1/commands Bearer bridge-token ") {
		t.Fatalf("upstream calls: %q", calls)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(strings.SplitN(calls[0], " ", 5)[4]), &sent); err != nil || sent["x"] != 0.25 || sent["y"] != 0.75 {
		t.Fatalf("body forwarded: %q (%v)", calls[0], err)
	}

	// The aircraft's refusal comes back with its status and reason.
	if code, body, _ := op.do("POST", path, `{"kind":"rtk_enable","enabled":true}`, csrf); code != 409 || body["error"] != "no RTK module on the aircraft" {
		t.Fatalf("refusal: %d %v", code, body)
	}

	// Only the listed kinds pass: nothing that flies, nothing unknown.
	for _, b := range []string{`{"kind":"takeoff"}`, `{"kind":"gimbal_angle","pitch":-90}`, `{"x":1}`, `{"kind":5}`, `[]`, `nope`} {
		if code, _, _ := op.do("POST", path, b, csrf); code != 400 {
			t.Errorf("%s: %d, want 400", b, code)
		}
	}
	if n := len(f.callLog()); n != 2 {
		t.Fatalf("refused bodies reached the bridge: %d calls", n)
	}

	// Viewers can't, and a write needs the CSRF header.
	if code, _, _ := view.do("POST", path, `{"kind":"meter_spot","x":0.5,"y":0.5}`, csrf); code != 403 {
		t.Fatalf("viewer: %d", code)
	}
	if code, _, _ := op.do("POST", path, `{"kind":"meter_spot","x":0.5,"y":0.5}`, nil); code != 403 {
		t.Fatalf("no csrf: %d", code)
	}
	if code, body, _ := op.do("POST", "/api/v1/drones/kotowali/aircraft-command", `{"kind":"meter_spot","x":0.5,"y":0.5}`, csrf); code != 404 || body["error"] != "no aircraft features" {
		t.Fatalf("kotowali: %d %v", code, body)
	}
}

func TestAircraftUpstreamProblems(t *testing.T) {
	f := newFakeAircraft(t)
	// The bridge refuses the gateway's token: the user must not be signed out.
	srv := aircraftGateway(t, f, "wrong")
	op := login(t, srv.URL, "pilot", "operator-pass")
	if code, body, _ := op.do("GET", "/api/v1/drones/m3t/features", "", nil); code != 502 || !strings.Contains(body["error"].(string), "token") {
		t.Fatalf("bad token: %d %v", code, body)
	}
	if code, _, _ := op.do("GET", "/api/auth/me", "", nil); code != 200 {
		t.Fatal("signed out after the bridge refused the gateway")
	}

	f.srv.Close()
	if code, body, _ := op.do("POST", "/api/v1/drones/m3t/aircraft-command", `{"kind":"thermal_gain","gain":"low"}`, csrf); code != 502 || body["error"] != "aircraft service unreachable" {
		t.Fatalf("unreachable: %d %v", code, body)
	}
}
