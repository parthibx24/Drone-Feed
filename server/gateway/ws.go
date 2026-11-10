package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// wsIn is a message from the browser.
type wsIn struct {
	Type      string  `json:"type"` // "gimbal_rate"
	PitchDegS float64 `json:"pitch_deg_s"`
	YawDegS   float64 `json:"yaw_deg_s"`
}

// serveWS streams telemetry at telemetry_hz plus events (statustext, link,
// mission_item_reached) as they happen. Operators can also stream
// {"type":"gimbal_rate","pitch_deg_s":..,"yaw_deg_s":..} for hold-to-move.
func (g *Gateway) serveWS(w http.ResponseWriter, r *http.Request, d *Drone) {
	opts := &websocket.AcceptOptions{OriginPatterns: g.wsOrigins()}
	if len(opts.OriginPatterns) == 1 && opts.OriginPatterns[0] == "*" {
		opts.InsecureSkipVerify = true
	}
	c, err := websocket.Accept(w, r, opts)
	if err != nil {
		return
	}
	defer c.CloseNow()
	a, _ := r.Context().Value(actorKey).(actor)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	events, unlisten := d.listen()
	defer unlisten()

	var movedGimbal atomic.Bool
	defer func() {
		if movedGimbal.Load() {
			_ = d.GimbalRate(0, 0)
		}
	}()

	go func() {
		defer cancel()
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			var in wsIn
			if json.Unmarshal(data, &in) != nil {
				continue
			}
			if in.Type == "gimbal_rate" && a.role == roleOperator {
				movedGimbal.Store(true)
				_ = d.GimbalRate(in.PitchDegS, in.YawDegS)
			}
		}
	}()

	tick := time.NewTicker(time.Duration(float64(time.Second) / g.cfg.TelemetryHz))
	defer tick.Stop()
	send := func(v any) bool {
		wctx, wcancel := context.WithTimeout(ctx, 5*time.Second)
		defer wcancel()
		return wsjson.Write(wctx, c, v) == nil
	}
	if !send(Event{Type: "telemetry", Data: d.Snapshot()}) {
		return
	}
	for {
		select {
		case <-ctx.Done():
			c.Close(websocket.StatusNormalClosure, "")
			return
		case e := <-events:
			if !send(e) {
				return
			}
		case <-tick.C:
			if !send(Event{Type: "telemetry", Data: d.Snapshot()}) {
				return
			}
		}
	}
}

func (g *Gateway) wsOrigins() []string {
	var out []string
	for _, o := range g.cfg.CORSOrigins {
		if o == "*" {
			return []string{"*"}
		}
		// coder/websocket matches host patterns, not full origins.
		if u := hostOf(o); u != "" {
			out = append(out, u)
		}
	}
	return out
}

func hostOf(origin string) string {
	for _, p := range []string{"https://", "http://"} {
		if len(origin) > len(p) && origin[:len(p)] == p {
			return origin[len(p):]
		}
	}
	return origin
}
