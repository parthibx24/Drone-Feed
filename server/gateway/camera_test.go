package gateway

import "testing"

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
