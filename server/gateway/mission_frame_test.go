package gateway

import (
	"testing"

	"github.com/bluenviron/gomavlib/v3/pkg/dialects/common"
)

// Missions made in QGC/Mission Planner with terrain or MSL altitudes must keep
// their altitude frame when read, or a rewrite would silently make them
// relative to home.
func TestDescribeMissionKeepsAltitudeFrame(t *testing.T) {
	wp := func(seq uint16, f common.MAV_FRAME) *common.MessageMissionItemInt {
		return &common.MessageMissionItemInt{Seq: seq, Frame: f, Command: common.MAV_CMD_NAV_WAYPOINT, X: 237756840, Y: 904225445, Z: 30}
	}
	got := DescribeMission([]*common.MessageMissionItemInt{
		wp(0, common.MAV_FRAME_GLOBAL), // home, skipped
		wp(1, common.MAV_FRAME_GLOBAL_RELATIVE_ALT_INT),
		wp(2, common.MAV_FRAME_GLOBAL_TERRAIN_ALT_INT),
		wp(3, common.MAV_FRAME_GLOBAL_INT),
		wp(4, common.MAV_FRAME_LOCAL_NED),
	})
	want := []struct{ typ, frame string }{{"waypoint", ""}, {"waypoint", "terrain"}, {"waypoint", "absolute"}, {"raw", ""}}
	if len(got) != len(want) {
		t.Fatalf("got %d items, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Type != w.typ || got[i].AltFrame != w.frame {
			t.Errorf("item %d: got %s/%q, want %s/%q", i+1, got[i].Type, got[i].AltFrame, w.typ, w.frame)
		}
	}
	if got[3].Frame == nil || common.MAV_FRAME(*got[3].Frame) != common.MAV_FRAME_LOCAL_NED || got[3].Command == nil || *got[3].Command != uint16(common.MAV_CMD_NAV_WAYPOINT) {
		t.Errorf("unsupported frame not preserved as raw: %+v", got[3])
	}

	// And encoding uses the frame asked for.
	frames := map[string]common.MAV_FRAME{"": common.MAV_FRAME_GLOBAL_RELATIVE_ALT_INT, "terrain": common.MAV_FRAME_GLOBAL_TERRAIN_ALT_INT}
	for name, f := range frames {
		if altFrames[name] != f {
			t.Errorf("altFrames[%q] = %v, want %v", name, altFrames[name], f)
		}
	}
}

// A position read while the drone had no GPS fix is 0,0. Waypoints placed
// next to it (or a Go to there) would send the drone towards the Atlantic.
func TestRejectsTargetsNearNullIsland(t *testing.T) {
	d := &Drone{gw: &Config{Limits: Limits{MaxAltM: 120, MaxSpeedMS: 15, MaxMissionItems: 500}}}
	lat, lon, alt := 0.0003, 0.0003, 30.0
	if _, err := d.BuildMission([]MissionItem{{Type: "waypoint", Lat: &lat, Lon: &lon, AltM: &alt}}); err == nil {
		t.Fatal("a waypoint 40 m from 0,0 must be rejected")
	}
	sylLat, sylLon := 24.8949, 91.8687
	if _, err := d.BuildMission([]MissionItem{{Type: "waypoint", Lat: &sylLat, Lon: &sylLon, AltM: &alt}}); err != nil {
		t.Fatalf("a waypoint in Sylhet must be accepted: %v", err)
	}
	if err := d.Goto(t.Context(), 0.0002, -0.0004, 30, nil); err == nil {
		t.Fatal("go to next to 0,0 must be refused")
	}
}
