package gateway

import (
	"testing"
	"time"
)

// The lens comes from a VIDEO_STREAM_INFORMATION name (dji-bridge names the
// stream after the lens). MAVLink pads names with NULs.
func TestLensOf(t *testing.T) {
	for name, want := range map[string]string{
		"wide": "wide", "zoom": "zoom", "thermal": "thermal",
		"Thermal\x00\x00": "thermal", " zoom ": "zoom",
		"": "", "camera": "", "rtsp main": "",
	} {
		if got := lensOf(name); got != want {
			t.Errorf("lensOf(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestCameraSourceParams(t *testing.T) {
	if cameraSources["wide"] != 4 || cameraSources["zoom"] != 1 || cameraSources["thermal"] != 2 {
		t.Fatalf("source numbers changed: %v", cameraSources)
	}
}

func TestKindOfFollowsTheLens(t *testing.T) {
	g, _ := testGateway(t, nil)
	d := g.drones["kotowali"]
	// No lens reported: the stream's name decides.
	if kindOf(d, "x_camera") != "visible" || kindOf(d, "x_thermal") != "thermal" {
		t.Fatal("by name")
	}
	// A lens-switching camera: its one stream is thermal while the thermal lens is on.
	set := func(lens string) {
		d.mu.Lock()
		d.st.Camera = &CameraState{Source: &lens}
		d.sourceSeen = time.Now()
		d.mu.Unlock()
	}
	set("thermal")
	if got := kindOf(d, "mavic3t_camera"); got != "thermal" {
		t.Fatalf("thermal lens: %s", got)
	}
	set("zoom")
	if got := kindOf(d, "mavic3t_camera"); got != "visible" {
		t.Fatalf("zoom lens: %s", got)
	}
}
