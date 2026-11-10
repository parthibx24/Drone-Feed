package gateway

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/bluenviron/gomavlib/v3/pkg/dialects/common"

	"github.com/parthibx24/Drone-Feed/internal/mav"
	"github.com/parthibx24/Drone-Feed/server/recorder"
)

const (
	roleViewer   = "viewer"
	roleOperator = "operator"
	cmdTimeout   = 20 * time.Second
	maxBody      = 1 << 20
)

// Gateway owns all drone links and the HTTP API.
type Gateway struct {
	cfg     *Config
	log     *slog.Logger
	audit   *slog.Logger
	drones  map[string]*Drone
	order   []string
	version string
	auth    *authState
	rec     *recorder.Manager
}

// New creates links for every configured drone.
func New(cfg *Config, log *slog.Logger, version string) (*Gateway, error) {
	g := &Gateway{cfg: cfg, log: log, audit: log.With("audit", true), drones: map[string]*Drone{}, version: version}
	for _, dc := range cfg.Drones {
		d, err := newDrone(dc, cfg, log)
		if err != nil {
			return nil, err
		}
		g.drones[dc.ID] = d
		g.order = append(g.order, dc.ID)
	}
	auth, generated := newAuthState(cfg)
	g.auth = auth
	switch {
	case g.authOpen():
		log.Warn("no users or tokens configured: the API and UI are OPEN to anyone who can reach them (development only)")
	case generated && len(cfg.Auth.Users) > 0:
		log.Warn("auth.session_secret not set: using a random one, so everyone is logged out on restart")
	}
	rec, err := recorder.New(cfg.RecordingsDir, log.With("component", "recorder"))
	if err != nil {
		return nil, fmt.Errorf("recordings_dir: %w", err)
	}
	g.rec = rec
	return g, nil
}

// Run serves until ctx is cancelled.
func (g *Gateway) Run(ctx context.Context) error {
	g.StartLinks(ctx)
	srv := &http.Server{Addr: g.cfg.Listen, Handler: g.Handler(), ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	g.log.Info("drone-gateway listening", "addr", g.cfg.Listen, "drones", len(g.drones), "version", g.version)
	defer g.rec.Shutdown() // finalise any recording in progress
	select {
	case <-ctx.Done():
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(sctx)
	case err := <-errc:
		return err
	}
}

// StartLinks starts every drone's MAVLink loop (Run calls it).
func (g *Gateway) StartLinks(ctx context.Context) {
	for _, d := range g.drones {
		go d.run(ctx)
	}
}

// ---------------------------------------------------------------- plumbing

type ctxKey int

const actorKey ctxKey = 0

type actor struct {
	name, role string
	viaCookie  bool
}

// authenticate accepts, in order: an API token (Authorization: Bearer, or
// ?token= for WebSockets from scripts) or a web login session cookie.
func (g *Gateway) authenticate(r *http.Request) (actor, bool) {
	if g.authOpen() {
		return actor{name: "anonymous", role: roleOperator}, true
	}
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if tok == "" {
		tok = r.URL.Query().Get("token")
	}
	if tok != "" {
		for _, t := range g.cfg.Tokens {
			if subtle.ConstantTimeCompare([]byte(tok), []byte(t.Token)) == 1 {
				return actor{name: t.Name, role: t.Role}, true
			}
		}
		return actor{}, false
	}
	if c, err := r.Cookie(sessionCookie); err == nil {
		if u, ok := g.auth.verify(c.Value); ok {
			return actor{name: u.Username, role: u.Role, viaCookie: true}, true
		}
	}
	return actor{}, false
}

// authed wraps a handler that is not tied to one drone.
func (g *Gateway) authed(role string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a, ok := g.authorize(w, r, role)
		if !ok {
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), actorKey, a))
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			h(w, r)
			return
		}
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		start := time.Now()
		h(rec, r)
		g.audit.Info("command", "actor", a.name, "method", r.Method, "path", r.URL.Path,
			"status", rec.status, "error", rec.errMsg, "duration_ms", time.Since(start).Milliseconds())
	}
}

// authorize checks identity, role and (for cookie sessions) the CSRF header.
func (g *Gateway) authorize(w http.ResponseWriter, r *http.Request, role string) (actor, bool) {
	a, ok := g.authenticate(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "not logged in")
		return a, false
	}
	if role == roleOperator && a.role != roleOperator {
		writeErr(w, http.StatusForbidden, "operator role required")
		return a, false
	}
	if !csrfOK(r, a) {
		writeErr(w, http.StatusForbidden, "missing "+csrfHeader+" header")
		return a, false
	}
	return a, true
}

func (g *Gateway) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && g.originAllowed(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, "+csrfHeader)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (g *Gateway) originAllowed(origin string) bool {
	for _, o := range g.cfg.CORSOrigins {
		if o == "*" || strings.EqualFold(o, origin) {
			return true
		}
	}
	return false
}

// route wraps a handler with auth, the drone lookup, and auditing for writes.
func (g *Gateway) route(role string, h func(w http.ResponseWriter, r *http.Request, d *Drone)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a, ok := g.authorize(w, r, role)
		if !ok {
			return
		}
		d := g.drones[r.PathValue("id")]
		if d == nil {
			writeErr(w, http.StatusNotFound, "unknown drone")
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), actorKey, a))
		if r.Method == http.MethodGet {
			h(w, r, d)
			return
		}
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		start := time.Now()
		h(rec, r, d)
		g.audit.Info("command", "actor", a.name, "drone", d.cfg.ID, "method", r.Method, "path", r.URL.Path,
			"status", rec.status, "error", rec.errMsg, "duration_ms", time.Since(start).Milliseconds())
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	errMsg string
}

func (s *statusRecorder) WriteHeader(code int) { s.status = code; s.ResponseWriter.WriteHeader(code) }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	if rec, ok := w.(*statusRecorder); ok {
		rec.errMsg = msg
	}
	writeJSON(w, status, map[string]any{"ok": false, "error": msg})
}

// writeResult maps a command outcome to an HTTP status.
func writeResult(w http.ResponseWriter, err error, extra map[string]any) {
	if err == nil {
		body := map[string]any{"ok": true}
		for k, v := range extra {
			body[k] = v
		}
		writeJSON(w, http.StatusOK, body)
		return
	}
	var ce *CommandError
	var me *MissionError
	switch {
	case errors.As(err, &ce):
		if rec, ok := w.(*statusRecorder); ok {
			rec.errMsg = err.Error()
		}
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": err.Error(), "result": shortResult(ce.Result)})
	case errors.As(err, &me), errors.Is(err, ErrRejected):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrPrecondition):
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, ErrOffline), errors.Is(err, ErrNotIdentified):
		writeErr(w, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		writeErr(w, http.StatusGatewayTimeout, err.Error())
	default:
		writeErr(w, http.StatusBadGateway, err.Error())
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.ContentLength == 0 {
		return true // all bodies are optional or validated afterwards
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

func cmdCtx(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), cmdTimeout)
}

// simple wires a handler that takes no body and returns only ok/error.
func simple(f func(*Drone, context.Context) error) func(http.ResponseWriter, *http.Request, *Drone) {
	return func(w http.ResponseWriter, r *http.Request, d *Drone) {
		ctx, cancel := cmdCtx(r)
		defer cancel()
		writeResult(w, f(d, ctx), nil)
	}
}

func nanIfNil(p *float64) float32 {
	if p == nil {
		return mav.NaN
	}
	return float32(*p)
}

// ---------------------------------------------------------------- routes

// Handler returns the HTTP API.
func (g *Gateway) Handler() http.Handler {
	mux := http.NewServeMux()
	V, O := roleViewer, roleOperator

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"ok": true, "version": g.version})
	})
	mux.HandleFunc("GET /api/v1/drones", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := g.authorize(w, r, roleViewer); !ok {
			return
		}
		out := make([]map[string]any, 0, len(g.order))
		for _, id := range g.order {
			d := g.drones[id]
			st := d.Snapshot()
			out = append(out, map[string]any{
				"id": id, "name": d.cfg.Name, "online": st.Online, "armed": st.Armed, "mode": st.Mode,
				"stream_prefix": d.cfg.StreamPrefix, "telemetry": st,
			})
		}
		writeJSON(w, 200, out)
	})

	// --- auth (login is the only unauthenticated write)
	mux.HandleFunc("POST /api/auth/login", g.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", g.handleLogout)
	mux.HandleFunc("GET /api/auth/me", g.handleMe)
	mux.HandleFunc("GET /api/v1/limits", g.authed(roleViewer, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, g.cfg.Limits)
	}))

	g.mediaRoutes(mux)

	// --- read
	mux.HandleFunc("GET /api/v1/drones/{id}", g.route(V, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		writeJSON(w, 200, d.Snapshot())
	}))
	mux.HandleFunc("GET /api/v1/drones/{id}/statustext", g.route(V, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		writeJSON(w, 200, d.StatusTexts())
	}))
	mux.HandleFunc("GET /api/v1/drones/{id}/modes", g.route(V, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		writeJSON(w, 200, mav.CopterModeNames())
	}))
	mux.HandleFunc("GET /api/v1/drones/{id}/ws", g.route(V, g.serveWS))

	// --- flight
	mux.HandleFunc("POST /api/v1/drones/{id}/arm", g.route(O, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		var b struct {
			Force bool `json:"force"`
		}
		if !decode(w, r, &b) {
			return
		}
		ctx, cancel := cmdCtx(r)
		defer cancel()
		writeResult(w, d.Arm(ctx, b.Force), nil)
	}))
	mux.HandleFunc("POST /api/v1/drones/{id}/disarm", g.route(O, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		var b struct {
			Force bool `json:"force"`
		}
		if !decode(w, r, &b) {
			return
		}
		ctx, cancel := cmdCtx(r)
		defer cancel()
		writeResult(w, d.Disarm(ctx, b.Force), nil)
	}))
	mux.HandleFunc("POST /api/v1/drones/{id}/mode", g.route(O, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		var b struct {
			Mode string `json:"mode"`
		}
		if !decode(w, r, &b) {
			return
		}
		ctx, cancel := cmdCtx(r)
		defer cancel()
		writeResult(w, d.SetMode(ctx, b.Mode), map[string]any{"mode": strings.ToUpper(b.Mode)})
	}))
	mux.HandleFunc("POST /api/v1/drones/{id}/takeoff", g.route(O, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		var b struct {
			AltM float64 `json:"alt_m"`
		}
		if !decode(w, r, &b) {
			return
		}
		ctx, cancel := cmdCtx(r)
		defer cancel()
		writeResult(w, d.Takeoff(ctx, b.AltM), nil)
	}))
	mux.HandleFunc("POST /api/v1/drones/{id}/land", g.route(O, simple((*Drone).Land)))
	mux.HandleFunc("POST /api/v1/drones/{id}/rtl", g.route(O, simple((*Drone).RTL)))
	mux.HandleFunc("POST /api/v1/drones/{id}/pause", g.route(O, simple((*Drone).Pause)))
	mux.HandleFunc("POST /api/v1/drones/{id}/resume", g.route(O, simple((*Drone).Resume)))
	mux.HandleFunc("POST /api/v1/drones/{id}/reboot", g.route(O, simple((*Drone).RebootAutopilot)))
	mux.HandleFunc("POST /api/v1/drones/{id}/goto", g.route(O, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		var b struct {
			Lat     float64  `json:"lat"`
			Lon     float64  `json:"lon"`
			AltM    float64  `json:"alt_m"`
			SpeedMS *float64 `json:"speed_m_s"`
		}
		if !decode(w, r, &b) {
			return
		}
		ctx, cancel := cmdCtx(r)
		defer cancel()
		writeResult(w, d.Goto(ctx, b.Lat, b.Lon, b.AltM, b.SpeedMS), nil)
	}))
	mux.HandleFunc("POST /api/v1/drones/{id}/speed", g.route(O, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		var b struct {
			SpeedMS float64 `json:"speed_m_s"`
		}
		if !decode(w, r, &b) {
			return
		}
		ctx, cancel := cmdCtx(r)
		defer cancel()
		writeResult(w, d.ChangeSpeed(ctx, b.SpeedMS), nil)
	}))
	mux.HandleFunc("POST /api/v1/drones/{id}/roi", g.route(O, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		var b struct {
			Lat  float64 `json:"lat"`
			Lon  float64 `json:"lon"`
			AltM float64 `json:"alt_m"`
		}
		if !decode(w, r, &b) {
			return
		}
		ctx, cancel := cmdCtx(r)
		defer cancel()
		writeResult(w, d.SetROI(ctx, b.Lat, b.Lon, b.AltM), nil)
	}))
	mux.HandleFunc("DELETE /api/v1/drones/{id}/roi", g.route(O, simple((*Drone).ClearROI)))

	// --- mission
	mux.HandleFunc("GET /api/v1/drones/{id}/mission", g.route(V, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		ctx, cancel := cmdCtx(r)
		defer cancel()
		items, err := d.DownloadMission(ctx)
		if err != nil {
			writeResult(w, err, nil)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "items": DescribeMission(items)})
	}))
	mux.HandleFunc("PUT /api/v1/drones/{id}/mission", g.route(O, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		var b struct {
			Items []MissionItem `json:"items"`
		}
		if !decode(w, r, &b) {
			return
		}
		items, err := d.BuildMission(b.Items)
		if err != nil {
			writeErr(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second+time.Duration(len(items))*500*time.Millisecond)
		defer cancel()
		writeResult(w, d.UploadMission(ctx, items), map[string]any{"count": len(b.Items)})
	}))
	mux.HandleFunc("DELETE /api/v1/drones/{id}/mission", g.route(O, simple((*Drone).ClearMission)))
	mux.HandleFunc("POST /api/v1/drones/{id}/mission/start", g.route(O, simple((*Drone).MissionStart)))
	mux.HandleFunc("POST /api/v1/drones/{id}/mission/current", g.route(O, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		var b struct {
			Seq int `json:"seq"`
		}
		if !decode(w, r, &b) {
			return
		}
		ctx, cancel := cmdCtx(r)
		defer cancel()
		writeResult(w, d.SetMissionCurrent(ctx, b.Seq), nil)
	}))

	// --- parameters
	mux.HandleFunc("GET /api/v1/drones/{id}/params", g.route(V, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		// A full download loads the drone's serial link for ~20 s, so forcing
		// one is for operators; viewers get the cache (or the first download).
		refresh := r.URL.Query().Get("refresh") == "1"
		if refresh && len(d.CachedParams()) > 0 {
			if _, ok := g.authorize(w, r, roleOperator); !ok {
				return
			}
		}
		if !refresh {
			if cached := d.CachedParams(); len(cached) > 0 {
				writeJSON(w, 200, map[string]any{"ok": true, "cached": true, "params": cached})
				return
			}
		}
		ps, err := d.FetchParams(r.Context())
		if err != nil {
			writeResult(w, err, nil)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "cached": false, "params": ps})
	}))
	mux.HandleFunc("GET /api/v1/drones/{id}/params/{name}", g.route(V, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		ctx, cancel := cmdCtx(r)
		defer cancel()
		p, err := d.GetParam(ctx, strings.ToUpper(r.PathValue("name")))
		if err != nil {
			writeResult(w, err, nil)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "param": p})
	}))
	mux.HandleFunc("PUT /api/v1/drones/{id}/params/{name}", g.route(O, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		var b struct {
			Value *float64 `json:"value"`
		}
		if !decode(w, r, &b) {
			return
		}
		if b.Value == nil {
			writeErr(w, http.StatusBadRequest, "value is required")
			return
		}
		ctx, cancel := cmdCtx(r)
		defer cancel()
		p, err := d.SetParam(ctx, strings.ToUpper(r.PathValue("name")), float32(*b.Value))
		writeResult(w, err, map[string]any{"param": p})
	}))

	// --- gimbal
	mux.HandleFunc("POST /api/v1/drones/{id}/gimbal/attitude", g.route(O, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		var b struct {
			PitchDeg *float64 `json:"pitch_deg"`
			YawDeg   *float64 `json:"yaw_deg"`
		}
		if !decode(w, r, &b) {
			return
		}
		if b.PitchDeg == nil && b.YawDeg == nil {
			writeErr(w, http.StatusBadRequest, "pitch_deg and/or yaw_deg required")
			return
		}
		ctx, cancel := cmdCtx(r)
		defer cancel()
		writeResult(w, d.GimbalAttitude(ctx, nanIfNil(b.PitchDeg), nanIfNil(b.YawDeg)), nil)
	}))
	mux.HandleFunc("POST /api/v1/drones/{id}/gimbal/rate", g.route(O, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		var b struct {
			PitchDegS float64 `json:"pitch_deg_s"`
			YawDegS   float64 `json:"yaw_deg_s"`
		}
		if !decode(w, r, &b) {
			return
		}
		writeResult(w, d.GimbalRate(b.PitchDegS, b.YawDegS), map[string]any{"acknowledged": false})
	}))
	mux.HandleFunc("POST /api/v1/drones/{id}/gimbal/center", g.route(O, simple((*Drone).GimbalCenter)))
	mux.HandleFunc("POST /api/v1/drones/{id}/gimbal/mode", g.route(O, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		var b struct {
			Mode string `json:"mode"`
		}
		if !decode(w, r, &b) {
			return
		}
		ctx, cancel := cmdCtx(r)
		defer cancel()
		writeResult(w, d.GimbalMode(ctx, b.Mode), nil)
	}))

	// --- camera
	mux.HandleFunc("POST /api/v1/drones/{id}/camera/photo", g.route(O, simple((*Drone).Photo)))
	mux.HandleFunc("POST /api/v1/drones/{id}/camera/photo/interval", g.route(O, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		var b struct {
			Seconds float64 `json:"seconds"`
			Count   int     `json:"count"`
		}
		if !decode(w, r, &b) {
			return
		}
		ctx, cancel := cmdCtx(r)
		defer cancel()
		writeResult(w, d.PhotoInterval(ctx, b.Seconds, b.Count), nil)
	}))
	mux.HandleFunc("POST /api/v1/drones/{id}/camera/photo/stop", g.route(O, simple((*Drone).PhotoStop)))
	mux.HandleFunc("POST /api/v1/drones/{id}/camera/video/start", g.route(O, simple(func(d *Drone, ctx context.Context) error { return d.Video(ctx, true) })))
	mux.HandleFunc("POST /api/v1/drones/{id}/camera/video/stop", g.route(O, simple(func(d *Drone, ctx context.Context) error { return d.Video(ctx, false) })))
	mux.HandleFunc("POST /api/v1/drones/{id}/camera/zoom", g.route(O, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		var b struct {
			Type  string  `json:"type"`
			Value float64 `json:"value"`
		}
		if !decode(w, r, &b) {
			return
		}
		ctx, cancel := cmdCtx(r)
		defer cancel()
		writeResult(w, d.Zoom(ctx, b.Type, b.Value), nil)
	}))

	// --- raw escape hatch (disabled unless allow_raw_commands: true)
	mux.HandleFunc("POST /api/v1/drones/{id}/command", g.route(O, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		if !g.cfg.AllowRawCommands {
			writeErr(w, http.StatusForbidden, "raw commands are disabled (allow_raw_commands: false)")
			return
		}
		var b struct {
			Command   uint16      `json:"command"`
			Component *uint8      `json:"component"`
			Params    [7]*float64 `json:"params"`
		}
		if !decode(w, r, &b) {
			return
		}
		comp := mav.CompAutopilot
		if b.Component != nil {
			comp = *b.Component
		}
		var p [7]float32 // null params are sent as NaN ("unused"), as MAVLink expects
		for i, v := range b.Params {
			p[i] = nanIfNil(v)
		}
		ctx, cancel := cmdCtx(r)
		defer cancel()
		res, err := d.CommandLong(ctx, comp, common.MAV_CMD(b.Command), p)
		writeResult(w, err, map[string]any{"result": shortResult(res)})
	}))

	if g.cfg.WebDir != "" {
		mux.Handle("/", spa(g.cfg.WebDir))
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			writeErr(w, http.StatusNotFound, "not found (web_dir not configured)")
		})
	}
	return securityHeaders(g.cors(mux))
}

// String is used in logs.
func (g *Gateway) String() string { return fmt.Sprintf("gateway(%d drones)", len(g.drones)) }
