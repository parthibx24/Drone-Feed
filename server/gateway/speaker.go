package gateway

// The drone's loudspeaker, for drones whose speaker is not reached over
// MAVLink (the Mavic 3T: dji-bridge turns text into speech and plays audio
// through the pilot app). The gateway only checks who may do what and
// proxies to the speaker API in the drone's config (DroneConfig.Speaker).
//
// Upstream contract (dji-bridge's operator API, bearer token):
//
//	GET  {api}          → {"present","status","volume","languages","can_play"}
//	POST {api}/say      {"text","lang","loop"}
//	POST {api}/stop
//	POST {api}/volume   {"volume"}
//	GET  {api}/live     WebSocket: binary PCM s16le 16 kHz mono in, one
//	                    {"type":"result","outcome","reason"} out.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"
)

const (
	speakerTimeout  = 15 * time.Second
	speakerSayWait  = 75 * time.Second // speech synthesis plus the pilot app's answer
	speakerMaxText  = 500              // characters
	speakerMaxBody  = 64 << 10
	speakerMaxFrame = 64 << 10 // 40 ms of 16 kHz PCM is 1.3 kB
	speakerMaxLive  = 5 * time.Minute
)

var speakerHTTP = &http.Client{} // per-request deadlines come from the context

// speakerFor returns the drone's speaker config, or answers 404.
func speakerFor(w http.ResponseWriter, d *Drone) (*SpeakerConfig, bool) {
	if d.cfg.Speaker == nil {
		writeErr(w, http.StatusNotFound, "no speaker")
		return nil, false
	}
	return d.cfg.Speaker, true
}

// speakerProxy forwards one request to the speaker API.
func speakerProxy(w http.ResponseWriter, r *http.Request, sp *SpeakerConfig, method, path string, body any, wait time.Duration) {
	upstreamProxy(w, r, "speaker service", sp.API, sp.Token, method, path, body, wait)
}

// upstreamProxy forwards one request to an upstream API (the speaker's or
// the aircraft's, both dji-bridge's) with its bearer token, and passes the
// status and the JSON body back unchanged. Unreachable upstream → 502,
// timeout → 504; service names it in those errors.
func upstreamProxy(w http.ResponseWriter, r *http.Request, service, api, token, method, path string, body any, wait time.Duration) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	ctx, cancel := context.WithTimeout(r.Context(), wait)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, api+path, rd)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := speakerHTTP.Do(req)
	if err != nil {
		var ne net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
			writeErr(w, http.StatusGatewayTimeout, "no answer from the "+service)
		} else {
			writeErr(w, http.StatusBadGateway, service+" unreachable")
		}
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		// The upstream refused the gateway, not the user: passing a 401 on
		// would sign the dashboard user out.
		writeErr(w, http.StatusBadGateway, service+" refused the gateway's token")
		return
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, speakerMaxBody))
	var parsed map[string]any
	if json.Unmarshal(data, &parsed) != nil {
		msg := strings.TrimSpace(string(data))
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		if resp.StatusCode < 300 {
			writeJSON(w, resp.StatusCode, map[string]any{"ok": true})
		} else {
			writeErr(w, resp.StatusCode, msg)
		}
		return
	}
	if rec, ok := w.(*statusRecorder); ok && resp.StatusCode >= 300 {
		if e, _ := parsed["error"].(string); e != "" {
			rec.errMsg = e
		}
	}
	writeJSON(w, resp.StatusCode, parsed)
}

type sayRequest struct {
	Text string `json:"text"`
	Lang string `json:"lang"`
	Loop bool   `json:"loop"`
}

// validate trims the text and checks the language; "" if fine.
func (s *sayRequest) validate() string {
	s.Text = strings.TrimSpace(s.Text)
	switch {
	case s.Text == "":
		return "text is required"
	case utf8.RuneCountInString(s.Text) > speakerMaxText:
		return "text is too long (max 500 characters)"
	case s.Lang != "en" && s.Lang != "bn":
		return "lang must be en or bn"
	}
	return ""
}

func (g *Gateway) speakerRoutes(mux *http.ServeMux) {
	V, O := roleViewer, roleOperator

	mux.HandleFunc("GET /api/v1/drones/{id}/speaker", g.route(V, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		if sp, ok := speakerFor(w, d); ok {
			speakerProxy(w, r, sp, http.MethodGet, "", nil, speakerTimeout)
		}
	}))

	mux.HandleFunc("POST /api/v1/drones/{id}/speaker/say", g.route(O, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		sp, ok := speakerFor(w, d)
		if !ok {
			return
		}
		var b sayRequest
		if !decode(w, r, &b) {
			return
		}
		if msg := b.validate(); msg != "" {
			writeErr(w, http.StatusUnprocessableEntity, msg)
			return
		}
		d.log.Info("speaker say", "lang", b.Lang, "loop", b.Loop, "chars", utf8.RuneCountInString(b.Text))
		d.log.Debug("speaker say text", "text", b.Text)
		speakerProxy(w, r, sp, http.MethodPost, "/say", b, speakerSayWait)
	}))

	mux.HandleFunc("POST /api/v1/drones/{id}/speaker/stop", g.route(O, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		if sp, ok := speakerFor(w, d); ok {
			speakerProxy(w, r, sp, http.MethodPost, "/stop", struct{}{}, speakerTimeout)
		}
	}))

	mux.HandleFunc("POST /api/v1/drones/{id}/speaker/volume", g.route(O, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		sp, ok := speakerFor(w, d)
		if !ok {
			return
		}
		var b struct {
			Volume *int `json:"volume"`
		}
		if !decode(w, r, &b) {
			return
		}
		if b.Volume == nil || *b.Volume < 0 || *b.Volume > 100 {
			writeErr(w, http.StatusUnprocessableEntity, "volume must be 0-100")
			return
		}
		speakerProxy(w, r, sp, http.MethodPost, "/volume", map[string]int{"volume": *b.Volume}, speakerTimeout)
	}))

	mux.HandleFunc("GET /api/v1/drones/{id}/speaker/live", g.route(O, g.speakerLive))
}

// speakerLive relays push-to-talk audio: the browser's binary PCM frames and
// its {"type":"end"} go upstream, the upstream's result comes back. Either
// side closing ends the session; it never outlives speakerMaxLive.
func (g *Gateway) speakerLive(w http.ResponseWriter, r *http.Request, d *Drone) {
	sp, ok := speakerFor(w, d)
	if !ok {
		return
	}
	a, _ := r.Context().Value(actorKey).(actor)
	opts := &websocket.AcceptOptions{OriginPatterns: g.wsOrigins()}
	if len(opts.OriginPatterns) == 1 && opts.OriginPatterns[0] == "*" {
		opts.InsecureSkipVerify = true
	}
	client, err := websocket.Accept(w, r, opts)
	if err != nil {
		return
	}
	defer client.CloseNow()
	client.SetReadLimit(speakerMaxFrame)

	ctx := r.Context()
	start := time.Now()
	var sent int64
	g.audit.Info("speaker live start", "actor", a.name, "drone", d.cfg.ID)
	defer func() {
		g.audit.Info("speaker live end", "actor", a.name, "drone", d.cfg.ID,
			"seconds", time.Since(start).Round(100*time.Millisecond).Seconds(), "audio_bytes", sent)
	}()

	fail := func(reason string) {
		wctx, wcancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer wcancel()
		b, _ := json.Marshal(map[string]string{"type": "result", "outcome": "failed", "reason": reason})
		_ = client.Write(wctx, websocket.MessageText, b)
		client.Close(websocket.StatusNormalClosure, "")
	}

	dctx, dcancel := context.WithTimeout(ctx, speakerTimeout)
	upstream, resp, err := websocket.Dial(dctx, wsURL(sp.API+"/live"), &websocket.DialOptions{
		HTTPClient: speakerHTTP,
		HTTPHeader: http.Header{"Authorization": {"Bearer " + sp.Token}},
	})
	dcancel()
	if err != nil {
		reason := "speaker service unreachable"
		if resp != nil {
			reason = upstreamReason(resp)
		}
		d.log.Warn("speaker live: upstream", "err", err)
		fail(reason)
		return
	}
	defer upstream.CloseNow()
	upstream.SetReadLimit(speakerMaxBody)

	// A session never outlives speakerMaxLive: closing the browser side
	// ends the loop below, which ends the upstream side.
	limit := time.AfterFunc(speakerMaxLive, func() { client.Close(websocket.StatusNormalClosure, "time limit") })
	defer limit.Stop()

	// Upstream → browser: text frames (the result). When upstream ends,
	// close the browser side cleanly, which also ends the loop below.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			typ, data, err := upstream.Read(ctx)
			if err != nil {
				client.Close(websocket.StatusNormalClosure, "")
				return
			}
			if typ == websocket.MessageText {
				wctx, wcancel := context.WithTimeout(ctx, 5*time.Second)
				err := client.Write(wctx, websocket.MessageText, data)
				wcancel()
				if err != nil {
					return
				}
			}
		}
	}()

	// Browser → upstream: audio and the end marker.
	for {
		typ, data, err := client.Read(ctx)
		if err != nil {
			break
		}
		wctx, wcancel := context.WithTimeout(ctx, 5*time.Second)
		err = upstream.Write(wctx, typ, data)
		wcancel()
		if err != nil {
			break
		}
		if typ == websocket.MessageBinary {
			sent += int64(len(data))
		}
	}
	// The browser left or the time is up: tell upstream the audio has ended,
	// give it a moment to answer, then close both sides.
	upstream.Close(websocket.StatusNormalClosure, "")
	select {
	case <-done:
	case <-time.After(3 * time.Second):
	}
	client.Close(websocket.StatusNormalClosure, "")
}

// upstreamReason reads a failed WebSocket handshake's error message.
func upstreamReason(resp *http.Response) string {
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var parsed struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &parsed) == nil && parsed.Error != "" {
		return parsed.Error
	}
	if s := strings.TrimSpace(string(data)); s != "" {
		return s
	}
	return "speaker service answered " + resp.Status
}

// wsURL turns an http(s) URL into ws(s).
func wsURL(u string) string {
	switch {
	case strings.HasPrefix(u, "https://"):
		return "wss://" + strings.TrimPrefix(u, "https://")
	case strings.HasPrefix(u, "http://"):
		return "ws://" + strings.TrimPrefix(u, "http://")
	}
	return u
}
