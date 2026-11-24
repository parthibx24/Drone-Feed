package gateway

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Detection is one object in a processed frame. Box is [x, y, w, h],
// normalised 0–1 from the top-left corner.
type Detection struct {
	TrackID    int        `json:"track_id"`
	Label      string     `json:"label"`
	Group      string     `json:"group"`
	Confidence float64    `json:"confidence"`
	Box        [4]float64 `json:"box"`
}

// DetectionFrame is the detector's result for one video frame.
type DetectionFrame struct {
	DroneID    string      `json:"drone_id"`
	Stream     string      `json:"stream"`
	Kind       string      `json:"kind"`
	TS         time.Time   `json:"ts"`
	Width      int         `json:"width"`
	Height     int         `json:"height"`
	Model      string      `json:"model"`
	InferMS    float64     `json:"infer_ms"`
	Detections []Detection `json:"detections"`
}

const (
	maxDetectionsPerFrame = 300
	detectorOfflineAfter  = 10 * time.Second
	streamCacheTTL        = 30 * time.Second
	followLostAfter       = 2 * time.Second
	followStaleAfter      = 800 * time.Millisecond
	followHz              = 5
)

type aiStream struct {
	kind     string
	frame    DetectionFrame
	received time.Time
	fps      float64
}

type followState struct {
	stream  string
	trackID int
	box     [4]float64
	seen    time.Time // last frame that contained the track
}

type aiDrone struct {
	enabled   bool
	streams   map[string]*aiStream
	follow    *followState
	autoRec   bool // a recording this module started
	autoLast  time.Time
	lastAlert map[string]time.Time
}

type cachedStreams struct {
	names []string
	at    time.Time
}

// aiHub implements docs/09-ai.md on the gateway side.
type aiHub struct {
	g      *Gateway
	cfg    *AIConfig
	log    *slog.Logger
	store  *eventStore
	alerts *alerter
	vision *visionClient

	mu           sync.Mutex
	drones       map[string]*aiDrone
	detectorSeen time.Time
	streamCache  map[string]cachedStreams
}

func newAIHub(g *Gateway) (*aiHub, error) {
	cfg := &g.cfg.AI
	store, err := newEventStore(cfg.DataDir, cfg.RetentionDays)
	if err != nil {
		return nil, err
	}
	h := &aiHub{
		g: g, cfg: cfg, log: g.log.With("component", "ai"), store: store,
		drones: map[string]*aiDrone{}, streamCache: map[string]cachedStreams{},
	}
	for id := range g.drones {
		h.drones[id] = &aiDrone{enabled: true, streams: map[string]*aiStream{}, lastAlert: map[string]time.Time{}}
	}
	if cfg.Alerts.configured() {
		h.alerts = newAlerter(cfg.Alerts, h.log)
	}
	if cfg.Vision.APIKey != "" {
		h.vision = newVisionClient(cfg.Vision)
	}
	return h, nil
}

// run drives follow control, auto-record stop and retention.
func (h *aiHub) run(ctx context.Context) {
	follow := time.NewTicker(time.Second / followHz)
	housekeeping := time.NewTicker(2 * time.Second)
	prune := time.NewTicker(6 * time.Hour)
	defer follow.Stop()
	defer housekeeping.Stop()
	defer prune.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-follow.C:
			h.followTick()
		case <-housekeeping.C:
			h.autoRecordTick()
		case <-prune.C:
			if err := h.store.prune(time.Now()); err != nil {
				h.log.Warn("event retention failed", "err", err)
			}
		}
	}
}

// ------------------------------------------------------------ detector side

func (h *aiHub) ingestAuthorized(r *http.Request) bool {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && subtle.ConstantTimeCompare([]byte(tok), []byte(h.cfg.IngestToken)) == 1
}

func streamKind(name string) string {
	n := strings.ToLower(name)
	if strings.Contains(n, "thermal") || strings.HasSuffix(n, "_ir") || strings.Contains(n, "_ir_") {
		return "thermal"
	}
	return "visible"
}

func (h *aiHub) streamsFor(ctx context.Context, d *Drone) []string {
	h.mu.Lock()
	c, ok := h.streamCache[d.cfg.ID]
	h.mu.Unlock()
	if ok && time.Since(c.at) < streamCacheTTL {
		return c.names
	}
	names := h.g.discoverStreams(ctx, d.cfg.StreamPrefix)
	if len(names) == 0 {
		names = defaultStreams(d.cfg.StreamPrefix)
	}
	h.mu.Lock()
	h.streamCache[d.cfg.ID] = cachedStreams{names: names, at: time.Now()}
	h.mu.Unlock()
	return names
}

// active reports whether the detector should process this drone now.
func (h *aiHub) active(d *Drone) bool {
	h.mu.Lock()
	ad := h.drones[d.cfg.ID]
	enabled := ad != nil && ad.enabled
	following := ad != nil && ad.follow != nil
	h.mu.Unlock()
	if !enabled {
		return false
	}
	return h.cfg.RunWhen == "always" || following || d.Snapshot().Armed || d.watchers() > 0
}

type aiTarget struct {
	DroneID string `json:"drone_id"`
	Stream  string `json:"stream"`
	Kind    string `json:"kind"`
}

func (h *aiHub) handleTargets(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	h.detectorSeen = time.Now()
	h.mu.Unlock()
	targets := []aiTarget{}
	for _, id := range h.g.order {
		d := h.g.drones[id]
		if !h.active(d) {
			continue
		}
		for _, s := range h.streamsFor(r.Context(), d) {
			targets = append(targets, aiTarget{DroneID: id, Stream: s, Kind: streamKind(s)})
		}
	}
	writeJSON(w, 200, map[string]any{"targets": targets, "fps": h.cfg.FPS, "min_confidence": h.cfg.MinConfidence})
}

func sanitizeDetection(det *Detection) bool {
	for i, v := range det.Box {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
		det.Box[i] = clamp(v, 0, 1)
	}
	if det.Box[2] <= 0 || det.Box[3] <= 0 || det.Label == "" {
		return false
	}
	det.Label = strings.ToLower(det.Label)
	switch det.Group {
	case "person", "vehicle", "other":
	default:
		det.Group = "other"
	}
	det.Confidence = clamp(det.Confidence, 0, 1)
	return true
}

func (h *aiHub) handleDetections(w http.ResponseWriter, r *http.Request) {
	var f DetectionFrame
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512<<10))
	if err := dec.Decode(&f); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	d := h.g.drones[f.DroneID]
	if d == nil || f.Stream == "" {
		writeErr(w, http.StatusNotFound, "unknown drone or empty stream")
		return
	}
	if f.Kind != "thermal" {
		f.Kind = "visible"
	}
	if f.TS.IsZero() {
		f.TS = time.Now().UTC()
	}
	if len(f.Detections) > maxDetectionsPerFrame {
		f.Detections = f.Detections[:maxDetectionsPerFrame]
	}
	kept := f.Detections[:0]
	for _, det := range f.Detections {
		if sanitizeDetection(&det) {
			kept = append(kept, det)
		}
	}
	f.Detections = kept
	now := time.Now()

	h.mu.Lock()
	h.detectorSeen = now
	ad := h.drones[f.DroneID]
	st := ad.streams[f.Stream]
	if st == nil {
		st = &aiStream{kind: f.Kind}
		ad.streams[f.Stream] = st
	} else if dt := now.Sub(st.received).Seconds(); dt > 0 && dt < 10 {
		inst := 1 / dt
		if st.fps == 0 {
			st.fps = inst
		} else {
			st.fps = 0.8*st.fps + 0.2*inst
		}
	}
	st.kind, st.frame, st.received = f.Kind, f, now
	if fs := ad.follow; fs != nil && fs.stream == f.Stream {
		for _, det := range f.Detections {
			if det.TrackID == fs.trackID {
				fs.box, fs.seen = det.Box, now
			}
		}
	}
	if ad.autoRec {
		for _, det := range f.Detections {
			if slices.Contains(h.cfg.AutoRecord.Labels, det.Label) {
				ad.autoLast = now
				break
			}
		}
	}
	h.mu.Unlock()

	d.publish(Event{Type: "detections", Data: f})
	writeJSON(w, 200, map[string]any{"ok": true})
}

type eventIn struct {
	DroneID    string     `json:"drone_id"`
	Stream     string     `json:"stream"`
	Kind       string     `json:"kind"`
	TS         time.Time  `json:"ts"`
	TrackID    int        `json:"track_id"`
	Label      string     `json:"label"`
	Group      string     `json:"group"`
	Confidence float64    `json:"confidence"`
	Box        [4]float64 `json:"box"`
}

func (h *aiHub) handleEvent(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "multipart form required: "+err.Error())
		return
	}
	var in eventIn
	if err := json.Unmarshal([]byte(r.FormValue("event")), &in); err != nil {
		writeErr(w, http.StatusBadRequest, "event: invalid JSON: "+err.Error())
		return
	}
	d := h.g.drones[in.DroneID]
	if d == nil {
		writeErr(w, http.StatusNotFound, "unknown drone")
		return
	}
	det := Detection{TrackID: in.TrackID, Label: in.Label, Group: in.Group, Confidence: in.Confidence, Box: in.Box}
	if !sanitizeDetection(&det) {
		writeErr(w, http.StatusBadRequest, "event: invalid label or box")
		return
	}
	var snap []byte
	if f, hdr, err := r.FormFile("snapshot"); err == nil {
		defer f.Close()
		if hdr.Size > 0 {
			snap, _ = io.ReadAll(io.LimitReader(f, 8<<20))
			if len(snap) < 3 || snap[0] != 0xFF || snap[1] != 0xD8 {
				writeErr(w, http.StatusBadRequest, "snapshot must be a JPEG")
				return
			}
		}
	}
	e := h.recordEvent(d, in, det, snap)
	writeJSON(w, 200, map[string]any{"ok": true, "id": e.ID})
}

// recordEvent enriches, stores and fans out one event.
func (h *aiHub) recordEvent(d *Drone, in eventIn, det Detection, snap []byte) AIEvent {
	at := in.TS
	if at.IsZero() || time.Since(at) > time.Minute || time.Until(at) > time.Minute {
		at = time.Now().UTC() // never trust a skewed detector clock for ordering
	}
	kind := in.Kind
	if kind != "thermal" {
		kind = "visible"
	}
	e := AIEvent{
		ID: newEventID(at, d.cfg.ID, det.TrackID), DroneID: d.cfg.ID, DroneName: d.cfg.Name,
		Stream: in.Stream, Kind: kind, At: at, TrackID: det.TrackID, Label: det.Label,
		Group: det.Group, Confidence: det.Confidence, Box: det.Box,
	}
	st := d.Snapshot()
	if p := st.Position; p != nil && st.GPS != nil && st.GPS.FixType != "NO_FIX" && st.GPS.FixType != "NO_GPS" {
		pose := DronePose{Lat: p.Lat, Lon: p.Lon, AltRelM: p.AltRelM, HeadingDeg: p.HeadingDeg}
		e.Drone = &pose
		if pitch, yaw, known := d.gim.estimate(); known {
			hfov := h.cfg.Camera.VisibleHFOVDeg
			if kind == "thermal" {
				hfov = h.cfg.Camera.ThermalHFOVDeg
			}
			e.Geo = groundPoint(pose, pitch, yaw, hfov, h.aspect(d.cfg.ID, in.Stream, kind), det.Box)
		}
	}

	h.mu.Lock()
	ad := h.drones[d.cfg.ID]
	if h.alerts != nil && slices.Contains(h.cfg.Alerts.Labels, det.Label) {
		key := det.Label
		if time.Since(ad.lastAlert[key]) >= time.Duration(h.cfg.Alerts.CooldownS*float64(time.Second)) {
			ad.lastAlert[key] = time.Now()
			e.Alerted = true
		}
	}
	wantRec := len(h.cfg.AutoRecord.Labels) > 0 && slices.Contains(h.cfg.AutoRecord.Labels, det.Label)
	h.mu.Unlock()

	if wantRec {
		e.Recording = h.ensureRecording(d)
	}
	if err := h.store.add(e, snap); err != nil {
		h.log.Warn("storing event failed", "err", err, "drone", d.cfg.ID)
	} else if len(snap) > 0 {
		e.SnapshotURL = "/api/v1/ai/events/" + e.ID + "/snapshot"
	}
	d.publish(Event{Type: "ai_event", Data: e})
	if e.Alerted {
		go h.alerts.send(e, snap)
	}
	return e
}

func (h *aiHub) aspect(droneID, stream, kind string) float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ad := h.drones[droneID]; ad != nil {
		if st := ad.streams[stream]; st != nil && st.frame.Width > 0 && st.frame.Height > 0 {
			return float64(st.frame.Width) / float64(st.frame.Height)
		}
	}
	if kind == "thermal" {
		return 4.0 / 3
	}
	return 16.0 / 9
}

// ------------------------------------------------------------ auto-record

// ensureRecording starts the drone's recording unless one is running. It
// returns true when a recording is (now) active.
func (h *aiHub) ensureRecording(d *Drone) bool {
	prefix := d.cfg.StreamPrefix
	if st := h.g.rec.Status(prefix); st["is_recording"] == true {
		h.mu.Lock()
		if ad := h.drones[d.cfg.ID]; ad.autoRec {
			ad.autoLast = time.Now()
		}
		h.mu.Unlock()
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	names := h.streamsFor(ctx, d)
	if _, err := h.g.rec.Start(prefix, h.g.streamSources(names)); err != nil {
		h.log.Warn("auto-record start failed", "drone", d.cfg.ID, "err", err)
		return false
	}
	h.log.Info("auto-record started", "drone", d.cfg.ID)
	h.mu.Lock()
	ad := h.drones[d.cfg.ID]
	ad.autoRec, ad.autoLast = true, time.Now()
	h.mu.Unlock()
	return true
}

func (h *aiHub) autoRecordTick() {
	stopAfter := time.Duration(h.cfg.AutoRecord.StopAfterS * float64(time.Second))
	for id, d := range h.g.drones {
		h.mu.Lock()
		ad := h.drones[id]
		auto, last := ad.autoRec, ad.autoLast
		h.mu.Unlock()
		if !auto {
			continue
		}
		prefix := d.cfg.StreamPrefix
		running := h.g.rec.Status(prefix)["is_recording"] == true
		if running && time.Since(last) < stopAfter {
			continue
		}
		if running {
			h.g.rec.Stop(prefix)
			h.log.Info("auto-record stopped", "drone", id, "quiet_s", int(time.Since(last).Seconds()))
		}
		h.mu.Lock()
		ad.autoRec = false
		h.mu.Unlock()
	}
}

// ------------------------------------------------------------ follow

func (h *aiHub) startFollow(d *Drone, stream string, trackID int) error {
	if h.cfg.Follow.Disabled {
		return precondition("follow is disabled in the configuration")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	ad := h.drones[d.cfg.ID]
	st := ad.streams[stream]
	if st == nil || time.Since(st.received) > 3*time.Second {
		return precondition("no recent detections on that stream")
	}
	for _, det := range st.frame.Detections {
		if det.TrackID == trackID {
			ad.follow = &followState{stream: stream, trackID: trackID, box: det.Box, seen: time.Now()}
			d.publish(Event{Type: "ai_follow", Data: map[string]any{"active": true, "stream": stream, "track_id": trackID, "reason": ""}})
			return nil
		}
	}
	return precondition("that object is no longer detected")
}

// stopFollow ends following (if active) and stops the gimbal.
func (h *aiHub) stopFollow(d *Drone, reason string) {
	h.mu.Lock()
	ad := h.drones[d.cfg.ID]
	fs := ad.follow
	ad.follow = nil
	h.mu.Unlock()
	if fs == nil {
		return
	}
	_ = d.GimbalRate(0, 0)
	d.publish(Event{Type: "ai_follow", Data: map[string]any{"active": false, "stream": fs.stream, "track_id": fs.trackID, "reason": reason}})
}

// followRates turns a box offset into gimbal rates (deg/s). The video and
// the detections lag the gimbal by up to a second over 4G, so the gain is low
// and there is a dead band around the centre.
func followRates(box [4]float64, hfovDeg, aspect, maxRate float64) (pitch, yaw float64) {
	const gain, deadband = 0.6, 1.5 // 1/s, degrees
	tanH := math.Tan(toRad(hfovDeg / 2))
	tanV := tanH / aspect
	cx, cy := box[0]+box[2]/2, box[1]+box[3]/2
	errYaw := toDeg(math.Atan((cx - 0.5) * 2 * tanH))
	errPitch := -toDeg(math.Atan((cy - 0.5) * 2 * tanV)) // above centre → tilt up
	f := func(e float64) float64 {
		if math.Abs(e) < deadband {
			return 0
		}
		return clamp(gain*e, -maxRate, maxRate)
	}
	return f(errPitch), f(errYaw)
}

func (h *aiHub) followTick() {
	for id, d := range h.g.drones {
		h.mu.Lock()
		ad := h.drones[id]
		fs := ad.follow
		var box [4]float64
		var seen, frameAt time.Time
		var kind string
		if fs != nil {
			box, seen = fs.box, fs.seen
			if st := ad.streams[fs.stream]; st != nil {
				frameAt, kind = st.received, st.kind
			}
		}
		h.mu.Unlock()
		if fs == nil {
			continue
		}
		if time.Since(seen) > followLostAfter {
			h.stopFollow(d, "lost track")
			continue
		}
		if time.Since(frameAt) > followStaleAfter || time.Since(seen) > followStaleAfter {
			_ = d.GimbalRate(0, 0) // no fresh position: hold still rather than guess
			continue
		}
		hfov := h.cfg.Camera.VisibleHFOVDeg
		if kind == "thermal" {
			hfov = h.cfg.Camera.ThermalHFOVDeg
		}
		p, y := followRates(box, hfov, h.aspect(id, fs.stream, kind), h.cfg.Follow.MaxRateDegS)
		if err := d.GimbalRate(p, y); err != nil {
			h.stopFollow(d, "gimbal command failed: "+err.Error())
		}
	}
}

// operatorGimbal is called when a person moves the gimbal: their input wins.
func (g *Gateway) operatorGimbal(d *Drone) {
	if g.ai != nil {
		g.ai.stopFollow(d, "operator moved the gimbal")
	}
}

// ------------------------------------------------------------ UI side

type aiStreamStatus struct {
	Kind    string          `json:"kind"`
	FPS     float64         `json:"fps"`
	InferMS float64         `json:"infer_ms"`
	Counts  map[string]int  `json:"counts"`
	Frame   *DetectionFrame `json:"frame"`
}

func (h *aiHub) status(d *Drone) map[string]any {
	active := h.active(d)
	h.mu.Lock()
	defer h.mu.Unlock()
	ad := h.drones[d.cfg.ID]
	streams := map[string]aiStreamStatus{}
	var last time.Time
	for name, st := range ad.streams {
		if time.Since(st.received) > time.Minute {
			continue
		}
		counts := map[string]int{"person": 0, "vehicle": 0, "other": 0}
		fresh := time.Since(st.received) < 3*time.Second
		if fresh {
			for _, det := range st.frame.Detections {
				counts[det.Group]++
			}
		}
		fr := st.frame
		streams[name] = aiStreamStatus{Kind: st.kind, FPS: round(st.fps, 1), InferMS: round(st.frame.InferMS, 1), Counts: counts, Frame: &fr}
		if st.received.After(last) {
			last = st.received
		}
	}
	follow := map[string]any{"active": false, "stream": "", "track_id": 0}
	if fs := ad.follow; fs != nil {
		follow = map[string]any{"active": true, "stream": fs.stream, "track_id": fs.trackID}
	}
	var lastAt any
	if !last.IsZero() {
		lastAt = last.UTC()
	}
	return map[string]any{
		"enabled": ad.enabled, "configured": true,
		"detector_online": time.Since(h.detectorSeen) < detectorOfflineAfter,
		"active":          active, "last_frame_at": lastAt, "streams": streams, "follow": follow,
		"features": map[string]bool{
			"analyze": h.vision != nil, "alerts": h.alerts != nil,
			"auto_record": len(h.cfg.AutoRecord.Labels) > 0, "follow": !h.cfg.Follow.Disabled,
		},
	}
}

func (h *aiHub) setEnabled(d *Drone, on bool) {
	h.mu.Lock()
	h.drones[d.cfg.ID].enabled = on
	h.mu.Unlock()
	if !on {
		h.stopFollow(d, "AI turned off")
	}
}

// visibleStream picks the stream to analyze when none is named.
func (h *aiHub) visibleStream(ctx context.Context, d *Drone) string {
	names := h.streamsFor(ctx, d)
	for _, n := range names {
		if streamKind(n) == "visible" {
			return n
		}
	}
	if len(names) > 0 {
		return names[0]
	}
	return ""
}

func parseTimeParam(v string) (time.Time, error) {
	if v == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, v)
}

// aiRoutes registers docs/09-ai.md. Without ai.enabled the UI endpoints
// report "not configured" and the detector endpoints refuse.
func (g *Gateway) aiRoutes(mux *http.ServeMux) {
	V, O := roleViewer, roleOperator
	notConfigured := func(w http.ResponseWriter) {
		writeErr(w, http.StatusServiceUnavailable, "AI is not configured (ai.enabled in gateway.yaml)")
	}
	ingest := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if g.ai == nil {
				notConfigured(w)
				return
			}
			if !g.ai.ingestAuthorized(r) {
				writeErr(w, http.StatusUnauthorized, "invalid AI ingest token")
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("GET /api/v1/ai/targets", ingest(func(w http.ResponseWriter, r *http.Request) { g.ai.handleTargets(w, r) }))
	mux.HandleFunc("POST /api/v1/ai/detections", ingest(func(w http.ResponseWriter, r *http.Request) { g.ai.handleDetections(w, r) }))
	mux.HandleFunc("POST /api/v1/ai/events", ingest(func(w http.ResponseWriter, r *http.Request) { g.ai.handleEvent(w, r) }))

	mux.HandleFunc("GET /api/v1/drones/{id}/ai", g.route(V, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		if g.ai == nil {
			writeJSON(w, 200, map[string]any{"enabled": false, "configured": false, "detector_online": false, "active": false,
				"last_frame_at": nil, "streams": map[string]any{}, "follow": map[string]any{"active": false, "stream": "", "track_id": 0},
				"features": map[string]bool{"analyze": false, "alerts": false, "auto_record": false, "follow": false}})
			return
		}
		writeJSON(w, 200, g.ai.status(d))
	}))
	mux.HandleFunc("POST /api/v1/drones/{id}/ai", g.route(O, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		if g.ai == nil {
			notConfigured(w)
			return
		}
		var b struct {
			Enabled *bool `json:"enabled"`
		}
		if !decode(w, r, &b) {
			return
		}
		if b.Enabled == nil {
			writeErr(w, http.StatusBadRequest, "enabled (true or false) is required")
			return
		}
		g.ai.setEnabled(d, *b.Enabled)
		writeJSON(w, 200, map[string]any{"ok": true, "enabled": *b.Enabled})
	}))
	mux.HandleFunc("POST /api/v1/drones/{id}/ai/follow", g.route(O, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		if g.ai == nil {
			notConfigured(w)
			return
		}
		var b struct {
			Stream  string `json:"stream"`
			TrackID int    `json:"track_id"`
		}
		if !decode(w, r, &b) {
			return
		}
		writeResult(w, g.ai.startFollow(d, b.Stream, b.TrackID), nil)
	}))
	mux.HandleFunc("DELETE /api/v1/drones/{id}/ai/follow", g.route(O, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		if g.ai != nil {
			g.ai.stopFollow(d, "stopped by operator")
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	}))
	mux.HandleFunc("POST /api/v1/drones/{id}/ai/analyze", g.route(O, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		if g.ai == nil || g.ai.vision == nil {
			writeErr(w, http.StatusServiceUnavailable, "frame analysis is not configured (ai.vision.api_key)")
			return
		}
		var b struct {
			Stream string `json:"stream"`
			Prompt string `json:"prompt"`
		}
		if !decode(w, r, &b) {
			return
		}
		if len(b.Prompt) > 2000 {
			writeErr(w, http.StatusBadRequest, "prompt is limited to 2000 characters")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()
		if b.Stream == "" {
			b.Stream = g.ai.visibleStream(ctx, d)
		}
		res, err := g.ai.analyze(ctx, d, b.Stream, b.Prompt)
		if err != nil {
			var ve *visionError
			if errors.As(err, &ve) {
				writeErr(w, ve.status, ve.msg)
				return
			}
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, 200, res)
	}))

	mux.HandleFunc("GET /api/v1/ai/events", g.authed(V, func(w http.ResponseWriter, r *http.Request) {
		if g.ai == nil {
			writeJSON(w, 200, map[string]any{"events": []AIEvent{}})
			return
		}
		q := r.URL.Query()
		since, err1 := parseTimeParam(q.Get("since"))
		until, err2 := parseTimeParam(q.Get("until"))
		if err1 != nil || err2 != nil {
			writeErr(w, http.StatusBadRequest, "since/until must be RFC 3339 times")
			return
		}
		limit, _ := strconv.Atoi(q.Get("limit"))
		writeJSON(w, 200, map[string]any{"events": g.ai.store.query(eventQuery{
			Drone: q.Get("drone"), Group: q.Get("group"), Label: strings.ToLower(q.Get("label")),
			Since: since, Until: until, Limit: limit,
		})})
	}))
	mux.HandleFunc("GET /api/v1/ai/events/{eid}/snapshot", g.authed(V, func(w http.ResponseWriter, r *http.Request) {
		if g.ai == nil {
			notConfigured(w)
			return
		}
		p, ok := g.ai.store.snapshot(r.PathValue("eid"))
		if !ok {
			writeErr(w, http.StatusNotFound, "snapshot not found")
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "private, max-age=86400")
		http.ServeFile(w, r, p)
	}))
}
