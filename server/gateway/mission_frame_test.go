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
