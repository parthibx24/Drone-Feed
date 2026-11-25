package recorder

import (
	"context"
	"fmt"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Recording to MKV is safe: a file cut off by a crash or a dropped stream
// still plays. Browsers, though, can't play MKV themselves, and its index
// sits at the end, so the dashboard had to fetch and decode it piece by
// piece. Once a recording is finished it is therefore copied (no
// re-encoding) into an MP4 with its index at the front ("faststart"),
// which the browser's own player streams and seeks with range requests.
// The MKV is deleted only after the MP4 has been checked.

const (
	webCopyTimeout = 30 * time.Minute // a multi-GB file on a slow disk
	webCopyQueue   = 256
	tmpSuffix      = ".mp4.part" // not listed: List only shows .mkv/.mp4
)

// startWebCopies runs the converter: one file at a time, so a burst of
// recordings doesn't compete with live video for the disk.
func (m *Manager) startWebCopies() {
	m.webOnce.Do(func() {
		m.webQueue = make(chan string, webCopyQueue)
		go func() {
			for p := range m.webQueue {
				m.webCopy(p)
			}
		}()
	})
}

// queueWebCopy schedules a finished MKV for conversion. It never blocks.
func (m *Manager) queueWebCopy(mkv string) {
	if m.ffmpeg == "" || m.ffprobe == "" || !strings.EqualFold(filepath.Ext(mkv), ".mkv") {
		return
	}
	m.startWebCopies()
	select {
	case m.webQueue <- mkv:
	default:
		m.log.Warn("web copy queue full; the recording stays MKV until the next restart", "file", filepath.Base(mkv))
	}
}

// ConvertExisting queues every finished MKV in the recordings directory,
// newest first (the ones most likely to be watched), so recordings made
// before this existed play in the browser too.
func (m *Manager) ConvertExisting() {
	var found []string
	_ = filepath.WalkDir(m.dir, func(p string, de fs.DirEntry, err error) error {
		if err == nil && !de.IsDir() && strings.EqualFold(filepath.Ext(p), ".mkv") {
			found = append(found, p)
		}
		return nil
	})
	sort.Slice(found, func(i, j int) bool { return modTime(found[i]).After(modTime(found[j])) })
	for _, p := range found {
		if !m.isRecording(p) {
			m.queueWebCopy(p)
		}
	}
}

func modTime(p string) time.Time {
	if st, err := os.Stat(p); err == nil {
		return st.ModTime()
	}
	return time.Time{}
}

// isRecording reports whether FFmpeg is still writing p.
func (m *Manager) isRecording(p string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.active {
		if r.path == p {
			return true
		}
	}
	return false
}

// webCopy converts one MKV. On any failure the MKV is kept as it was.
func (m *Manager) webCopy(mkv string) {
	if m.isRecording(mkv) {
		return
	}
	if _, err := os.Stat(mkv); err != nil {
		return // deleted meanwhile
	}
	mp4 := strings.TrimSuffix(mkv, filepath.Ext(mkv)) + ".mp4"
	if _, err := os.Stat(mp4); err == nil {
		m.log.Warn("web copy skipped: an MP4 of this name exists", "file", filepath.Base(mp4))
		return
	}
	src := m.ProbeFile(mkv)
	if src.StreamCount == 0 {
		m.log.Warn("web copy skipped: no video tracks found", "file", filepath.Base(mkv))
		return
	}

	tmp := strings.TrimSuffix(mkv, filepath.Ext(mkv)) + tmpSuffix
	defer os.Remove(tmp) // left behind only by a failure
	args := []string{"-y", "-hide_banner", "-loglevel", "error", "-nostdin", "-i", mkv, "-map", "0:v", "-c", "copy"}
	for i, s := range src.Streams {
		// Safari plays HEVC in MP4 only with the hvc1 sample entry.
		if s.CodecName == "hevc" {
			args = append(args, fmt.Sprintf("-tag:v:%d", i), "hvc1")
		}
	}
	args = append(args, "-movflags", "+faststart", "-f", "mp4", tmp)

	ctx, cancel := context.WithTimeout(context.Background(), webCopyTimeout)
	defer cancel()
	started := time.Now()
	if out, err := exec.CommandContext(ctx, m.ffmpeg, args...).CombinedOutput(); err != nil {
		m.log.Warn("web copy failed; keeping the MKV", "file", filepath.Base(mkv), "err", err, "ffmpeg", tail(string(out), 300))
		return
	}
	if err := sameRecording(src, m.probeUncached(tmp)); err != nil {
		m.log.Warn("web copy does not match the MKV; keeping the MKV", "file", filepath.Base(mkv), "err", err)
		return
	}
	// A recording could have been deleted while it was being copied.
	if _, err := os.Stat(mkv); err != nil {
		return
	}
	if err := os.Rename(tmp, mp4); err != nil {
		m.log.Warn("web copy could not be put in place; keeping the MKV", "file", filepath.Base(mkv), "err", err)
		return
	}
	// Same modification time as the MKV, so the library keeps its order and dates.
	if st, err := os.Stat(mkv); err == nil {
		_ = os.Chtimes(mp4, st.ModTime(), st.ModTime())
	}
	if err := os.Remove(mkv); err != nil {
		m.log.Warn("web copy made but the MKV could not be removed", "file", filepath.Base(mkv), "err", err)
	}
	m.log.Info("recording ready for the browser", "file", filepath.Base(mp4), "took", time.Since(started).Round(time.Millisecond))
}

// probeUncached is ProbeFile for a file that is about to be renamed.
func (m *Manager) probeUncached(p string) FileDetails {
	d := m.ProbeFile(p)
	m.mu.Lock()
	delete(m.probeCache, filepath.Base(p))
	m.mu.Unlock()
	return d
}

// sameRecording checks a copy has every track and (nearly) the whole length.
func sameRecording(src, dst FileDetails) error {
	if dst.StreamCount != src.StreamCount {
		return fmt.Errorf("%d video tracks instead of %d", dst.StreamCount, src.StreamCount)
	}
	if src.DurationSec == nil {
		return nil // nothing to compare against (an MKV cut off before its header was finished)
	}
	if dst.DurationSec == nil {
		return fmt.Errorf("the copy has no duration")
	}
	if diff := math.Abs(*dst.DurationSec - *src.DurationSec); diff > math.Max(1, *src.DurationSec*0.02) {
		return fmt.Errorf("%.1f s long instead of %.1f s", *dst.DurationSec, *src.DurationSec)
	}
	return nil
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}
