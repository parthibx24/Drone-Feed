package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"mime/multipart"
	"net/http"
	"time"
)

// alerter delivers AI events to Telegram and/or a webhook. Deliveries are
// best effort: a failure is logged, never retried, and never blocks detection.
type alerter struct {
	cfg  AIAlertsConfig
	log  *slog.Logger
	http *http.Client
	api  string // Telegram API base, replaceable in tests
}

func newAlerter(cfg AIAlertsConfig, log *slog.Logger) *alerter {
	return &alerter{cfg: cfg, log: log, http: &http.Client{Timeout: 20 * time.Second}, api: "https://api.telegram.org"}
}

func alertText(e AIEvent) string {
	s := fmt.Sprintf("%s detected — %s (%s camera, %.0f%%)\n%s",
		e.Label, e.DroneName, e.Kind, e.Confidence*100, e.At.Local().Format("2006-01-02 15:04:05"))
	if e.Geo != nil {
		s += fmt.Sprintf("\nApprox. position (±%s): https://www.google.com/maps?q=%.6f,%.6f", geoError(e.Geo.DistanceM), e.Geo.Lat, e.Geo.Lon)
	} else if e.Drone != nil {
		s += fmt.Sprintf("\nDrone position: https://www.google.com/maps?q=%.6f,%.6f", e.Drone.Lat, e.Drone.Lon)
	}
	return s
}

// geoError is a rough uncertainty for a ground estimate at this distance.
func geoError(distanceM float64) string {
	return fmt.Sprintf("%.0f m", 10+distanceM*0.15)
}

func (a *alerter) send(e AIEvent, snapshot []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if a.cfg.Telegram.BotToken != "" && a.cfg.Telegram.ChatID != "" {
		if err := a.telegram(ctx, e, snapshot); err != nil {
			a.log.Warn("telegram alert failed", "err", err, "event", e.ID)
		}
	}
	if a.cfg.WebhookURL != "" {
		if err := a.webhook(ctx, e); err != nil {
			a.log.Warn("webhook alert failed", "err", err, "event", e.ID)
		}
	}
}

func (a *alerter) telegram(ctx context.Context, e AIEvent, snapshot []byte) error {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("chat_id", a.cfg.Telegram.ChatID)
	method := "sendMessage"
	if len(snapshot) > 0 {
		method = "sendPhoto"
		_ = mw.WriteField("caption", alertText(e))
		fw, err := mw.CreateFormFile("photo", e.ID+".jpg")
		if err != nil {
			return err
		}
		if _, err := fw.Write(snapshot); err != nil {
			return err
		}
	} else {
		_ = mw.WriteField("text", alertText(e))
	}
	if err := mw.Close(); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.api+"/bot"+a.cfg.Telegram.BotToken+"/"+method, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := a.http.Do(req)
	if err != nil {
		// The URL contains the bot token; report only the status, not the URL.
		return fmt.Errorf("request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func (a *alerter) webhook(ctx context.Context, e AIEvent) error {
	payload, err := json.Marshal(map[string]any{"type": "ai_event", "event": e, "text": alertText(e)})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.WebhookURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
	}
	return nil
}
