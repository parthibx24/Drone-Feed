package gateway

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"
)

// AIEvent is one newly confirmed object (docs/09-ai.md).
type AIEvent struct {
	ID          string       `json:"id"`
	DroneID     string       `json:"drone_id"`
	DroneName   string       `json:"drone_name"`
	Stream      string       `json:"stream"`
	Kind        string       `json:"kind"`
	At          time.Time    `json:"at"`
	TrackID     int          `json:"track_id"`
	Label       string       `json:"label"`
	Group       string       `json:"group"`
	Confidence  float64      `json:"confidence"`
	Box         [4]float64   `json:"box"`
	Drone       *DronePose   `json:"drone"`
	Geo         *GeoEstimate `json:"geo"`
	SnapshotURL string       `json:"snapshot_url"`
	Alerted     bool         `json:"alerted"`
	Recording   bool         `json:"recording"`
}

// eventQuery filters the event log. Zero values match everything.
type eventQuery struct {
	Drone, Group, Label string
	Since, Until        time.Time
	Limit               int
}

const maxEventsInMemory = 20000

var eventIDRe = regexp.MustCompile(`^[A-Za-z0-9_.\-]{1,120}$`)
var unsafeIDChars = regexp.MustCompile(`[^A-Za-z0-9_\-]`)

// eventStore keeps the event log in <dir>/events.jsonl and snapshots in
// <dir>/snapshots/<id>.jpg, with an in-memory index for queries.
type eventStore struct {
	dir       string
	retention time.Duration
	mu        sync.RWMutex
	events    []AIEvent // oldest first
}

func newEventStore(dir string, retentionDays int) (*eventStore, error) {
	if err := os.MkdirAll(filepath.Join(dir, "snapshots"), 0o755); err != nil {
		return nil, err
	}
	s := &eventStore{dir: dir, retention: time.Duration(retentionDays) * 24 * time.Hour}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, s.prune(time.Now())
}

func (s *eventStore) logPath() string { return filepath.Join(s.dir, "events.jsonl") }

func (s *eventStore) snapshotPath(id string) string {
	return filepath.Join(s.dir, "snapshots", id+".jpg")
}

func (s *eventStore) load() error {
	f, err := os.Open(s.logPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		var e AIEvent
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.ID != "" {
			s.events = append(s.events, e)
		}
	}
	sort.SliceStable(s.events, func(i, j int) bool { return s.events[i].At.Before(s.events[j].At) })
	if len(s.events) > maxEventsInMemory {
		s.events = s.events[len(s.events)-maxEventsInMemory:]
	}
	return sc.Err()
}

// newID builds a unique, path-safe event id.
func newEventID(at time.Time, droneID string, track int) string {
	return fmt.Sprintf("%s-%s-%d", at.UTC().Format("20060102T150405.000Z"),
		unsafeIDChars.ReplaceAllString(droneID, "_"), track)
}

// add stores the snapshot (if any) and appends the event.
func (s *eventStore) add(e AIEvent, snapshot []byte) error {
	if len(snapshot) > 0 {
		if err := os.WriteFile(s.snapshotPath(e.ID), snapshot, 0o644); err != nil {
			return err
		}
		e.SnapshotURL = "/api/v1/ai/events/" + e.ID + "/snapshot"
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(s.logPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, werr := f.Write(append(line, '\n'))
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return werr
	}
	s.events = append(s.events, e)
	if len(s.events) > maxEventsInMemory {
		s.events = s.events[len(s.events)-maxEventsInMemory:]
	}
	return nil
}

// update rewrites one event's mutable fields (alerted, recording) in memory.
// The log keeps the original line; flags are informational.
func (s *eventStore) update(id string, f func(*AIEvent)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.events) - 1; i >= 0; i-- {
		if s.events[i].ID == id {
			f(&s.events[i])
			return
		}
	}
}

func (s *eventStore) query(q eventQuery) []AIEvent {
	if q.Limit <= 0 {
		q.Limit = 200
	}
	if q.Limit > 1000 {
		q.Limit = 1000
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []AIEvent{}
	for i := len(s.events) - 1; i >= 0 && len(out) < q.Limit; i-- {
		e := s.events[i]
		switch {
		case q.Drone != "" && e.DroneID != q.Drone,
			q.Group != "" && e.Group != q.Group,
			q.Label != "" && e.Label != q.Label,
			!q.Since.IsZero() && e.At.Before(q.Since),
			!q.Until.IsZero() && e.At.After(q.Until):
			continue
		}
		out = append(out, e)
	}
	return out
}

// snapshot returns the JPEG path for a known event id.
func (s *eventStore) snapshot(id string) (string, bool) {
	if !eventIDRe.MatchString(id) {
		return "", false
	}
	p := s.snapshotPath(id)
	if _, err := os.Stat(p); err != nil {
		return "", false
	}
	return p, true
}

// prune drops events older than the retention period, from memory, the log
// and the snapshot folder.
func (s *eventStore) prune(now time.Time) error {
	cutoff := now.Add(-s.retention)
	s.mu.Lock()
	defer s.mu.Unlock()
	keep := s.events[:0:0]
	var dropped []string
	for _, e := range s.events {
		if e.At.Before(cutoff) {
			dropped = append(dropped, e.ID)
		} else {
			keep = append(keep, e)
		}
	}
	if len(dropped) == 0 {
		return nil
	}
	tmp := s.logPath() + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	for _, e := range keep {
		line, _ := json.Marshal(e)
		w.Write(append(line, '\n'))
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.logPath()); err != nil {
		return err
	}
	s.events = keep
	for _, id := range dropped {
		_ = os.Remove(s.snapshotPath(id))
	}
	return nil
}
