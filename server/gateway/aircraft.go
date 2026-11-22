package gateway

// The aircraft's functions beyond MAVLink, for drones with an aircraft API
// (DroneConfig.Bridge: dji-bridge's, for the Mavic 3T). As for the speaker,
// the gateway checks who may do what and proxies.
//
// Upstream contract (dji-bridge's operator API, bearer token):
//
//	GET  {api}/features  → {"features":[…],"thermal","avoidance","rtk","online"}
//	POST {api}/commands  {"kind", …fields of that kind}
//	                     → 200 {"ok":true,"kind"} or {"error"} with 4xx/5xx
//	                       (409/502 when the aircraft or the pilot refuses)

import (
	"encoding/json"
	"net/http"
	"time"
)

const (
	aircraftTimeout = 15 * time.Second
	aircraftMaxBody = 4 << 10
)

// aircraftKinds are the commands the dashboard may send through
// POST /aircraft-command. Flight, gimbal and camera commands go through the
// MAVLink routes, where the gateway applies its own checks; this list keeps
// that the only way to fly.
var aircraftKinds = map[string]bool{
	"set_metering":       true,
	"meter_spot":         true,
	"meter_area":         true,
	"thermal_palette":    true,
	"thermal_gain":       true,
	"thermal_isotherm":   true,
	"obstacle_avoidance": true,
	"rtk_enable":         true,
}

// bridgeFor returns the drone's aircraft API, or answers 404.
func bridgeFor(w http.ResponseWriter, d *Drone) (*BridgeConfig, bool) {
	if d.cfg.Bridge == nil {
		writeErr(w, http.StatusNotFound, "no aircraft features")
		return nil, false
	}
	return d.cfg.Bridge, true
}

func (g *Gateway) aircraftRoutes(mux *http.ServeMux) {
	V, O := roleViewer, roleOperator

	mux.HandleFunc("GET /api/v1/drones/{id}/features", g.route(V, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		if b, ok := bridgeFor(w, d); ok {
			upstreamProxy(w, r, "aircraft service", b.API, b.Token, http.MethodGet, "/features", nil, aircraftTimeout)
		}
	}))

	mux.HandleFunc("POST /api/v1/drones/{id}/aircraft-command", g.route(O, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		b, ok := bridgeFor(w, d)
		if !ok {
			return
		}
		// The body is forwarded as it is (its fields depend on the kind and
		// the bridge validates them); only the kind is checked here.
		var cmd map[string]any
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, aircraftMaxBody))
		if err := dec.Decode(&cmd); err != nil || cmd == nil {
			writeErr(w, http.StatusBadRequest, "body must be a JSON object with a kind")
			return
		}
		kind, _ := cmd["kind"].(string)
		if !aircraftKinds[kind] {
			writeErr(w, http.StatusBadRequest, "unknown aircraft command kind "+quoteKind(kind))
			return
		}
		// The audit line written after this names the path only: say which.
		g.audit.Info("aircraft command", "drone", d.cfg.ID, "kind", kind)
		upstreamProxy(w, r, "aircraft service", b.API, b.Token, http.MethodPost, "/commands", cmd, aircraftTimeout)
	}))
}

func quoteKind(k string) string {
	if k == "" {
		return "(none)"
	}
	if len(k) > 40 {
		k = k[:40] + "…"
	}
	return `"` + k + `"`
}
