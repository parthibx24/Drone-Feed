package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/parthibx24/Drone-Feed/server/recorder"
)

// discoverStreams lists go2rtc streams named "<prefix>_*" (empty on failure).
func (g *Gateway) discoverStreams(ctx context.Context, prefix string) []string {
	base := g.cfg.Go2RTC.APIURL
	if base == "" || prefix == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/streams", nil)
	if err != nil {
		return nil
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		g.log.Debug("go2rtc discovery failed", "err", err)
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var streams map[string]json.RawMessage
	if json.NewDecoder(resp.Body).Decode(&streams) != nil {
		return nil
	}
	var out []string
	for name := range streams {
		if strings.HasPrefix(name, prefix+"_") {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func defaultStreams(prefix string) []string { return []string{prefix + "_camera", prefix + "_thermal"} }

// streamSources builds recorder ingest URLs for the configured ingest mode.
func (g *Gateway) streamSources(names []string) []recorder.Stream {
	c := g.cfg.Go2RTC
	out := make([]recorder.Stream, 0, len(names))
	for _, n := range names {
		var u string
		switch c.IngestMode {
		case "mp4":
			u = c.APIURL + "/api/stream.mp4?src=" + url.QueryEscape(n)
		case "hls":
			u = c.APIURL + "/api/stream.m3u8?src=" + url.QueryEscape(n)
		default:
			u = c.RTSPURL + "/" + n
		}
		out = append(out, recorder.Stream{Name: n, URL: u})
	}
	return out
}

func (g *Gateway) mediaRoutes(mux *http.ServeMux) {
	V, O := roleViewer, roleOperator

	// Cameras for one drone: the go2rtc URL browsers play from plus stream names.
	mux.HandleFunc("GET /api/v1/drones/{id}/cameras", g.route(V, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		streams := g.discoverStreams(r.Context(), d.cfg.StreamPrefix)
		discovered := len(streams) > 0
		if !discovered {
			streams = defaultStreams(d.cfg.StreamPrefix)
		}
		writeJSON(w, 200, map[string]any{
			"go2rtc_url": g.cfg.Go2RTC.PublicURL, "stream_prefix": d.cfg.StreamPrefix,
			"streams": streams, "discovered": discovered,
		})
	}))

	// Recording control is per drone; files are named by stream prefix.
	mux.HandleFunc("POST /api/v1/drones/{id}/recording/start", g.route(O, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		names := g.discoverStreams(r.Context(), d.cfg.StreamPrefix)
		if len(names) == 0 {
			names = defaultStreams(d.cfg.StreamPrefix)
		}
		res, err := g.rec.Start(d.cfg.StreamPrefix, g.streamSources(names))
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, 200, res)
	}))
	mux.HandleFunc("POST /api/v1/drones/{id}/recording/stop", g.route(O, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		writeJSON(w, 200, g.rec.Stop(d.cfg.StreamPrefix))
	}))
	mux.HandleFunc("GET /api/v1/drones/{id}/recording", g.route(V, func(w http.ResponseWriter, r *http.Request, d *Drone) {
		writeJSON(w, 200, g.rec.Status(d.cfg.StreamPrefix))
	}))

	// Library (all drones).
	mux.HandleFunc("GET /api/v1/recordings/status", g.authed(V, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, g.rec.StatusAll())
	}))
	mux.HandleFunc("GET /api/v1/recordings", g.authed(V, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		sortBy, order := q.Get("sort_by"), q.Get("order")
		if sortBy == "" {
			sortBy = "time"
		}
		if order == "" {
			order = "desc"
		}
		writeJSON(w, 200, g.rec.List(q.Get("name"), sortBy, order))
	}))
	download := g.authed(V, func(w http.ResponseWriter, r *http.Request) {
		p, ok := g.rec.Path(r.PathValue("path"))
		if !ok {
			writeErr(w, http.StatusNotFound, "recording file not found")
			return
		}
		ct := "video/mp4"
		if strings.HasSuffix(p, ".mkv") {
			ct = "video/x-matroska"
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(p)+`"`)
		http.ServeFile(w, r, p)
	})
	mux.HandleFunc("GET /api/v1/recordings/download/{path...}", download)
	mux.HandleFunc("HEAD /api/v1/recordings/download/{path...}", download)
	mux.HandleFunc("DELETE /api/v1/recordings/{path...}", g.authed(O, func(w http.ResponseWriter, r *http.Request) {
		rel := r.PathValue("path")
		err := g.rec.Delete(rel)
		switch {
		case err == nil:
			writeJSON(w, 200, map[string]any{"ok": true, "deleted": true, "filename": rel})
		case errors.Is(err, fs.ErrNotExist):
			writeErr(w, http.StatusNotFound, "recording file not found")
		case errors.Is(err, recorder.ErrActive):
			writeErr(w, http.StatusConflict, err.Error())
		default:
			writeErr(w, http.StatusInternalServerError, err.Error())
		}
	}))
}
