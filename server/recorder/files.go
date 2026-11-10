package recorder

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// StreamInfo describes one video track in a recording.
type StreamInfo struct {
	Index         int      `json:"index"`
	Role          string   `json:"role"`
	CodecName     string   `json:"codec_name,omitempty"`
	CodecLongName string   `json:"codec_long_name,omitempty"`
	Width         int      `json:"width,omitempty"`
	Height        int      `json:"height,omitempty"`
	Resolution    *string  `json:"resolution"`
	FPS           *float64 `json:"fps"`
	BitRate       *int64   `json:"bit_rate"`
}

// FileDetails is ffprobe metadata for one file.
type FileDetails struct {
	FormatName        string       `json:"format_name"`
	DurationSec       *float64     `json:"duration_sec"`
	DurationFormatted *string      `json:"duration_formatted,omitempty"`
	BitRate           *int64       `json:"bit_rate,omitempty"`
	Streams           []StreamInfo `json:"streams"`
	StreamCount       int          `json:"stream_count"`
}

// Recording is one entry in the library.
type Recording struct {
	Filename          string       `json:"filename"`
	RelativePath      string       `json:"relative_path"`
	Name              string       `json:"name"`
	DroneName         string       `json:"drone_name"`
	CreatedAt         string       `json:"created_at"`
	CreatedTimestamp  float64      `json:"created_timestamp"`
	FileSizeBytes     int64        `json:"file_size_bytes"`
	FileSizeHuman     string       `json:"file_size_human"`
	DurationSec       *float64     `json:"duration_sec"`
	DurationFormatted string       `json:"duration_formatted"`
	IsActive          bool         `json:"is_active"`
	Streams           []StreamInfo `json:"streams"`
	StreamCount       int          `json:"stream_count"`
	FormatName        string       `json:"format_name"`
	DownloadURL       string       `json:"download_url"`
}

// FormatBytes renders a size like the Python version ("12.3 MB").
func FormatBytes(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	case n < 1024*1024*1024:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
	return fmt.Sprintf("%.2f GB", float64(n)/(1024*1024*1024))
}

// FormatDuration renders seconds as MM:SS or HH:MM:SS.
func FormatDuration(sec float64) string {
	t := int(max(0, sec))
	h, m, s := t/3600, (t%3600)/60, t%60
	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

func parseFPS(rate string) *float64 {
	if rate == "" || rate == "0/0" {
		return nil
	}
	if num, den, ok := strings.Cut(rate, "/"); ok {
		n, err1 := strconv.ParseFloat(num, 64)
		d, err2 := strconv.ParseFloat(den, 64)
		if err1 != nil || err2 != nil || d == 0 {
			return nil
		}
		v := round2(n / d)
		return &v
	}
	v, err := strconv.ParseFloat(rate, 64)
	if err != nil {
		return nil
	}
	v = round2(v)
	return &v
}

// ProbeFile returns ffprobe metadata, cached by file name + mtime.
func (m *Manager) ProbeFile(path string) FileDetails {
	st, err := os.Stat(path)
	if err != nil {
		return FileDetails{FormatName: "matroska", Streams: []StreamInfo{}}
	}
	key := filepath.Base(path)
	m.mu.Lock()
	if c, ok := m.probeCache[key]; ok && c.mtime.Equal(st.ModTime()) {
		m.mu.Unlock()
		return c.data
	}
	m.mu.Unlock()

	fallback := FileDetails{FormatName: "matroska", Streams: []StreamInfo{}}
	if m.ffprobe == "" {
		return fallback
	}
	out, err := exec.Command(m.ffprobe, "-v", "quiet", "-print_format", "json", "-show_format", "-show_streams", path).Output()
	if err != nil || len(out) == 0 {
		return fallback
	}
	var raw struct {
		Format struct {
			FormatName string `json:"format_name"`
			Duration   string `json:"duration"`
			BitRate    string `json:"bit_rate"`
		} `json:"format"`
		Streams []struct {
			Index         int    `json:"index"`
			CodecType     string `json:"codec_type"`
			CodecName     string `json:"codec_name"`
			CodecLongName string `json:"codec_long_name"`
			Width         int    `json:"width"`
			Height        int    `json:"height"`
			RFrameRate    string `json:"r_frame_rate"`
			AvgFrameRate  string `json:"avg_frame_rate"`
			BitRate       string `json:"bit_rate"`
		} `json:"streams"`
	}
	if json.Unmarshal(out, &raw) != nil {
		return fallback
	}
	d := FileDetails{FormatName: raw.Format.FormatName, Streams: []StreamInfo{}}
	if d.FormatName == "" {
		d.FormatName = "matroska"
	}
	for i, s := range raw.Streams {
		if s.CodecType != "video" {
			continue
		}
		role := fmt.Sprintf("track_%d", i)
		if i == 0 {
			role = "visible"
		} else if i == 1 {
			role = "thermal"
		}
		info := StreamInfo{Index: s.Index, Role: role, CodecName: s.CodecName, CodecLongName: s.CodecLongName, Width: s.Width, Height: s.Height}
		if s.Width > 0 && s.Height > 0 {
			r := fmt.Sprintf("%dx%d", s.Width, s.Height)
			info.Resolution = &r
		}
		rate := s.RFrameRate
		if rate == "" {
			rate = s.AvgFrameRate
		}
		info.FPS = parseFPS(rate)
		if b, err := strconv.ParseInt(s.BitRate, 10, 64); err == nil {
			info.BitRate = &b
		}
		d.Streams = append(d.Streams, info)
	}
	d.StreamCount = len(d.Streams)
	if v, err := strconv.ParseFloat(raw.Format.Duration, 64); err == nil {
		r := round2(v)
		f := FormatDuration(v)
		d.DurationSec, d.DurationFormatted = &r, &f
	}
	if b, err := strconv.ParseInt(raw.Format.BitRate, 10, 64); err == nil {
		d.BitRate = &b
	}
	m.mu.Lock()
	m.probeCache[key] = probeEntry{mtime: st.ModTime(), data: d}
	m.mu.Unlock()
	return d
}

// List returns recordings (.mkv/.mp4), optionally only one drone's, sorted
// by time (default), duration, size or name.
func (m *Manager) List(drone, sortBy, order string) []Recording {
	filter := strings.ToLower(clean(drone))
	var out []Recording
	_ = filepath.WalkDir(m.dir, func(p string, de fs.DirEntry, err error) error {
		if err != nil || de.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		if ext != ".mkv" && ext != ".mp4" {
			return nil
		}
		st, err := de.Info()
		if err != nil {
			return nil
		}
		stem := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
		stemDrone := ""
		if parts := rsplit(stem, "_", 2); len(parts) >= 3 {
			stemDrone = parts[0]
		}
		parsed := stemDrone
		if filepath.Dir(p) != m.dir {
			parsed = filepath.Base(filepath.Dir(p))
		} else if parsed == "" {
			parsed = "unknown"
		}
		if filter != "" && strings.ToLower(parsed) != filter && strings.ToLower(stemDrone) != filter {
			return nil
		}

		m.mu.Lock()
		var activeRec *active
		for _, r := range m.active {
			if r.path == p && r.state == "recording" {
				activeRec = r
			}
		}
		m.mu.Unlock()

		details := m.ProbeFile(p)
		rec := Recording{
			Filename: filepath.Base(p), Name: parsed, DroneName: parsed,
			CreatedAt:        st.ModTime().UTC().Format(time.RFC3339Nano),
			CreatedTimestamp: unix(st.ModTime()),
			FileSizeBytes:    st.Size(), FileSizeHuman: FormatBytes(st.Size()),
			IsActive: activeRec != nil, Streams: details.Streams, StreamCount: details.StreamCount,
			FormatName: details.FormatName, DurationFormatted: "--:--",
		}
		rel, _ := filepath.Rel(m.dir, p)
		rec.RelativePath = filepath.ToSlash(rel)
		rec.DownloadURL = DownloadURL(rec.RelativePath)
		if activeRec != nil {
			d := round2(time.Since(activeRec.startTime).Seconds())
			rec.DurationSec, rec.DurationFormatted = &d, FormatDuration(d)
		} else if details.DurationSec != nil {
			rec.DurationSec = details.DurationSec
			if details.DurationFormatted != nil {
				rec.DurationFormatted = *details.DurationFormatted
			}
		}
		out = append(out, rec)
		return nil
	})

	desc := strings.ToLower(order) != "asc"
	less := func(i, j int) bool { return out[i].CreatedTimestamp < out[j].CreatedTimestamp }
	switch sortBy {
	case "duration":
		dur := func(r Recording) float64 {
			if r.DurationSec == nil {
				return 0
			}
			return *r.DurationSec
		}
		less = func(i, j int) bool { return dur(out[i]) < dur(out[j]) }
	case "size":
		less = func(i, j int) bool { return out[i].FileSizeBytes < out[j].FileSizeBytes }
	case "name":
		less = func(i, j int) bool { return strings.ToLower(out[i].Filename) < strings.ToLower(out[j].Filename) }
	}
	sort.SliceStable(out, func(i, j int) bool {
		if desc {
			return less(j, i)
		}
		return less(i, j)
	})
	if out == nil {
		out = []Recording{}
	}
	return out
}

func rsplit(s, sep string, n int) []string {
	var parts []string
	for len(parts) < n {
		i := strings.LastIndex(s, sep)
		if i < 0 {
			break
		}
		parts = append([]string{s[i+len(sep):]}, parts...)
		s = s[:i]
	}
	return append([]string{s}, parts...)
}

// ErrActive is returned when deleting a file that is still being recorded.
var ErrActive = errors.New("recording is currently active")

// Path resolves a relative recording path safely inside the recordings
// directory (no traversal), falling back to a basename search.
func (m *Manager) Path(rel string) (string, bool) {
	inside := func(p string) (string, bool) {
		r, err := filepath.EvalSymlinks(p)
		if err != nil {
			return "", false
		}
		if r != m.dir && !strings.HasPrefix(r, m.dir+string(filepath.Separator)) {
			return "", false
		}
		st, err := os.Stat(r)
		if err != nil || !st.Mode().IsRegular() {
			return "", false
		}
		return r, true
	}
	joined := filepath.Join(m.dir, filepath.FromSlash(rel))
	if !strings.HasPrefix(joined, m.dir+string(filepath.Separator)) {
		return "", false // traversal attempt
	}
	if p, ok := inside(joined); ok {
		return p, true
	}
	base := filepath.Base(rel)
	if base == "." || base == ".." || base == "/" || base == "" {
		return "", false
	}
	var found string
	_ = filepath.WalkDir(m.dir, func(p string, de fs.DirEntry, err error) error {
		if err == nil && !de.IsDir() && de.Name() == base {
			if r, ok := inside(p); ok {
				found = r
				return fs.SkipAll
			}
		}
		return nil
	})
	return found, found != ""
}

// Delete removes a recording unless it is being written.
func (m *Manager) Delete(rel string) error {
	p, ok := m.Path(rel)
	if !ok {
		return fs.ErrNotExist
	}
	m.mu.Lock()
	for _, r := range m.active {
		if r.path == p && (r.state == "waiting" || r.state == "recording") {
			m.mu.Unlock()
			return fmt.Errorf("cannot delete '%s': %w", filepath.Base(p), ErrActive)
		}
	}
	delete(m.probeCache, filepath.Base(p))
	m.mu.Unlock()
	return os.Remove(p)
}
