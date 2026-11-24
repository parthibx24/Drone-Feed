package gateway

// Reads, every few seconds, what a DJI aircraft's link can do right now.
// dji-bridge answers GET {bridge.api} with the aircraft and the command
// kinds its current link carries out: StarWatch Pilot on the remote
// controller can fly, DJI Pilot 2 (Cloud API) can only move the camera.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const (
	bridgePollEvery = 5 * time.Second
	// bridgeFresh: an answer older than this no longer counts, and the
	// dashboard falls back to every DJI feature (see profileFeatures).
	bridgeFresh   = 20 * time.Second
	bridgeMaxBody = 64 << 10
)

var bridgeHTTP = &http.Client{Timeout: 5 * time.Second}

// bridgeAircraft is the part of dji-bridge's aircraft JSON used here.
type bridgeAircraft struct {
	Model        string   `json:"model"`
	Online       bool     `json:"online"`
	Capabilities []string `json:"capabilities"`
}

// pollBridge keeps d's bridge status current until ctx ends.
func (d *Drone) pollBridge(ctx context.Context) {
	b := d.cfg.Bridge
	t := time.NewTicker(bridgePollEvery)
	defer t.Stop()
	failing := false
	for {
		st, err := fetchBridge(ctx, b)
		switch {
		case err == nil:
			d.mu.Lock()
			d.bridgeSt = st
			d.mu.Unlock()
			if failing {
				d.log.Info("aircraft service answering again")
			}
			failing = false
		case ctx.Err() != nil:
			return
		case !failing:
			d.log.Warn("aircraft service not answering", "err", err)
			failing = true
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func fetchBridge(ctx context.Context, b *BridgeConfig) (*bridgeStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.API, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+b.Token)
	resp, err := bridgeHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		// The bridge is up but has not seen this aircraft since it started.
		return &bridgeStatus{at: time.Now(), noLink: true}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	var a bridgeAircraft
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, bridgeMaxBody)).Decode(&a); err != nil {
		return nil, err
	}
	// No commands while online is a model the bridge can't command (see
	// profileFeatures), not a missing remote controller.
	return &bridgeStatus{at: time.Now(), model: a.Model, online: a.Online, kinds: a.Capabilities,
		noLink: len(a.Capabilities) == 0 && !a.Online}, nil
}

// bridgeNow is the last bridge answer, or nil if there is none lately.
func (d *Drone) bridgeNow() *bridgeStatus {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.bridgeSt == nil || time.Since(d.bridgeSt.at) > bridgeFresh {
		return nil
	}
	return d.bridgeSt
}
