package gateway_test

// End-to-end test of the whole stack without hardware:
//
//   HTTP client ─► drone-gateway ─UDP─► sim vehicle (autopilot + router) ─UDP─► gimbal-bridge ─UDP─► fake C12
//
// The fake C12 is a UDP socket that records every packet the bridge sends.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bluenviron/gomavlib/v3"
	"github.com/bluenviron/gomavlib/v3/pkg/dialects/common"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/parthibx24/Drone-Feed/drone/bridge"
	"github.com/parthibx24/Drone-Feed/drone/skydroid"
	"github.com/parthibx24/Drone-Feed/internal/sim"
	"github.com/parthibx24/Drone-Feed/server/gateway"
)

const (
	opToken     = "op-secret"
	viewerToken = "view-secret"
)

func freePort(t *testing.T) int {
	t.Helper()
	c, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).Port
}

// fakeC12 records UDP packets sent to the "gimbal".
type fakeC12 struct {
	conn *net.UDPConn
	mu   sync.Mutex
	pkts []string
}

func newFakeC12(t *testing.T) *fakeC12 {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeC12{conn: conn}
	go func() {
		buf := make([]byte, 2048)
		for {
			n, _, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			f.mu.Lock()
			f.pkts = append(f.pkts, string(buf[:n]))
			f.mu.Unlock()
		}
	}()
	return f
}

func (f *fakeC12) port() int { return f.conn.LocalAddr().(*net.UDPAddr).Port }

func (f *fakeC12) reset() { f.mu.Lock(); f.pkts = nil; f.mu.Unlock() }

func (f *fakeC12) packets() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.pkts...)
}

func (f *fakeC12) waitFor(t *testing.T, want string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		for _, p := range f.packets() {
			if p == want {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("C12 never received %q; got %q", want, f.packets())
}

type stack struct {
	t       *testing.T
	api     *httptest.Server
	vehicle *sim.Vehicle
	c12     *fakeC12
}

func startStack(t *testing.T) *stack {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	log := slog.New(slog.NewTextHandler(testLogOut(), &slog.HandlerOptions{Level: slog.LevelDebug}))

	gwPort, bridgePort := freePort(t), freePort(t)

	// Vehicle: UDP server for the gateway, UDP client towards the bridge, routing between them.
	v, err := sim.New([]gomavlib.EndpointConf{
		gomavlib.EndpointUDPServer{Address: fmt.Sprintf("127.0.0.1:%d", gwPort)},
		gomavlib.EndpointUDPClient{Address: fmt.Sprintf("127.0.0.1:%d", bridgePort)},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(v.Close)
	go v.Run(ctx)

	c12 := newFakeC12(t)
	t.Cleanup(func() { c12.conn.Close() })
	g, err := skydroid.Dial("127.0.0.1", c12.port(), 0, skydroid.SpeedLegacy)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { g.Close() })
	b, err := bridge.New(bridge.Config{
		Endpoint: gomavlib.EndpointUDPServer{Address: fmt.Sprintf("127.0.0.1:%d", bridgePort)},
		Gimbal:   g, RateTimeout: 400 * time.Millisecond, Log: log,
		Streams: []bridge.Stream{{Name: "visible", URI: "rtsp://example/visible"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	go b.Run(ctx)

	cfg := &gateway.Config{
		Listen:        "127.0.0.1:0",
		RecordingsDir: t.TempDir(),
		Tokens:        []gateway.Token{{Name: "ops", Token: opToken, Role: "operator"}, {Name: "feed", Token: viewerToken, Role: "viewer"}},
		Drones:        []gateway.DroneConfig{{ID: "sim", MAVLink: fmt.Sprintf("udp:127.0.0.1:%d", gwPort)}},
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	gw, err := gateway.New(cfg, log, "test")
	if err != nil {
		t.Fatal(err)
	}
	gw.StartLinks(ctx)
	api := httptest.NewServer(gw.Handler())
	t.Cleanup(api.Close)

	s := &stack{t: t, api: api, vehicle: v, c12: c12}
	s.waitTelemetry(func(m map[string]any) bool {
		cam, _ := m["camera"].(map[string]any)
		gim, _ := m["gimbal"].(map[string]any)
		return m["online"] == true && m["firmware"] == "4.7.0" && cam["present"] == true && gim["present"] == true
	}, 8*time.Second)
	return s
}

func (s *stack) do(method, path, token string, body any) (int, map[string]any) {
	s.t.Helper()
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, s.api.URL+path, rdr)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func (s *stack) waitTelemetry(ok func(map[string]any) bool, within time.Duration) map[string]any {
	s.t.Helper()
	deadline := time.Now().Add(within)
	var last map[string]any
	for time.Now().Before(deadline) {
		_, last = s.do("GET", "/api/v1/drones/sim", viewerToken, nil)
		if ok(last) {
			return last
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.t.Fatalf("telemetry condition not met; last: %v", last)
	return nil
}

func expect(t *testing.T, code int, body map[string]any, want int) {
	t.Helper()
	if code != want {
		t.Fatalf("status %d, want %d: %v", code, want, body)
	}
}

func TestEndToEnd(t *testing.T) {
	s := startStack(t)

	t.Run("telemetry", func(t *testing.T) {
		m := s.waitTelemetry(func(m map[string]any) bool { return m["position"] != nil && m["battery"] != nil }, 3*time.Second)
		pos := m["position"].(map[string]any)
		if pos["lat"].(float64) < 23 || m["mode"] != "STABILIZE" || m["vehicle_type"] != "QUADROTOR" {
			t.Fatalf("unexpected telemetry: %v", m)
		}
	})

	t.Run("auth", func(t *testing.T) {
		c, b := s.do("POST", "/api/v1/drones/sim/arm", "", nil)
		expect(t, c, b, 401)
		c, b = s.do("POST", "/api/v1/drones/sim/arm", viewerToken, nil)
		expect(t, c, b, 403)
		c, b = s.do("GET", "/api/v1/drones/nope", viewerToken, nil)
		expect(t, c, b, 404)
	})

	t.Run("arm rejected by prearm is reported", func(t *testing.T) {
		s.vehicle.Set(func(v *sim.Vehicle) { v.PrearmFail = true })
		c, b := s.do("POST", "/api/v1/drones/sim/arm", opToken, nil)
		expect(t, c, b, 409)
		if b["result"] != "FAILED" {
			t.Fatalf("want result FAILED: %v", b)
		}
		s.vehicle.Set(func(v *sim.Vehicle) { v.PrearmFail = false })
	})

	t.Run("takeoff requires arming", func(t *testing.T) {
		c, b := s.do("POST", "/api/v1/drones/sim/takeoff", opToken, map[string]any{"alt_m": 10})
		expect(t, c, b, 422)
	})

	t.Run("lost ACK is retried", func(t *testing.T) {
		s.vehicle.Set(func(v *sim.Vehicle) { v.DropAcks[common.MAV_CMD_COMPONENT_ARM_DISARM] = 1 })
		c, b := s.do("POST", "/api/v1/drones/sim/arm", opToken, nil)
		expect(t, c, b, 200)
		_, _, _, cmds := s.vehicle.Snapshot()
		var confs []uint8
		for _, r := range cmds {
			if r.Command == common.MAV_CMD_COMPONENT_ARM_DISARM && r.Params[0] == 1 {
				confs = append(confs, r.Confirmation)
			}
		}
		if len(confs) < 2 || confs[len(confs)-1] != 1 {
			t.Fatalf("expected a retry with confirmation=1, got %v", confs)
		}
		s.waitTelemetry(func(m map[string]any) bool { return m["armed"] == true }, 3*time.Second)
	})

	t.Run("takeoff switches to guided", func(t *testing.T) {
		c, b := s.do("POST", "/api/v1/drones/sim/takeoff", opToken, map[string]any{"alt_m": 10})
		expect(t, c, b, 200)
		s.waitTelemetry(func(m map[string]any) bool {
			pos, _ := m["position"].(map[string]any)
			return m["mode"] == "GUIDED" && pos != nil && pos["alt_rel_m"].(float64) > 1
		}, 5*time.Second)
	})

	t.Run("limits", func(t *testing.T) {
		c, b := s.do("POST", "/api/v1/drones/sim/goto", opToken, map[string]any{"lat": 23.81, "lon": 90.41, "alt_m": 500})
		expect(t, c, b, 422)
		c, b = s.do("POST", "/api/v1/drones/sim/mode", opToken, map[string]any{"mode": "WARP"})
		expect(t, c, b, 422)
	})

	t.Run("goto", func(t *testing.T) {
		c, b := s.do("POST", "/api/v1/drones/sim/goto", opToken, map[string]any{"lat": 23.8110, "lon": 90.4130, "alt_m": 20})
		expect(t, c, b, 200)
		_, _, _, cmds := s.vehicle.Snapshot()
		last := cmds[len(cmds)-1]
		if last.Command != common.MAV_CMD_DO_REPOSITION || last.X != 238110000 || last.Z != 20 {
			t.Fatalf("bad reposition: %+v", last)
		}
	})

	t.Run("mission upload with a dropped request, then download", func(t *testing.T) {
		s.vehicle.Set(func(v *sim.Vehicle) { v.DropMissionRequests = 1 })
		items := []map[string]any{
			{"type": "takeoff", "alt_m": 15},
			{"type": "waypoint", "lat": 23.8110, "lon": 90.4130, "alt_m": 20, "hold_s": 2},
			{"type": "photo"},
			{"type": "speed", "speed_m_s": 8},
			{"type": "waypoint", "lat": 23.8120, "lon": 90.4140, "alt_m": 25},
			{"type": "rtl"},
		}
		c, b := s.do("PUT", "/api/v1/drones/sim/mission", opToken, map[string]any{"items": items})
		expect(t, c, b, 200)
		_, _, mission, _ := s.vehicle.Snapshot()
		if len(mission) != 7 { // home + 6
			t.Fatalf("vehicle stored %d items, want 7", len(mission))
		}
		if mission[0].Command != common.MAV_CMD_NAV_WAYPOINT || mission[1].Command != common.MAV_CMD_NAV_TAKEOFF {
			t.Fatalf("item 0 must be home and item 1 the takeoff: %v / %v", mission[0].Command, mission[1].Command)
		}
		c, b = s.do("GET", "/api/v1/drones/sim/mission", viewerToken, nil)
		expect(t, c, b, 200)
		got := b["items"].([]any)
		var types []string
		for _, it := range got {
			types = append(types, it.(map[string]any)["type"].(string))
		}
		if strings.Join(types, ",") != "takeoff,waypoint,photo,speed,waypoint,rtl" {
			t.Fatalf("downloaded types %v", types)
		}
	})

	t.Run("invalid mission rejected before sending", func(t *testing.T) {
		c, b := s.do("PUT", "/api/v1/drones/sim/mission", opToken, map[string]any{"items": []map[string]any{{"type": "waypoint", "lat": 0, "lon": 0, "alt_m": 10}}})
		expect(t, c, b, 422)
	})

	t.Run("mission start", func(t *testing.T) {
		c, b := s.do("POST", "/api/v1/drones/sim/mission/start", opToken, nil)
		expect(t, c, b, 200)
		s.waitTelemetry(func(m map[string]any) bool { return m["mode"] == "AUTO" }, 3*time.Second)
	})

	t.Run("params with a lost value are recovered", func(t *testing.T) {
		s.vehicle.Set(func(v *sim.Vehicle) { v.DropParamOnce[1] = true })
		c, b := s.do("GET", "/api/v1/drones/sim/params?refresh=1", viewerToken, nil)
		expect(t, c, b, 200)
		if n := len(b["params"].([]any)); n != 4 {
			t.Fatalf("got %d params, want 4", n)
		}
		// Once cached, only operators may force another full download.
		c, b = s.do("GET", "/api/v1/drones/sim/params?refresh=1", viewerToken, nil)
		expect(t, c, b, 403)
		c, b = s.do("GET", "/api/v1/drones/sim/params", viewerToken, nil)
		expect(t, c, b, 200)
		c, b = s.do("PUT", "/api/v1/drones/sim/params/rtl_alt_m", opToken, map[string]any{"value": 30})
		expect(t, c, b, 200)
		c, b = s.do("PUT", "/api/v1/drones/sim/params/FENCE_ENABLE", opToken, map[string]any{"value": 7})
		expect(t, c, b, 409) // out of range: vehicle keeps the old value
		c, b = s.do("GET", "/api/v1/drones/sim/params/NOT_A_PARAM", viewerToken, nil)
		expect(t, c, b, 504)
	})

	t.Run("camera photo reaches the C12", func(t *testing.T) {
		s.c12.reset()
		c, b := s.do("POST", "/api/v1/drones/sim/camera/photo", opToken, nil)
		expect(t, c, b, 200)
		s.c12.waitFor(t, "#TPUD2wCAP013E\r\n", time.Second)
	})

	t.Run("camera record and zoom", func(t *testing.T) {
		s.c12.reset()
		c, b := s.do("POST", "/api/v1/drones/sim/camera/video/start", opToken, nil)
		expect(t, c, b, 200)
		s.c12.waitFor(t, "#TPUD2wREC0144\r\n", time.Second)
		c, b = s.do("POST", "/api/v1/drones/sim/camera/zoom", opToken, map[string]any{"type": "range", "value": 100})
		expect(t, c, b, 200)
		s.c12.waitFor(t, "#TPUD2wDZM06791\r\n", time.Second)
		c, b = s.do("POST", "/api/v1/drones/sim/camera/zoom", opToken, map[string]any{"type": "step", "value": 1})
		expect(t, c, b, 200)
		s.c12.waitFor(t, "#TPUD2wDZM0A65", time.Second)
	})

	t.Run("gimbal attitude, center and lock", func(t *testing.T) {
		s.c12.reset()
		c, b := s.do("POST", "/api/v1/drones/sim/gimbal/attitude", opToken, map[string]any{"pitch_deg": -45})
		expect(t, c, b, 200)
		s.c12.waitFor(t, "#TPUG6wGAPEE6C104C", time.Second)
		c, b = s.do("POST", "/api/v1/drones/sim/gimbal/center", opToken, nil)
		expect(t, c, b, 200)
		s.c12.waitFor(t, "#TPUG2wPTZ056F", time.Second)
		c, b = s.do("POST", "/api/v1/drones/sim/gimbal/mode", opToken, map[string]any{"mode": "lock"})
		expect(t, c, b, 200)
		s.c12.waitFor(t, "#TPUG2wPTZ0771", time.Second)
		s.waitTelemetry(func(m map[string]any) bool {
			g, _ := m["gimbal"].(map[string]any)
			return g["yaw_lock"] == true
		}, 3*time.Second)
	})

	t.Run("gimbal rate stops on its own when updates stop (dead-man)", func(t *testing.T) {
		s.c12.reset()
		c, b := s.do("POST", "/api/v1/drones/sim/gimbal/rate", opToken, map[string]any{"pitch_deg_s": -5})
		expect(t, c, b, 200)
		s.c12.waitFor(t, (skydroid.Encoder{Speed: skydroid.SpeedLegacy}).SpeedPitch(-5), time.Second)
		// No further updates: the bridge must send a stop by itself.
		s.c12.waitFor(t, (skydroid.Encoder{Speed: skydroid.SpeedLegacy}).SpeedPitch(0), 2*time.Second)
	})

	t.Run("websocket telemetry and hold-to-move", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		url := "ws" + strings.TrimPrefix(s.api.URL, "http") + "/api/v1/drones/sim/ws?token=" + opToken
		c, _, err := websocket.Dial(ctx, url, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer c.CloseNow()
		var first map[string]any
		if err := wsjson.Read(ctx, c, &first); err != nil || first["type"] != "telemetry" {
			t.Fatalf("first message: %v %v", first, err)
		}
		s.c12.reset()
		want := (skydroid.Encoder{Speed: skydroid.SpeedLegacy}).SpeedYaw(8)
		for i := 0; i < 5; i++ { // operator holds the control for ~0.5 s
			if err := wsjson.Write(ctx, c, map[string]any{"type": "gimbal_rate", "yaw_deg_s": 8}); err != nil {
				t.Fatal(err)
			}
			time.Sleep(100 * time.Millisecond)
		}
		s.c12.waitFor(t, want, time.Second)
		c.Close(websocket.StatusNormalClosure, "") // release: gateway sends zero rates
		s.c12.waitFor(t, (skydroid.Encoder{Speed: skydroid.SpeedLegacy}).SpeedYaw(0), 2*time.Second)

		// A viewer token must not be able to move the gimbal.
		vc, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(s.api.URL, "http")+"/api/v1/drones/sim/ws?token="+viewerToken, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer vc.CloseNow()
		s.c12.reset()
		_ = wsjson.Write(ctx, vc, map[string]any{"type": "gimbal_rate", "yaw_deg_s": 8})
		time.Sleep(400 * time.Millisecond)
		for _, p := range s.c12.packets() {
			if p == want {
				t.Fatal("viewer moved the gimbal")
			}
		}
	})

	t.Run("land disarms", func(t *testing.T) {
		c, b := s.do("POST", "/api/v1/drones/sim/land", opToken, nil)
		expect(t, c, b, 200)
		s.waitTelemetry(func(m map[string]any) bool { return m["mode"] == "LAND" }, 3*time.Second)
	})
}

func testLogOut() io.Writer {
	if testing.Verbose() {
		return os.Stderr
	}
	return io.Discard
}
