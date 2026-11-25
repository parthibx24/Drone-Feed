package recorder

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// realManager uses the ffmpeg/ffprobe on PATH; the test is skipped without them.
func realManager(t *testing.T) *Manager {
	t.Helper()
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not on PATH", bin)
		}
	}
	m, err := New(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// makeMKV writes a short H.264 test recording with the given number of
// video tracks, as the recorder writes them (stream copy into MKV).
func makeMKV(t *testing.T, path string, tracks int) {
	t.Helper()
	args := []string{"-y", "-hide_banner", "-loglevel", "error"}
	for i := 0; i < tracks; i++ {
		args = append(args, "-f", "lavfi", "-i", "testsrc2=size=320x240:rate=25:duration=4")
	}
	for i := 0; i < tracks; i++ {
		args = append(args, "-map", string(rune('0'+i))+":v")
	}
	args = append(args, "-c:v", "libx264", "-g", "25", "-pix_fmt", "yuv420p", path)
	if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Skipf("cannot make a test MKV (ffmpeg without libx264?): %v %s", err, out)
	}
}

func TestWebCopyMakesAFaststartMP4(t *testing.T) {
	for _, tracks := range []int{1, 2} {
		m := realManager(t)
		mkv := filepath.Join(m.Dir(), "m3t_20261125_101500.mkv")
		makeMKV(t, mkv, tracks)
		before, _ := os.Stat(mkv)

		m.webCopy(mkv)

		mp4 := filepath.Join(m.Dir(), "m3t_20261125_101500.mp4")
		if _, err := os.Stat(mkv); !os.IsNotExist(err) {
			t.Fatalf("%d tracks: the MKV should be gone after a good copy (err %v)", tracks, err)
		}
		data, err := os.ReadFile(mp4)
		if err != nil {
			t.Fatalf("%d tracks: %v", tracks, err)
		}
		// Faststart: the index (moov) comes before the media (mdat), so the
		// browser can start playing from the first bytes.
		moov, mdat := bytes.Index(data, []byte("moov")), bytes.Index(data, []byte("mdat"))
		if moov < 0 || mdat < 0 || moov > mdat {
			t.Fatalf("%d tracks: moov at %d, mdat at %d: not faststart", tracks, moov, mdat)
		}
		d := m.ProbeFile(mp4)
		if d.StreamCount != tracks || d.DurationSec == nil || *d.DurationSec < 3.5 {
			t.Fatalf("%d tracks: copy probes as %+v", tracks, d)
		}
		after, _ := os.Stat(mp4)
		if !after.ModTime().Equal(before.ModTime()) {
			t.Errorf("%d tracks: the MP4 should keep the recording's time", tracks)
		}
		if leftovers, _ := filepath.Glob(filepath.Join(m.Dir(), "*"+tmpSuffix)); len(leftovers) != 0 {
			t.Errorf("%d tracks: temporary file left: %v", tracks, leftovers)
		}
	}
}

func TestWebCopyKeepsTheMKVWhenItCannotCopy(t *testing.T) {
	m := realManager(t)
	mkv := filepath.Join(m.Dir(), "broken_20261125_101500.mkv")
	if err := os.WriteFile(mkv, []byte("not a video"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.webCopy(mkv)
	if _, err := os.Stat(mkv); err != nil {
		t.Fatalf("a file that can't be copied must stay: %v", err)
	}
	if mp4s, _ := filepath.Glob(filepath.Join(m.Dir(), "*.mp4*")); len(mp4s) != 0 {
		t.Fatalf("nothing should be written: %v", mp4s)
	}
}

func TestWebCopySkipsARecordingInProgress(t *testing.T) {
	m := realManager(t)
	mkv := filepath.Join(m.Dir(), "live_20261125_101500.mkv")
	makeMKV(t, mkv, 1)
	m.active["live"] = &active{path: mkv, state: "recording"}
	m.webCopy(mkv)
	if _, err := os.Stat(mkv); err != nil {
		t.Fatalf("the file being recorded must not be touched: %v", err)
	}
}

func TestSameRecording(t *testing.T) {
	sec := func(v float64) *float64 { return &v }
	src := FileDetails{StreamCount: 2, DurationSec: sec(100)}
	if err := sameRecording(src, FileDetails{StreamCount: 2, DurationSec: sec(99.5)}); err != nil {
		t.Errorf("half a second short is the same recording: %v", err)
	}
	if sameRecording(src, FileDetails{StreamCount: 1, DurationSec: sec(100)}) == nil {
		t.Error("a missing track must fail")
	}
	if sameRecording(src, FileDetails{StreamCount: 2, DurationSec: sec(60)}) == nil {
		t.Error("a short copy must fail")
	}
	if sameRecording(src, FileDetails{StreamCount: 2}) == nil {
		t.Error("a copy without duration must fail")
	}
}
