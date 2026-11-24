package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bluenviron/gomavlib/v3/pkg/dialects/common"
)

// Command kinds dji-bridge reports for each of the 3T's links.
var (
	pilotAppKinds = []string{"gimbal_angle", "gimbal_rate", "gimbal_reset", "goto", "land", "look_at", "pause",
		"resume_mission", "return_home", "set_zoom", "speaker_audio", "start_mission", "start_recording",
		"take_photo", "takeoff", "upload_mission"}
	pilot2Kinds = []string{"gimbal_rate", "gimbal_reset", "set_zoom", "take_photo", "start_recording", "meter_spot"}
)

func hasAll(t *testing.T, c Capabilities, want ...string) {
	t.Helper()
	for _, f := range want {
		if !c.Has(f) {
			t.Errorf("missing %s in %v", f, c.Features)
		}
	}
}

func hasNone(t *testing.T, c Capabilities, unwanted ...string) {
	t.Helper()
	for _, f := range unwanted {
		if c.Has(f) {
			t.Errorf("unexpected %s in %v", f, c.Features)
		}
	}
}

func TestArduPilotCapabilities(t *testing.T) {
	in := capInput{profile: ProfileArduPilot, ai: true}
	c := deriveCapabilities(in)
	hasAll(t, c, FeatArm, FeatSetMode, FeatTakeoff, FeatLand, FeatRTL, FeatGoto, FeatMission, FeatMissionJump,
		FeatParams, FeatRecording, FeatAI)
	// No gimbal or camera seen yet: their controls stay hidden.
	hasNone(t, c, FeatGimbal, FeatZoom, FeatPhoto, FeatSpeaker, FeatAircraft, FeatCameraSource, FeatRawCommand)
	if !slices.Contains(c.Modes, "LOITER") || !slices.Contains(c.Modes, "AUTO") {
		t.Errorf("modes: %v", c.Modes)
	}
	if !slices.Contains(c.MissionItems, "raw") || len(c.MissionAltFrames) != 3 {
		t.Errorf("mission: %v %v", c.MissionItems, c.MissionAltFrames)
	}

	in.cameraSeen, in.gimbalSeen, in.raw = true, true, true
	hasAll(t, deriveCapabilities(in), FeatGimbal, FeatZoom, FeatPhoto, FeatCameraVideo, FeatRawCommand)
}

func TestDJICapabilities(t *testing.T) {
	// StarWatch Pilot on the RC: flight, missions, gimbal; no ArduPilot-only routes.
	in := capInput{profile: ProfileDJI, speaker: true, aircraft: true, lensNow: true,
		bridge: &bridgeStatus{at: time.Now(), model: "M3T", online: true, kinds: pilotAppKinds}}
	c := deriveCapabilities(in)
	hasAll(t, c, FeatArm, FeatTakeoff, FeatLand, FeatRTL, FeatPause, FeatResume, FeatGoto, FeatROI, FeatMission,
		FeatMissionStart, FeatGimbal, FeatZoom, FeatPhoto, FeatCameraVideo, FeatCameraSource, FeatSpeaker, FeatAircraft)
	hasNone(t, c, FeatSetMode, FeatParams, FeatReboot, FeatSpeed, FeatMissionJump, FeatRawCommand)
	if c.Model != "M3T" || c.Note != "" || len(c.Modes) != 0 {
		t.Errorf("model %q note %q modes %v", c.Model, c.Note, c.Modes)
	}
	if slices.Contains(c.MissionItems, "raw") || !slices.Contains(c.MissionItems, "waypoint") ||
		!slices.Equal(c.MissionAltFrames, []string{"relative"}) {
		t.Errorf("mission: %v %v", c.MissionItems, c.MissionAltFrames)
	}

	// DJI Pilot 2 (Cloud API): camera only, and the dashboard says why.
	in.bridge.kinds = pilot2Kinds
	c = deriveCapabilities(in)
	hasAll(t, c, FeatGimbal, FeatZoom, FeatPhoto)
	hasNone(t, c, FeatArm, FeatTakeoff, FeatLand, FeatRTL, FeatGoto, FeatMission)
	if !strings.Contains(c.Note, "StarWatch Pilot") {
		t.Errorf("note: %q", c.Note)
	}

	// The bridge not answering: nothing is hidden, the aircraft refuses what it can't do.
	in.bridge = nil
	c = deriveCapabilities(in)
	hasAll(t, c, FeatTakeoff, FeatLand, FeatRTL, FeatPause, FeatGimbal)
	if c.Note == "" {
		t.Error("expected a note while the bridge is silent")
	}
}

func TestMAVLinkCapabilities(t *testing.T) {
	c := deriveCapabilities(capInput{profile: ProfileMAVLink, gimbalSeen: true, raw: true})
	hasAll(t, c, FeatArm, FeatPause, FeatMission, FeatParams, FeatGimbal, FeatRawCommand)
	// ArduCopter mode numbers mean something else elsewhere.
	hasNone(t, c, FeatSetMode, FeatTakeoff, FeatLand, FeatRTL, FeatGoto)
	if len(c.Modes) != 0 {
		t.Errorf("modes: %v", c.Modes)
	}
}

func TestFeatureOverrides(t *testing.T) {
	in := capInput{profile: ProfileMAVLink, enable: []string{FeatLand, FeatZoom}, disable: []string{FeatParams}}
	c := deriveCapabilities(in)
	hasAll(t, c, FeatLand, FeatZoom) // enabled: shown even before a camera is seen
	hasNone(t, c, FeatParams)
	set, _ := allowed(in)
	if !set[FeatLand] || set[FeatParams] {
		t.Errorf("allowed: %v", set)
	}
}

func TestProfileResolution(t *testing.T) {
	g, _ := testGateway(t, func(c *Config) {
		c.Drones = append(c.Drones,
			DroneConfig{ID: "m3t", MAVLink: "udp:127.0.0.1:9", Bridge: &BridgeConfig{API: "http://bridge/x", Token: "t"}},
			DroneConfig{ID: "px4", MAVLink: "udp:127.0.0.1:9", Autopilot: ProfileMAVLink},
		)
	})
	d := g.drones["kotowali"]
	if p := d.profile(); p != ProfileArduPilot {
		t.Errorf("unidentified: %s", p)
	}
	d.mu.Lock()
	d.autopilot = common.MAV_AUTOPILOT_PX4
	d.mu.Unlock()
	if p := d.profile(); p != ProfileMAVLink {
		t.Errorf("PX4 heartbeat: %s", p)
	}
	if p := g.drones["m3t"].profile(); p != ProfileDJI {
		t.Errorf("bridge: %s", p)
	}
	if p := g.drones["px4"].profile(); p != ProfileMAVLink {
		t.Errorf("configured: %s", p)
	}
}

func TestCapabilityConfig(t *testing.T) {
	base := func() *Config {
		return &Config{Drones: []DroneConfig{{ID: "a", MAVLink: "udp:127.0.0.1:9"}}}
	}
	for name, tc := range map[string]struct {
		mutate func(*DroneConfig)
		want   string
	}{
		"bad autopilot":   {func(d *DroneConfig) { d.Autopilot = "betaflight" }, "autopilot must be"},
		"dji no bridge":   {func(d *DroneConfig) { d.Autopilot = ProfileDJI }, "needs bridge"},
		"unknown feature": {func(d *DroneConfig) { d.Features.Disable = []string{"teleport"} }, `unknown feature "teleport"`},
	} {
		c := base()
		tc.mutate(&c.Drones[0])
		if err := c.Normalize(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	c := base()
	c.Drones[0].Autopilot = ProfileMAVLink
	c.Drones[0].Features = FeaturesConfig{Enable: []string{FeatLand}, Disable: []string{FeatParams}}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
}

// fakeBridgeAircraft answers GET /api/v1/aircraft/SN1 as dji-bridge does.
func fakeBridgeAircraft(t *testing.T, kinds []string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer bridge-token" {
			writeJSON(w, 401, map[string]any{"error": "bad token"})
			return
		}
		if r.URL.Path != "/api/v1/aircraft/SN1" {
			writeJSON(w, 404, map[string]any{"error": "no"})
			return
		}
		writeJSON(w, 200, map[string]any{"serial": "SN1", "model": "M3T", "online": true, "capabilities": kinds})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCapabilitiesRoutes(t *testing.T) {
	bridge := fakeBridgeAircraft(t, pilot2Kinds)
	g, srv := testGateway(t, func(c *Config) {
		c.Drones = append(c.Drones, DroneConfig{
			ID: "m3t", Name: "Mavic 3T", MAVLink: "udp:127.0.0.1:9",
			Bridge: &BridgeConfig{API: bridge.URL + "/api/v1/aircraft/SN1", Token: "bridge-token"},
		})
	})
	d := g.drones["m3t"]

	// The poller reads the bridge (one round, then stops with the context).
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { d.pollBridge(ctx); close(done) }()
	deadline := time.Now().Add(3 * time.Second)
	for d.bridgeNow() == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	if st := d.bridgeNow(); st == nil || st.model != "M3T" || !slices.Equal(st.kinds, pilot2Kinds) {
		t.Fatalf("bridge status: %+v", st)
	}

	pilot := login(t, srv.URL, "pilot", "operator-pass")
	code, body, _ := pilot.do("GET", "/api/v1/drones/m3t/capabilities", "", nil)
	if code != 200 || body["autopilot"] != "dji" || body["model"] != "M3T" {
		t.Fatalf("capabilities: %d %v", code, body)
	}

	// Pilot 2 can't fly: the gateway refuses before anything is sent.
	for _, path := range []string{"/takeoff", "/land", "/mode"} {
		code, body, _ := pilot.do("POST", "/api/v1/drones/m3t"+path, `{"alt_m":10,"mode":"LAND"}`, csrf)
		if code != 409 || !strings.Contains(body["error"].(string), "Mavic 3T does not offer") {
			t.Errorf("%s: %d %v", path, code, body)
		}
	}
	// No parameters on a DJI aircraft, for viewers either.
	if code, _, _ := pilot.do("GET", "/api/v1/drones/m3t/params", "", nil); code != 409 {
		t.Errorf("params: %d", code)
	}
	// The ArduPilot drone keeps every route: offline, so it fails further on.
	if code, _, _ := pilot.do("POST", "/api/v1/drones/kotowali/land", "", csrf); code == 409 {
		t.Errorf("kotowali land refused as unsupported")
	}

	// The drone list carries each drone's capabilities.
	view := login(t, srv.URL, "watcher", "viewer-pass")
	resp, err := view.http.Get(srv.URL + "/api/v1/drones")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var list []struct {
		ID           string       `json:"id"`
		Capabilities Capabilities `json:"capabilities"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Capabilities.Autopilot != "ardupilot" || list[1].Capabilities.Autopilot != "dji" ||
		list[1].Capabilities.Has(FeatTakeoff) || !list[0].Capabilities.Has(FeatTakeoff) {
		t.Fatalf("list: %+v", list)
	}
}
