package gateway

import (
	"fmt"
	"path/filepath"
	"strings"
)

// AIConfig turns on object detection and the features built on it
// (docs/09-ai.md). Every sub-block is optional; leaving one out turns that
// feature off.
type AIConfig struct {
	Enabled bool `yaml:"enabled"`
	// IngestToken authenticates the detector service. It is accepted only on
	// /api/v1/ai/{targets,detections,events}.
	IngestToken string `yaml:"ingest_token"`
	// DataDir holds events.jsonl and snapshots/ (default <recordings_dir>/.ai).
	DataDir       string  `yaml:"data_dir"`
	RetentionDays int     `yaml:"retention_days"`
	RunWhen       string  `yaml:"run_when"` // auto (armed or watched) | always
	FPS           float64 `yaml:"fps"`
	MinConfidence float64 `yaml:"min_confidence"`

	Camera     AICameraConfig     `yaml:"camera"`
	Alerts     AIAlertsConfig     `yaml:"alerts"`
	AutoRecord AIAutoRecordConfig `yaml:"auto_record"`
	Vision     AIVisionConfig     `yaml:"vision"`
	Follow     AIFollowConfig     `yaml:"follow"`
}

// AICameraConfig gives the horizontal field of view used for ground position
// estimates.
type AICameraConfig struct {
	VisibleHFOVDeg float64 `yaml:"visible_hfov_deg"`
	ThermalHFOVDeg float64 `yaml:"thermal_hfov_deg"`
}

type AIAlertsConfig struct {
	Labels     []string `yaml:"labels"`
	CooldownS  float64  `yaml:"cooldown_s"`
	Telegram   struct {
		BotToken string `yaml:"bot_token"`
		ChatID   string `yaml:"chat_id"`
	} `yaml:"telegram"`
	WebhookURL string `yaml:"webhook_url"`
}

func (a AIAlertsConfig) configured() bool {
	return len(a.Labels) > 0 && ((a.Telegram.BotToken != "" && a.Telegram.ChatID != "") || a.WebhookURL != "")
}

type AIAutoRecordConfig struct {
	Labels     []string `yaml:"labels"`
	StopAfterS float64  `yaml:"stop_after_s"`
}

type AIVisionConfig struct {
	APIKey string `yaml:"api_key"`
	Model  string `yaml:"model"`
	Effort string `yaml:"effort"`
	// BaseURL overrides the Anthropic API address (a proxy, or tests).
	BaseURL string `yaml:"base_url"`
}

type AIFollowConfig struct {
	MaxRateDegS float64 `yaml:"max_rate_deg_s"`
	// Disabled turns click-to-follow off.
	Disabled bool `yaml:"disabled"`
}

func (c *AIConfig) normalize(recordingsDir string) error {
	if !c.Enabled {
		return nil
	}
	if len(c.IngestToken) < 16 {
		return fmt.Errorf("ai.ingest_token must be at least 16 characters (e.g. openssl rand -hex 24)")
	}
	if c.DataDir == "" {
		c.DataDir = filepath.Join(recordingsDir, ".ai")
	}
	if c.RetentionDays <= 0 {
		c.RetentionDays = 30
	}
	c.RunWhen = strings.ToLower(strings.TrimSpace(c.RunWhen))
	if c.RunWhen == "" {
		c.RunWhen = "auto"
	}
	if c.RunWhen != "auto" && c.RunWhen != "always" {
		return fmt.Errorf("ai.run_when must be auto or always")
	}
	if c.FPS <= 0 {
		c.FPS = 3
	}
	if c.FPS > 15 {
		return fmt.Errorf("ai.fps must be at most 15")
	}
	if c.MinConfidence <= 0 {
		c.MinConfidence = 0.35
	}
	if c.MinConfidence >= 1 {
		return fmt.Errorf("ai.min_confidence must be below 1")
	}
	if c.Camera.VisibleHFOVDeg <= 0 {
		c.Camera.VisibleHFOVDeg = 62
	}
	if c.Camera.ThermalHFOVDeg <= 0 {
		c.Camera.ThermalHFOVDeg = 42
	}
	for _, h := range []float64{c.Camera.VisibleHFOVDeg, c.Camera.ThermalHFOVDeg} {
		if h >= 170 {
			return fmt.Errorf("ai.camera: field of view must be below 170 degrees")
		}
	}
	c.Alerts.Labels = lowerAll(c.Alerts.Labels)
	if c.Alerts.CooldownS <= 0 {
		c.Alerts.CooldownS = 120
	}
	if c.Alerts.WebhookURL != "" && !strings.HasPrefix(c.Alerts.WebhookURL, "http://") && !strings.HasPrefix(c.Alerts.WebhookURL, "https://") {
		return fmt.Errorf("ai.alerts.webhook_url must be an http(s) URL")
	}
	c.AutoRecord.Labels = lowerAll(c.AutoRecord.Labels)
	if c.AutoRecord.StopAfterS <= 0 {
		c.AutoRecord.StopAfterS = 30
	}
	if c.Vision.Model == "" {
		c.Vision.Model = "claude-opus-5-5"
	}
	c.Vision.Effort = strings.ToLower(c.Vision.Effort)
	if c.Vision.Effort == "" {
		c.Vision.Effort = "medium"
	}
	switch c.Vision.Effort {
	case "low", "medium", "high", "xhigh", "max":
	default:
		return fmt.Errorf("ai.vision.effort must be low, medium, high, xhigh or max")
	}
	if c.Follow.MaxRateDegS <= 0 {
		c.Follow.MaxRateDegS = 10
	}
	if c.Follow.MaxRateDegS > 30 {
		c.Follow.MaxRateDegS = 30
	}
	return nil
}

func lowerAll(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
			out = append(out, s)
		}
	}
	return out
}
