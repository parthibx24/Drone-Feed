package recorder

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeFFmpeg installs shell-script ffmpeg/ffprobe on PATH.
//
//   - probe (args contain "-f null"): exits 0 unless the stream URL contains "dead".
//   - record: writes bytes to the output file (last arg), then waits for "q" on stdin.
//   - ffprobe: prints fixed JSON metadata.
func fakeFFmpeg(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	ffmpeg := `#!/bin/sh
for a in "$@"; do case "$a" in *dead*) dead=1;; esac; done
case " $* " in *" -f null "*) [ -n "$dead" ] && exit 1; exit 0;; esac
for a in "$@"; do out="$a"; done
printf 'MKVDATA' > "$out"
while read line; do [ "$line" = "q" ] && exit 0; done
exit 0
`
	ffprobe := `#!/bin/sh
cat <<'JSON'
{"format":{"format_name":"matroska,webm","duration":"12.34","bit_rate":"800000"},
 "streams":[{"index":0,"codec_type":"video","codec_name":"hevc","width":1920,"height":1080,"r_frame_rate":"30/1"},
            {"index":1,"codec_type":"video","codec_name":"hevc","width":640,"height":512,"r_frame_rate":"25/1"}]}
JSON
`
	if err := os.WriteFile(filepath.Join(bin, "ffmpeg"), []byte(ffmpeg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "ffprobe"), []byte(ffprobe), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func newManager(t *testing.T) *Manager {
	t.Helper()
	m, err := New(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func waitState(t *testing.T, m *Manager, drone, want string, within time.Duration) map[string]any {
	t.Helper()
	deadline := time.Now().Add(within)
	var st map[string]any
	for time.Now().Before(deadline) {
		st = m.Status(drone)
		if st["state"] == want {
			return st
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("state never became %q: %v", want, st)
	return nil
}

func TestHelpers(t *testing.T) {
	for in, want := range map[int64]string{500: "500 B", 2048: "2.0 KB", 5 << 20: "5.0 MB", 3 << 30: "3.00 GB"} {
		if got := FormatBytes(in); got != want {
			t.Errorf("FormatBytes(%d)=%q want %q", in, got, want)
		}
	}
	if FormatDuration(65) != "01:05" || FormatDuration(3725) != "01:02:05" || FormatDuration(-3) != "00:00" {
		t.Error("FormatDuration")
	}
	if v := parseFPS("30000/1001"); v == nil || *v != 29.97 {
		t.Errorf("parseFPS ntsc: %v", v)
	}
	if parseFPS("0/0") != nil || parseFPS("x") != nil {
		t.Error("parseFPS invalid input")
	}
	if got := rsplit("kotowali_20261110_101500", "_", 2); len(got) != 3 || got[0] != "kotowali" {
		t.Errorf("rsplit: %v", got)
	}
}

func TestRecordStopListDelete(t *testing.T) {
	fakeFFmpeg(t)
	m := newManager(t)
	streams := []Stream{{Name: "kotowali_camera", URL: "rtsp://x/kotowali_camera"}, {Name: "kotowali_thermal", URL: "rtsp://x/kotowali_thermal"}}

	res, err := m.Start("kotowali", streams)
	if err != nil {
		t.Fatal(err)
	}
	if res["status"] != "waiting_for_stream" {
		t.Fatalf("start: %v", res)
	}
	if again, _ := m.Start("kotowali", streams); again["status"] != "already_waiting" && again["status"] != "already_recording" {
		t.Fatalf("second start should not create another recording: %v", again)
	}
	waitState(t, m, "kotowali", "recording", 5*time.Second)
	if all := m.StatusAll(); all["active_count"] != 1 {
		t.Fatalf("status all: %v", all)
	}

	list := m.List("", "time", "desc")
	if len(list) != 1 || !list[0].IsActive || list[0].DroneName != "kotowali" {
		t.Fatalf("active listing: %+v", list)
	}
	if err := m.Delete(list[0].RelativePath); err == nil {
		t.Fatal("deleting an active recording must fail")
	}

	stop := m.Stop("kotowali")
	if stop["status"] != "recording_stopped" || stop["file_size_bytes"].(int64) == 0 {
		t.Fatalf("stop: %v", stop)
	}
	if !strings.HasPrefix(stop["download_url"].(string), "/api/v1/recordings/download/kotowali/") {
		t.Fatalf("download url: %v", stop["download_url"])
	}
	if m.Status("kotowali")["state"] != "idle" {
		t.Fatal("not idle after stop")
	}

	list = m.List("kotowali", "time", "desc")
	if len(list) != 1 || list[0].IsActive || list[0].StreamCount != 2 || list[0].Streams[1].Role != "thermal" ||
		list[0].DurationFormatted != "00:12" {
		t.Fatalf("finished listing: %+v", list)
	}
	if len(m.List("someone-else", "time", "desc")) != 0 {
		t.Fatal("name filter")
	}
	if p, ok := m.Path(list[0].RelativePath); !ok || !strings.HasSuffix(p, ".mkv") {
		t.Fatal("path lookup")
	}
	if p, ok := m.Path(list[0].Filename); !ok || p == "" {
		t.Fatal("basename fallback lookup")
	}
	if err := m.Delete(list[0].RelativePath); err != nil {
		t.Fatal(err)
	}
	if len(m.List("", "time", "desc")) != 0 {
		t.Fatal("file not deleted")
	}
}

func TestCancelWhileWaiting(t *testing.T) {
	fakeFFmpeg(t)
	m := newManager(t)
	if _, err := m.Start("d1", []Stream{{Name: "d1_camera", URL: "rtsp://dead/d1_camera"}}); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, "d1", "waiting", time.Second)
	if res := m.Stop("d1"); res["status"] != "recording_cancelled" {
		t.Fatalf("stop while waiting: %v", res)
	}
	if res := m.Stop("d1"); res["status"] != "not_recording" {
		t.Fatalf("second stop: %v", res)
	}
}

func TestWaitTimesOutWithoutFrames(t *testing.T) {
	fakeFFmpeg(t)
	m := newManager(t)
	m.WaitTimeout = 1500 * time.Millisecond
	if _, err := m.Start("d2", []Stream{{Name: "d2_camera", URL: "rtsp://dead/d2_camera"}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st := m.Status("d2")
		if st["state"] == "idle" && st["error"] != nil && strings.Contains(st["error"].(string), "No video frames") {
			if len(m.List("", "time", "desc")) != 0 {
				t.Fatal("a failed recording must not leave a file")
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("expected a failure, got %v", m.Status("d2"))
}

func TestValidationAndTraversal(t *testing.T) {
	fakeFFmpeg(t)
	m := newManager(t)
	if _, err := m.Start("  ", []Stream{{Name: "a", URL: "rtsp://a"}}); err == nil {
		t.Fatal("empty name accepted")
	}
	if _, err := m.Start("d", nil); err == nil {
		t.Fatal("no streams accepted")
	}
	secret := filepath.Join(filepath.Dir(m.Dir()), "secret.mkv")
	_ = os.WriteFile(secret, []byte("x"), 0o644)
	for _, bad := range []string{"../secret.mkv", "../../etc/passwd", "/etc/passwd", ".."} {
		if _, ok := m.Path(bad); ok {
			t.Errorf("path traversal allowed: %q", bad)
		}
	}
	// Unsafe characters in the drone name are sanitised in the file name.
	res, err := m.Start("bad/name ../x", []Stream{{Name: "s", URL: "rtsp://dead/s"}})
	if err != nil {
		t.Fatal(err)
	}
	if fn := res["filename"].(string); strings.ContainsAny(fn, "/ .") && !strings.HasSuffix(fn, ".mkv") {
		t.Fatalf("unsafe filename %q", fn)
	}
	if strings.Contains(res["filename"].(string), "/") {
		t.Fatalf("filename contains a slash: %q", res["filename"])
	}
	m.Stop("bad/name ../x")
}
