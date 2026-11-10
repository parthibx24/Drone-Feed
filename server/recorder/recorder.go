// Package recorder records a drone's camera streams server-side with FFmpeg
// (stream copy, no re-encoding) into one MKV per session.
//
// It is a port of the original Python recorder.py, with the same behaviour:
//
//  1. "waiting": probe every stream until each delivers a real video frame
//     (a go2rtc producer existing is not enough), up to a deadline;
//  2. "recording": start FFmpeg with all streams mapped into one file, then
//     confirm bytes actually reach disk; if not, tear down and wait again;
//  3. stop: send "q" so FFmpeg finalises the MKV index, then terminate/kill.
//
// JSON field names match the Python API so the web UI is unchanged.
package recorder

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	probeInputTimeoutUS  = "15000000" // 15 s socket timeout for single-frame probes
	probeTimeout         = 25 * time.Second
	recordInputTimeoutUS = "300000000" // 300 s: survive transient source drops
	defaultWaitTimeout   = 90 * time.Second
	outputGrace          = 6 * time.Second
	stderrKeep           = 50
)

// Stream is one camera feed to record.
type Stream struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type active struct {
	drone    string
	filename string
	path     string
	streams  []Stream

	state      string // waiting | recording | stopping | failed
	initTime   time.Time
	startTime  time.Time
	errMsg     string
	pending    []string
	cancelWait context.CancelFunc
	proc       *exec.Cmd
	stdin      interface {
		Write([]byte) (int, error)
		Close() error
	}
	exited      chan struct{}
	exitErr     error
	stderrLines []string
}

// Manager owns all active recordings and the recordings directory.
type Manager struct {
	dir     string
	log     *slog.Logger
	ffmpeg  string
	ffprobe string

	mu         sync.Mutex
	active     map[string]*active
	probeCache map[string]probeEntry

	// WaitTimeout overrides the default stream wait (tests).
	WaitTimeout time.Duration
}

type probeEntry struct {
	mtime time.Time
	data  FileDetails
}

// New prepares dir and locates ffmpeg/ffprobe on PATH.
func New(dir string, log *slog.Logger) (*Manager, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	m := &Manager{dir: abs, log: log, active: map[string]*active{}, probeCache: map[string]probeEntry{}, WaitTimeout: defaultWaitTimeout}
	m.ffmpeg, _ = exec.LookPath("ffmpeg")
	m.ffprobe, _ = exec.LookPath("ffprobe")
	if m.ffmpeg == "" {
		log.Warn("ffmpeg not found on PATH: recording is disabled")
	}
	return m, nil
}

// Dir is the absolute recordings directory.
func (m *Manager) Dir() string { return m.dir }

var unsafeChars = regexp.MustCompile(`[^\p{L}\p{N}_\-]`)

func clean(s string) string { return strings.Trim(strings.TrimSpace(s), `'"`) }

// Start begins waiting for the streams, then records them. It returns
// immediately; progress is visible through Status.
func (m *Manager) Start(drone string, streams []Stream) (map[string]any, error) {
	name := clean(drone)
	if name == "" {
		return nil, errors.New("drone name cannot be empty")
	}
	var cleaned []Stream
	for _, s := range streams {
		if n, u := clean(s.Name), clean(s.URL); n != "" && u != "" {
			cleaned = append(cleaned, Stream{Name: n, URL: u})
		}
	}
	if len(cleaned) == 0 {
		return nil, errors.New("no stream URLs to record")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if rec, ok := m.active[name]; ok {
		switch {
		case rec.state == "waiting":
			return map[string]any{
				"status": "already_waiting", "state": "waiting", "name": name, "drone_name": name,
				"message":             "Already waiting for camera streams to deliver frames",
				"elapsed_waiting_sec": round1(time.Since(rec.initTime).Seconds()),
			}, nil
		case rec.state == "recording" && !rec.hasExited():
			return map[string]any{
				"status": "already_recording", "state": "recording", "name": name, "drone_name": name,
				"filename": rec.filename, "start_time": unix(rec.startTime),
				"duration_sec": round1(time.Since(rec.startTime).Seconds()),
			}, nil
		default:
			if rec.cancelWait != nil {
				rec.cancelWait()
			}
			delete(m.active, name)
		}
	}
	if m.ffmpeg == "" {
		return nil, errors.New("ffmpeg binary not found on system PATH")
	}

	prefix := unsafeChars.ReplaceAllString(name, "_")
	droneDir := filepath.Join(m.dir, prefix)
	if err := os.MkdirAll(droneDir, 0o755); err != nil {
		return nil, err
	}
	filename := fmt.Sprintf("%s_%s.mkv", prefix, time.Now().UTC().Format("20060102_150405"))
	ctx, cancel := context.WithCancel(context.Background())
	rec := &active{
		drone: name, filename: filename, path: filepath.Join(droneDir, filename),
		streams: cleaned, state: "waiting", initTime: time.Now(), cancelWait: cancel,
	}
	m.active[name] = rec
	go m.waitAndCapture(ctx, rec)

	return map[string]any{
		"status": "waiting_for_stream", "state": "waiting", "name": name, "drone_name": name,
		"filename": filename, "streams": cleaned,
		"message":    "Waiting for camera streams to deliver video frames…",
		"init_time":  unix(rec.initTime),
		"created_at": rec.initTime.UTC().Format(time.RFC3339),
	}, nil
}

func (m *Manager) setState(rec *active, f func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f()
}

func (m *Manager) waitAndCapture(ctx context.Context, rec *active) {
	deadline := time.Now().Add(m.WaitTimeout)
	var nextLog time.Time
	m.log.Info("recording: waiting for camera frames", "drone", rec.drone, "streams", len(rec.streams))

	for {
		// Phase 1: every stream must deliver a frame (probed in parallel).
		ready := false
		for time.Now().Before(deadline) {
			results := make([]bool, len(rec.streams))
			var wg sync.WaitGroup
			for i, s := range rec.streams {
				wg.Add(1)
				go func(i int, s Stream) {
					defer wg.Done()
					results[i] = m.probe(ctx, s)
				}(i, s)
			}
			wg.Wait()
			if ctx.Err() != nil {
				return // cancelled by Stop
			}
			var pending []string
			for i, ok := range results {
				if !ok {
					pending = append(pending, rec.streams[i].Name)
				}
			}
			m.setState(rec, func() { rec.pending = pending })
			if len(pending) == 0 {
				ready = true
				break
			}
			if time.Now().After(nextLog) {
				m.log.Info("recording: still waiting for frames", "drone", rec.drone, "pending", strings.Join(pending, ", "))
				nextLog = time.Now().Add(5 * time.Second)
			}
			if !sleep(ctx, time.Second) {
				return
			}
		}
		if !ready {
			names := make([]string, len(rec.streams))
			for i, s := range rec.streams {
				names[i] = s.Name
			}
			m.setState(rec, func() {
				rec.state = "failed"
				rec.errMsg = fmt.Sprintf("No video frames received from streams (%s) within %ds",
					strings.Join(names, ", "), int(m.WaitTimeout.Seconds()))
			})
			m.log.Error("recording failed", "drone", rec.drone, "err", rec.errMsg)
			return
		}

		// Phase 2: start FFmpeg.
		if err := m.spawn(rec); err != nil {
			m.setState(rec, func() { rec.state, rec.errMsg = "failed", err.Error() })
			m.log.Error("recording: ffmpeg start failed", "drone", rec.drone, "err", err)
			return
		}
		m.log.Info("recording started", "drone", rec.drone, "file", rec.filename)

		// Phase 3: confirm data lands on disk.
		if m.waitForData(ctx, rec) {
			return
		}
		if ctx.Err() != nil {
			return
		}
		m.mu.Lock()
		errMsg := rec.errMsg
		m.mu.Unlock()
		if errMsg == "" {
			errMsg = "FFmpeg produced no data (stream delivered no frames)"
		}
		m.log.Warn("recording: no data, waiting again", "drone", rec.drone, "err", errMsg)
		m.terminate(rec)
		_ = os.Remove(rec.path)
		if !time.Now().Before(deadline) {
			m.setState(rec, func() { rec.state, rec.errMsg = "failed", errMsg })
			return
		}
		m.setState(rec, func() { rec.state, rec.startTime, rec.errMsg = "waiting", time.Time{}, "" })
		if !sleep(ctx, time.Second) {
			return
		}
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func inputOpts(url, timeoutUS string) []string {
	if strings.HasPrefix(url, "rtsp://") || strings.HasPrefix(url, "rtsps://") {
		return []string{"-rtsp_transport", "tcp", "-timeout", timeoutUS}
	}
	return []string{"-rw_timeout", timeoutUS}
}

// probe reads one real video frame from the exact URL FFmpeg will record.
func (m *Manager) probe(ctx context.Context, s Stream) bool {
	if m.ffmpeg == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	args := append([]string{"-v", "error", "-nostdin"}, inputOpts(s.URL, probeInputTimeoutUS)...)
	args = append(args, "-i", s.URL, "-map", "0:v:0", "-c", "copy", "-frames:v", "1", "-f", "null", "-")
	out, err := exec.CommandContext(ctx, m.ffmpeg, args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if len(msg) > 300 {
			msg = msg[len(msg)-300:]
		}
		m.log.Debug("frame probe failed", "stream", s.Name, "err", err, "ffmpeg", msg)
		return false
	}
	return true
}

func (m *Manager) spawn(rec *active) error {
	args := []string{"-y", "-hide_banner", "-loglevel", "warning"}
	for _, s := range rec.streams {
		args = append(args, inputOpts(s.URL, recordInputTimeoutUS)...)
		args = append(args, "-i", s.URL)
	}
	for i := range rec.streams {
		args = append(args, "-map", fmt.Sprintf("%d:v", i))
	}
	args = append(args, "-c", "copy", rec.path)

	cmd := exec.Command(m.ffmpeg, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	exited := make(chan struct{})
	m.setState(rec, func() {
		rec.proc, rec.stdin, rec.exited = cmd, stdin, exited
		rec.stderrLines, rec.errMsg = nil, ""
		rec.state, rec.startTime = "recording", time.Now()
	})
	go func() {
		buf := make([]byte, 4096)
		var partial string
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				lines := strings.Split(partial+string(buf[:n]), "\n")
				partial = lines[len(lines)-1]
				m.mu.Lock()
				for _, l := range lines[:len(lines)-1] {
					if l = strings.TrimSpace(l); l != "" {
						rec.stderrLines = append(rec.stderrLines, l)
					}
				}
				if len(rec.stderrLines) > stderrKeep {
					rec.stderrLines = rec.stderrLines[len(rec.stderrLines)-stderrKeep:]
				}
				m.mu.Unlock()
			}
			if err != nil {
				break
			}
		}
		werr := cmd.Wait()
		m.mu.Lock()
		rec.exitErr = werr
		m.mu.Unlock()
		close(exited)
	}()
	return nil
}

func (rec *active) hasExited() bool {
	if rec.exited == nil {
		return false
	}
	select {
	case <-rec.exited:
		return true
	default:
		return false
	}
}

// waitForData returns true once the output file has bytes.
func (m *Manager) waitForData(ctx context.Context, rec *active) bool {
	deadline := time.Now().Add(outputGrace)
	for time.Now().Before(deadline) {
		if rec.hasExited() {
			m.mu.Lock()
			if n := len(rec.stderrLines); n > 0 {
				rec.errMsg = strings.Join(rec.stderrLines[max(0, n-10):], "\n")
			} else {
				rec.errMsg = fmt.Sprintf("FFmpeg exited early: %v", rec.exitErr)
			}
			m.mu.Unlock()
			return false
		}
		if st, err := os.Stat(rec.path); err == nil && st.Size() > 0 {
			return true
		}
		if !sleep(ctx, 500*time.Millisecond) {
			return false
		}
	}
	m.setState(rec, func() { rec.errMsg = "FFmpeg started but no data was written (stream delivered no frames)" })
	return false
}

// terminate stops FFmpeg gracefully: "q" (finalises the MKV), then SIGTERM, then SIGKILL.
func (m *Manager) terminate(rec *active) {
	m.mu.Lock()
	proc, stdin, exited := rec.proc, rec.stdin, rec.exited
	m.mu.Unlock()
	if proc == nil || exited == nil {
		return
	}
	if !rec.hasExited() {
		if stdin != nil {
			_, _ = stdin.Write([]byte("q\n"))
			_ = stdin.Close()
		}
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			m.log.Warn("ffmpeg ignored 'q'; terminating", "drone", rec.drone)
			_ = proc.Process.Signal(os.Interrupt)
			select {
			case <-exited:
			case <-time.After(3 * time.Second):
				_ = proc.Process.Kill()
				<-exited
			}
		}
	}
	m.setState(rec, func() { rec.proc, rec.stdin = nil, nil })
}

// Stop finishes the drone's recording (or cancels a pending one).
func (m *Manager) Stop(drone string) map[string]any {
	name := clean(drone)
	m.mu.Lock()
	rec, ok := m.active[name]
	if !ok {
		m.mu.Unlock()
		return map[string]any{
			"status": "not_recording", "state": "idle", "name": name, "drone_name": name,
			"message": fmt.Sprintf("No active recording for drone '%s'", name),
		}
	}
	if rec.state == "waiting" {
		rec.cancelWait()
		delete(m.active, name)
		m.mu.Unlock()
		m.log.Info("recording cancelled while waiting", "drone", name)
		return map[string]any{
			"status": "recording_cancelled", "state": "idle", "name": name, "drone_name": name,
			"message": "Recording cancelled while waiting for stream",
		}
	}
	rec.state = "stopping"
	rec.cancelWait()
	started := rec.startTime
	if started.IsZero() {
		started = rec.initTime
	}
	m.mu.Unlock()

	duration := time.Since(started).Seconds()
	m.log.Info("stopping recording", "drone", name, "file", rec.filename)
	m.terminate(rec)

	m.mu.Lock()
	delete(m.active, name)
	m.mu.Unlock()

	var size int64
	rel := rec.filename
	if st, err := os.Stat(rec.path); err == nil {
		size = st.Size()
		if r, err := filepath.Rel(m.dir, rec.path); err == nil {
			rel = filepath.ToSlash(r)
		}
	}
	return map[string]any{
		"status": "recording_stopped", "state": "idle", "name": name, "drone_name": name,
		"filename": rec.filename, "relative_path": rel,
		"duration_sec": round2(duration), "duration_formatted": FormatDuration(duration),
		"file_size_bytes": size, "file_size_human": FormatBytes(size),
		"download_url": DownloadURL(rel), "details": m.ProbeFile(rec.path),
	}
}

// Status describes one drone's recording.
func (m *Manager) Status(drone string) map[string]any {
	name := clean(drone)
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.active[name]
	if !ok {
		return map[string]any{"is_recording": false, "state": "idle", "name": name, "drone_name": name}
	}
	return m.statusLocked(name, rec)
}

// StatusAll lists every active recording (by_drone is keyed by drone name).
func (m *Manager) StatusAll() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := []map[string]any{}
	byDrone := map[string]any{}
	names := make([]string, 0, len(m.active))
	for n := range m.active {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		rec, ok := m.active[n]
		if !ok {
			continue
		}
		st := m.statusLocked(n, rec)
		if st["is_recording"] == true {
			list = append(list, st)
			byDrone[n] = st
		}
	}
	return map[string]any{"active_count": len(list), "recordings": list, "by_drone": byDrone}
}

func (m *Manager) statusLocked(name string, rec *active) map[string]any {
	switch rec.state {
	case "waiting":
		return map[string]any{
			"is_recording": true, "state": "waiting", "name": name, "drone_name": name,
			"filename": rec.filename, "init_time": unix(rec.initTime),
			"elapsed_waiting_sec": round1(time.Since(rec.initTime).Seconds()),
			"pending_streams":     append([]string{}, rec.pending...),
			"message":             "Waiting for camera streams to deliver video frames…",
		}
	case "recording":
		if rec.hasExited() {
			delete(m.active, name)
			return map[string]any{"is_recording": false, "state": "idle", "name": name, "drone_name": name}
		}
		elapsed := time.Since(rec.startTime).Seconds()
		var size int64
		if st, err := os.Stat(rec.path); err == nil {
			size = st.Size()
		}
		return map[string]any{
			"is_recording": true, "state": "recording", "name": name, "drone_name": name,
			"filename": rec.filename, "start_time": unix(rec.startTime),
			"created_at":   rec.startTime.UTC().Format(time.RFC3339),
			"duration_sec": round1(elapsed), "duration_formatted": FormatDuration(elapsed),
			"file_size_bytes": size, "file_size_human": FormatBytes(size),
		}
	}
	return map[string]any{"is_recording": false, "state": "idle", "name": name, "drone_name": name, "error": rec.errMsg}
}

// Shutdown stops every recording (server exit), finalising files.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	names := make([]string, 0, len(m.active))
	for n := range m.active {
		names = append(names, n)
	}
	m.mu.Unlock()
	for _, n := range names {
		m.Stop(n)
	}
}

func unix(t time.Time) float64 { return float64(t.UnixNano()) / 1e9 }
func round1(v float64) float64 { return float64(int64(v*10+0.5)) / 10 }
func round2(v float64) float64 { return float64(int64(v*100+0.5)) / 100 }

// DownloadURL is the API path for a recording's relative path.
func DownloadURL(rel string) string { return "/api/v1/recordings/download/" + rel }
