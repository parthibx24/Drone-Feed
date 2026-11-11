package gateway

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func ptr[T any](v T) *T { return &v }

func TestGroundPoint(t *testing.T) {
	pose := DronePose{Lat: 24.9, Lon: 91.87, AltRelM: 100, HeadingDeg: ptr(0.0)}
	box := [4]float64{0.45, 0.4, 0.1, 0.1} // feet exactly at the frame centre

	g := groundPoint(pose, -45, 0, 60, 16.0/9, box)
	if g == nil {
		t.Fatal("expected a ground point at 45° down")
	}
	if math.Abs(g.DistanceM-100) > 1 {
		t.Fatalf("distance = %v, want 100", g.DistanceM)
	}
	if dLat := (g.Lat - pose.Lat) * 111320; math.Abs(dLat-100) > 2 || math.Abs(g.Lon-pose.Lon) > 1e-6 {
		t.Fatalf("heading 0 should move north only: %+v", g)
	}

	pose.HeadingDeg = ptr(90.0)
	g = groundPoint(pose, -45, 0, 60, 16.0/9, box)
	if g == nil || math.Abs(g.Lat-pose.Lat) > 1e-6 || g.Lon <= pose.Lon {
		t.Fatalf("heading 90 should move east: %+v", g)
	}
	// Gimbal yawed 90° left while flying east: the object is north.
	g = groundPoint(pose, -45, -90, 60, 16.0/9, box)
	if g == nil || g.Lat <= pose.Lat || math.Abs(g.Lon-pose.Lon) > 1e-5 {
		t.Fatalf("camera yaw should add to heading: %+v", g)
	}

	if groundPoint(pose, -2, 0, 60, 16.0/9, box) != nil {
		t.Fatal("a ray 2° below the horizon must not produce a point")
	}
	if g := groundPoint(pose, -90, 0, 60, 16.0/9, box); g == nil || g.DistanceM > 1 {
		t.Fatalf("straight down should be under the drone: %+v", g)
	}
	pose.HeadingDeg = nil
	if groundPoint(pose, -45, 0, 60, 16.0/9, box) != nil {
		t.Fatal("no heading, no estimate")
	}
}

func TestFollowRates(t *testing.T) {
	centred := [4]float64{0.45, 0.45, 0.1, 0.1}
	if p, y := followRates(centred, 60, 16.0/9, 10); p != 0 || y != 0 {
		t.Fatalf("centred object must not move the gimbal: %v %v", p, y)
	}
	rightUp := [4]float64{0.7, 0.1, 0.1, 0.1}
	p, y := followRates(rightUp, 60, 16.0/9, 10)
	if y <= 0 || p <= 0 {
		t.Fatalf("object up-right needs yaw right and tilt up: pitch %v yaw %v", p, y)
	}
	farLeft := [4]float64{0, 0.45, 0.02, 0.1}
	if _, y := followRates(farLeft, 60, 16.0/9, 4); y != -4 {
		t.Fatalf("rate must be clamped to the maximum: %v", y)
	}
}

func TestGimbalTrackerDeadman(t *testing.T) {
	var tr gimbalTracker
	if _, _, known := tr.estimate(); known {
		t.Fatal("unknown until an absolute angle is commanded")
	}
	tr.setAbsolute(-30, float32(math.NaN()))
	now := time.Now()
	tr.ratePitch, tr.rateAt = -10, now.Add(-2*time.Second)
	tr.advance(now)
	if math.Abs(tr.pitch-(-36)) > 0.01 {
		t.Fatalf("a rate counts for at most the dead-man time: pitch %v, want -36", tr.pitch)
	}
	if tr.ratePitch != 0 {
		t.Fatal("rate must stop after the dead-man time")
	}
	tr.setAbsolute(0, 170)
	tr.rateYaw, tr.rateAt = 50, time.Now().Add(-500*time.Millisecond)
	tr.advance(time.Now())
	if tr.yaw > -150 || tr.yaw < -170 {
		t.Fatalf("yaw must wrap at ±180: %v", tr.yaw)
	}
}

func TestEventStore(t *testing.T) {
	dir := t.TempDir()
	s, err := newEventStore(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	old := AIEvent{ID: newEventID(now.Add(-40*24*time.Hour), "a", 1), DroneID: "a", At: now.Add(-40 * 24 * time.Hour), Label: "car", Group: "vehicle"}
	p := AIEvent{ID: newEventID(now, "a/b", 2), DroneID: "a", At: now, Label: "person", Group: "person"}
	if err := s.add(old, []byte{0xFF, 0xD8, 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.add(p, []byte{0xFF, 0xD8, 2}); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(p.ID, "/") {
		t.Fatalf("event ids must be path-safe: %s", p.ID)
	}
	if got := s.query(eventQuery{Group: "person"}); len(got) != 1 || got[0].ID != p.ID || got[0].SnapshotURL == "" {
		t.Fatalf("query by group: %+v", got)
	}
	if got := s.query(eventQuery{Since: now.Add(-time.Hour)}); len(got) != 1 {
		t.Fatalf("query since: %d events", len(got))
	}
	if _, ok := s.snapshot("../../etc/passwd"); ok {
		t.Fatal("snapshot ids must not escape the folder")
	}

	// Reloading prunes the 40-day-old event, from the log and its snapshot.
	s2, err := newEventStore(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	if got := s2.query(eventQuery{}); len(got) != 1 || got[0].ID != p.ID {
		t.Fatalf("after reload and prune: %+v", got)
	}
	if _, ok := s2.snapshot(old.ID); ok {
		t.Fatal("pruned snapshot must be deleted")
	}
}

// ------------------------------------------------------------ handler flow

const (
	testIngest = "ingest-token-0123456789abcdef"
	testOp     = "operator-token"
)

type aiEnv struct {
	t        *testing.T
	gw       *Gateway
	api      *httptest.Server
	mu       sync.Mutex
	webhooks []map[string]any
	claude   []map[string]any
}

func newAIEnv(t *testing.T) *aiEnv {
	env := &aiEnv{t: t}
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]any
		_ = json.NewDecoder(r.Body).Decode(&m)
		env.mu.Lock()
		env.webhooks = append(env.webhooks, m)
		env.mu.Unlock()
	}))
	t.Cleanup(hook.Close)
	go2rtc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/frame.jpeg":
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write([]byte{0xFF, 0xD8, 0xFF, 0xD9})
		case "/api/streams":
			w.Write([]byte(`{"sim_camera":{},"sim_thermal":{},"other_camera":{}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(go2rtc.Close)
	claude := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]any
		_ = json.NewDecoder(r.Body).Decode(&m)
		m["_beta"] = r.Header.Get("anthropic-beta")
		env.mu.Lock()
		env.claude = append(env.claude, m)
		env.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5",
			"content":[{"type":"thinking","thinking":"","signature":"x"},{"type":"text","text":"Two people beside a parked car."}],
			"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":900,"output_tokens":12}}`))
	}))
	t.Cleanup(claude.Close)

	cfg := &Config{
		Tokens:        []Token{{Name: "ops", Token: testOp, Role: "operator"}},
		Drones:        []DroneConfig{{ID: "sim", Name: "Sim", MAVLink: "udp:127.0.0.1:9"}},
		RecordingsDir: t.TempDir(),
		Go2RTC:        Go2RTCConfig{PublicURL: go2rtc.URL},
		AI: AIConfig{
			Enabled: true, IngestToken: testIngest, RunWhen: "always",
			Alerts:  AIAlertsConfig{Labels: []string{"person"}, WebhookURL: hook.URL},
			Vision:  AIVisionConfig{APIKey: "sk-test", BaseURL: claude.URL},
		},
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw, err := New(cfg, log, "test")
	if err != nil {
		t.Fatal(err)
	}
	env.gw = gw
	env.api = httptest.NewServer(gw.Handler())
	t.Cleanup(env.api.Close)
	return env
}

func (e *aiEnv) do(method, path, token string, body io.Reader, ctype string) (int, map[string]any) {
	req, _ := http.NewRequest(method, e.api.URL+path, body)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&m)
	return resp.StatusCode, m
}

func (e *aiEnv) json(method, path, token string, v any) (int, map[string]any) {
	b, _ := json.Marshal(v)
	return e.do(method, path, token, bytes.NewReader(b), "application/json")
}

func (e *aiEnv) postEvent(ev map[string]any) (int, map[string]any) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	b, _ := json.Marshal(ev)
	_ = mw.WriteField("event", string(b))
	fw, _ := mw.CreateFormFile("snapshot", "s.jpg")
	fw.Write([]byte{0xFF, 0xD8, 0xFF, 0xD9})
	mw.Close()
	return e.do("POST", "/api/v1/ai/events", testIngest, &buf, mw.FormDataContentType())
}

func TestAIFlow(t *testing.T) {
	env := newAIEnv(t)

	// The ingest token works only on the detector endpoints, and vice versa.
	if code, _ := env.do("GET", "/api/v1/ai/targets", testOp, nil, ""); code != 401 {
		t.Fatalf("operator token on targets: %d", code)
	}
	if code, _ := env.do("GET", "/api/v1/drones", testIngest, nil, ""); code != 401 {
		t.Fatalf("ingest token on the normal API: %d", code)
	}

	code, tg := env.do("GET", "/api/v1/ai/targets", testIngest, nil, "")
	if code != 200 {
		t.Fatalf("targets: %d %v", code, tg)
	}
	targets := tg["targets"].([]any)
	if len(targets) != 2 {
		t.Fatalf("want the drone's two discovered streams, got %v", targets)
	}
	kinds := map[string]string{}
	for _, x := range targets {
		m := x.(map[string]any)
		kinds[m["stream"].(string)] = m["kind"].(string)
	}
	if kinds["sim_camera"] != "visible" || kinds["sim_thermal"] != "thermal" {
		t.Fatalf("stream kinds: %v", kinds)
	}

	frame := map[string]any{
		"drone_id": "sim", "stream": "sim_camera", "kind": "visible", "width": 1280, "height": 720,
		"model": "test", "infer_ms": 40,
		"detections": []map[string]any{
			{"track_id": 7, "label": "Person", "group": "person", "confidence": 0.9, "box": []float64{0.4, 0.4, 0.05, 0.1}},
			{"track_id": 8, "label": "car", "group": "vehicle", "confidence": 0.8, "box": []float64{0.1, 0.6, 0.1, 0.08}},
			{"track_id": 9, "label": "car", "group": "vehicle", "confidence": 0.8, "box": []float64{0.1, 0.6, 0, 0.08}}, // empty box: dropped
		},
	}
	if code, m := env.json("POST", "/api/v1/ai/detections", testIngest, frame); code != 200 {
		t.Fatalf("detections: %d %v", code, m)
	}
	code, st := env.do("GET", "/api/v1/drones/sim/ai", testOp, nil, "")
	if code != 200 || st["detector_online"] != true {
		t.Fatalf("status: %d %v", code, st)
	}
	counts := st["streams"].(map[string]any)["sim_camera"].(map[string]any)["counts"].(map[string]any)
	if counts["person"].(float64) != 1 || counts["vehicle"].(float64) != 1 {
		t.Fatalf("counts: %v", counts)
	}

	// Two person events in a row: one alert (cooldown), both stored.
	ev := map[string]any{"drone_id": "sim", "stream": "sim_camera", "kind": "visible", "track_id": 7,
		"label": "person", "group": "person", "confidence": 0.9, "box": []float64{0.4, 0.4, 0.05, 0.1}}
	if code, m := env.postEvent(ev); code != 200 || m["id"] == "" {
		t.Fatalf("event: %d %v", code, m)
	}
	ev["track_id"] = 11
	if code, _ := env.postEvent(ev); code != 200 {
		t.Fatal("second event")
	}
	code, list := env.do("GET", "/api/v1/ai/events?drone=sim&label=person", testOp, nil, "")
	evs := list["events"].([]any)
	if code != 200 || len(evs) != 2 {
		t.Fatalf("events: %d %v", code, list)
	}
	alerted := 0
	for _, x := range evs {
		if x.(map[string]any)["alerted"] == true {
			alerted++
		}
	}
	if alerted != 1 {
		t.Fatalf("cooldown: %d of 2 events alerted", alerted)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		env.mu.Lock()
		n := len(env.webhooks)
		env.mu.Unlock()
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("webhook deliveries: %d, want 1", n)
		}
		time.Sleep(20 * time.Millisecond)
	}
	snapURL := evs[0].(map[string]any)["snapshot_url"].(string)
	req, _ := http.NewRequest("GET", env.api.URL+snapURL, nil)
	req.Header.Set("Authorization", "Bearer "+testOp)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("snapshot: %v %v", err, resp)
	}
	resp.Body.Close()

	// Follow: an object in the latest frame can be followed, an unknown one not.
	if code, _ := env.json("POST", "/api/v1/drones/sim/ai/follow", testOp, map[string]any{"stream": "sim_camera", "track_id": 99}); code != 422 {
		t.Fatalf("follow unknown track: %d", code)
	}
	if code, m := env.json("POST", "/api/v1/drones/sim/ai/follow", testOp, map[string]any{"stream": "sim_camera", "track_id": 7}); code != 200 {
		t.Fatalf("follow: %d %v", code, m)
	}
	if _, st := env.do("GET", "/api/v1/drones/sim/ai", testOp, nil, ""); st["follow"].(map[string]any)["active"] != true {
		t.Fatal("follow should be active")
	}
	env.gw.operatorGimbal(env.gw.drones["sim"])
	if _, st := env.do("GET", "/api/v1/drones/sim/ai", testOp, nil, ""); st["follow"].(map[string]any)["active"] != false {
		t.Fatal("operator gimbal input must stop follow")
	}

	// Analyze: frame from go2rtc, question to Claude with the image and fallbacks.
	code, an := env.json("POST", "/api/v1/drones/sim/ai/analyze", testOp, map[string]any{"prompt": "How many people?"})
	if code != 200 || !strings.Contains(an["text"].(string), "Two people") || an["stream"] != "sim_camera" {
		t.Fatalf("analyze: %d %v", code, an)
	}
	env.mu.Lock()
	req0 := env.claude[0]
	env.mu.Unlock()
	if req0["model"] != "claude-opus-5-5" || req0["fallbacks"] != "default" || !strings.Contains(req0["_beta"].(string), "server-side-fallback") {
		t.Fatalf("claude request: model %v fallbacks %v beta %v", req0["model"], req0["fallbacks"], req0["_beta"])
	}
	content := req0["messages"].([]any)[0].(map[string]any)["content"].([]any)
	if content[0].(map[string]any)["type"] != "image" || !strings.Contains(content[1].(map[string]any)["text"].(string), "How many people?") {
		t.Fatalf("claude content: %v", content)
	}

	// Turning AI off removes the drone from the detector's targets.
	if code, _ := env.json("POST", "/api/v1/drones/sim/ai", testOp, map[string]any{"enabled": false}); code != 200 {
		t.Fatal("disable")
	}
	if _, tg := env.do("GET", "/api/v1/ai/targets", testIngest, nil, ""); len(tg["targets"].([]any)) != 0 {
		t.Fatalf("disabled drone still targeted: %v", tg)
	}
}

func TestAIConfigValidation(t *testing.T) {
	base := func() *Config {
		return &Config{Drones: []DroneConfig{{ID: "a", MAVLink: "udp:127.0.0.1:1"}}, AI: AIConfig{Enabled: true, IngestToken: testIngest}}
	}
	c := base()
	if err := c.Normalize(); err != nil || c.AI.Vision.Model != "claude-opus-5-5" || c.AI.FPS != 3 {
		t.Fatalf("defaults: %v %+v", err, c.AI)
	}
	c = base()
	c.AI.IngestToken = "short"
	if c.Normalize() == nil {
		t.Fatal("short ingest token must be rejected")
	}
	c = base()
	c.AI.Alerts.WebhookURL = "ftp://x"
	if c.Normalize() == nil {
		t.Fatal("non-http webhook must be rejected")
	}
	c = base()
	c.AI.Enabled = false
	c.AI.IngestToken = ""
	if err := c.Normalize(); err != nil {
		t.Fatalf("AI disabled needs nothing: %v", err)
	}
}
