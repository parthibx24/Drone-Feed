package gateway

import (
	"os"
	"path/filepath"
	"testing"
)

func loadYAML(t *testing.T, body string) (*Config, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "gw.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return LoadConfig(p)
}

func TestStreamRatesEmptyMeansNone(t *testing.T) {
	cfg, err := loadYAML(t, "stream_rates: {}\ndrones: [{id: a, mavlink: 'udp:1.2.3.4:14560'}]\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.StreamRates) != 0 {
		t.Fatalf("stream_rates: {} must request nothing, got %v", cfg.StreamRates)
	}
}

func TestStreamRatesOmittedUsesDefaults(t *testing.T) {
	cfg, err := loadYAML(t, "drones: [{id: a, mavlink: 'udp:1.2.3.4:14560'}]\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.StreamRates) != len(DefaultStreamRates) {
		t.Fatalf("want defaults, got %v", cfg.StreamRates)
	}
}

func TestConfigRejectsBadInput(t *testing.T) {
	for name, body := range map[string]string{
		"no drones":    "listen: ':1'\n",
		"bad role":     "tokens: [{name: x, token: y, role: admin}]\ndrones: [{id: a, mavlink: 'udp:h:1'}]\n",
		"empty token":  "tokens: [{name: x, token: '${NOT_SET_ANYWHERE}', role: viewer}]\ndrones: [{id: a, mavlink: 'udp:h:1'}]\n",
		"unknown key":  "lisen: ':1'\ndrones: [{id: a, mavlink: 'udp:h:1'}]\n",
		"bad stream":   "stream_rates: {FOO: 1}\ndrones: [{id: a, mavlink: 'udp:h:1'}]\n",
		"duplicate id": "drones: [{id: a, mavlink: 'udp:h:1'}, {id: a, mavlink: 'udp:h:2'}]\n",
	} {
		if _, err := loadYAML(t, body); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestExpandEnvKeepsBcryptHashes(t *testing.T) {
	t.Setenv("GW_TEST_SECRET", "s3cret")
	hash := "$2a$12$abcdefghijklmnopqrstuuJ1lZ0bW5n3o3o1o2o3o4o5o6o7o8o9o"
	cfg, err := loadYAML(t, "auth:\n  session_secret: ${GW_TEST_SECRET}\n  users:\n    - {username: a, password_hash: \""+hash+"\", role: viewer}\ndrones: [{id: a, mavlink: 'udp:h:1'}]\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.SessionSecret != "s3cret" || cfg.Auth.Users[0].PasswordHash != hash {
		t.Fatalf("expansion: secret=%q hash=%q", cfg.Auth.SessionSecret, cfg.Auth.Users[0].PasswordHash)
	}
}
