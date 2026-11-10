package gateway

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the gateway configuration file (YAML). ${VAR} is expanded from
// the environment, so secrets can live outside the file.
type Config struct {
	Listen           string             `yaml:"listen"`
	GCSSystemID      uint8              `yaml:"gcs_system_id"`
	GCSComponentID   uint8              `yaml:"gcs_component_id"`
	CORSOrigins      []string           `yaml:"cors_origins"`
	Tokens           []Token            `yaml:"tokens"`
	AllowRawCommands bool               `yaml:"allow_raw_commands"`
	TelemetryHz      float64            `yaml:"telemetry_hz"`
	Limits           Limits             `yaml:"limits"`
	StreamRates      map[string]float64 `yaml:"stream_rates"`
	Drones           []DroneConfig      `yaml:"drones"`

	// Auth: people log in through the web UI; tokens are for scripts.
	Auth AuthConfig `yaml:"auth"`
	// WebDir holds the built React UI (index.html + assets). Empty = API only.
	WebDir string `yaml:"web_dir"`
	// Go2RTC is the central video hub (camera discovery and recording).
	Go2RTC Go2RTCConfig `yaml:"go2rtc"`
	// RecordingsDir stores server-side recordings.
	RecordingsDir string `yaml:"recordings_dir"`
}

// AuthConfig holds web users and session settings.
type AuthConfig struct {
	// SessionSecret signs login cookies. If empty a random one is generated at
	// startup (everyone is logged out on restart).
	SessionSecret string        `yaml:"session_secret"`
	SessionTTL    time.Duration `yaml:"session_ttl"`
	Users         []User        `yaml:"users"`
}

// User is a web login. PasswordHash is bcrypt ("drone-gateway hash-password").
type User struct {
	Username     string `yaml:"username"`
	PasswordHash string `yaml:"password_hash"`
	Role         string `yaml:"role"`
}

// Go2RTCConfig locates go2rtc.
type Go2RTCConfig struct {
	// PublicURL is what browsers use to play video.
	PublicURL string `yaml:"public_url"`
	// APIURL is what this server uses for discovery and mp4/hls ingest
	// (default: PublicURL).
	APIURL string `yaml:"api_url"`
	// RTSPURL is the RTSP base for recording ingest (default rtsp://<api host>:8554).
	RTSPURL string `yaml:"rtsp_url"`
	// IngestMode: rtsp (default), mp4 or hls.
	IngestMode string `yaml:"ingest_mode"`
}

// Token grants API access. Role is "viewer" (read + telemetry) or
// "operator" (everything, including flight commands).
type Token struct {
	Name  string `yaml:"name"`
	Token string `yaml:"token"`
	Role  string `yaml:"role"`
}

// Limits are server-side safety checks applied before anything is sent.
type Limits struct {
	MaxAltM         float64 `yaml:"max_alt_m" json:"max_alt_m"`
	MaxSpeedMS      float64 `yaml:"max_speed_m_s" json:"max_speed_m_s"`
	MaxMissionItems int     `yaml:"max_mission_items" json:"max_mission_items"`
}

// DroneConfig is one vehicle in the fleet.
type DroneConfig struct {
	ID       string `yaml:"id"`
	Name     string `yaml:"name"`
	MAVLink  string `yaml:"mavlink"`
	SystemID uint8  `yaml:"system_id"`
	// StreamPrefix: go2rtc streams named "<prefix>_*" belong to this drone
	// (e.g. kotowali_camera). Defaults to the id. Also names its recordings.
	StreamPrefix string `yaml:"stream_prefix"`
}

// DefaultStreamRates are requested from the autopilot on connect (Hz).
// They are deliberately modest: everything crosses a 4G link.
var DefaultStreamRates = map[string]float64{
	"GLOBAL_POSITION_INT": 4,
	"ATTITUDE":            4,
	"VFR_HUD":             2,
	"SYS_STATUS":          1,
	"GPS_RAW_INT":         1,
	"BATTERY_STATUS":      1,
	"EXTENDED_SYS_STATE":  1,
	"MISSION_CURRENT":     1,
	"EKF_STATUS_REPORT":   1,
	"HOME_POSITION":       0.2,
}

// streamMessageIDs maps the names allowed in stream_rates to MAVLink IDs.
var streamMessageIDs = map[string]uint32{
	"SYS_STATUS": 1, "GPS_RAW_INT": 24, "ATTITUDE": 30, "GLOBAL_POSITION_INT": 33,
	"MISSION_CURRENT": 42, "VFR_HUD": 74, "BATTERY_STATUS": 147, "EKF_STATUS_REPORT": 193,
	"HOME_POSITION": 242, "EXTENDED_SYS_STATE": 245, "RC_CHANNELS": 65, "SERVO_OUTPUT_RAW": 36,
	"LOCAL_POSITION_NED": 32, "NAV_CONTROLLER_OUTPUT": 62, "TERRAIN_REPORT": 136,
	"VIBRATION": 241, "WIND": 168,
}

var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// expandEnv replaces only ${VAR}. Bare $ is left alone, because bcrypt hashes
// ("$2a$12$...") contain it and os.ExpandEnv would mangle them.
func expandEnv(s string) string {
	return envRef.ReplaceAllStringFunc(s, func(m string) string {
		return os.Getenv(envRef.FindStringSubmatch(m)[1])
	})
}

// LoadConfig reads, expands and validates a config file.
func LoadConfig(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	dec := yaml.NewDecoder(strings.NewReader(expandEnv(string(raw))))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &cfg, cfg.Normalize()
}

// Normalize fills defaults and validates.
func (c *Config) Normalize() error {
	if c.Listen == "" {
		c.Listen = ":8091"
	}
	if c.GCSSystemID == 0 {
		c.GCSSystemID = 255
	}
	if c.GCSComponentID == 0 {
		c.GCSComponentID = 190
	}
	if c.TelemetryHz <= 0 {
		c.TelemetryHz = 5
	}
	if c.Limits.MaxAltM <= 0 {
		c.Limits.MaxAltM = 120
	}
	if c.Limits.MaxSpeedMS <= 0 {
		c.Limits.MaxSpeedMS = 15
	}
	if c.Limits.MaxMissionItems <= 0 {
		c.Limits.MaxMissionItems = 500
	}
	if c.StreamRates == nil {
		c.StreamRates = DefaultStreamRates
	}
	for name := range c.StreamRates {
		if _, ok := streamMessageIDs[name]; !ok {
			return fmt.Errorf("stream_rates: unknown message %q", name)
		}
	}
	if c.Auth.SessionTTL <= 0 {
		c.Auth.SessionTTL = 12 * time.Hour
	}
	users := map[string]bool{}
	for i, u := range c.Auth.Users {
		if u.Username == "" || u.PasswordHash == "" {
			return fmt.Errorf("auth.users[%d]: username and password_hash are required", i)
		}
		if !strings.HasPrefix(u.PasswordHash, "$2") {
			return fmt.Errorf("auth.users[%d] %s: password_hash must be a bcrypt hash (use: drone-gateway hash-password)", i, u.Username)
		}
		if u.Role != "viewer" && u.Role != "operator" {
			return fmt.Errorf("auth.users[%d] %s: role must be viewer or operator", i, u.Username)
		}
		if users[u.Username] {
			return fmt.Errorf("auth.users: duplicate username %q", u.Username)
		}
		users[u.Username] = true
	}
	g := &c.Go2RTC
	g.PublicURL = strings.TrimRight(strings.Trim(strings.TrimSpace(g.PublicURL), `'"`), "/")
	g.APIURL = strings.TrimRight(strings.Trim(strings.TrimSpace(g.APIURL), `'"`), "/")
	g.RTSPURL = strings.TrimRight(strings.Trim(strings.TrimSpace(g.RTSPURL), `'"`), "/")
	if g.APIURL == "" {
		g.APIURL = g.PublicURL
	}
	if g.RTSPURL == "" && g.APIURL != "" {
		host := "127.0.0.1"
		if u, err := url.Parse(g.APIURL); err == nil && u.Hostname() != "" {
			host = u.Hostname()
		}
		g.RTSPURL = "rtsp://" + host + ":8554"
	}
	g.IngestMode = strings.ToLower(strings.TrimSpace(g.IngestMode))
	if g.IngestMode == "" {
		g.IngestMode = "rtsp"
	}
	if g.IngestMode != "rtsp" && g.IngestMode != "mp4" && g.IngestMode != "hls" {
		return fmt.Errorf("go2rtc.ingest_mode must be rtsp, mp4 or hls")
	}
	if c.RecordingsDir == "" {
		c.RecordingsDir = "recordings"
	}
	for i, t := range c.Tokens {
		if t.Token == "" || t.Name == "" {
			return fmt.Errorf("tokens[%d]: name and token are required", i)
		}
		if t.Role != "viewer" && t.Role != "operator" {
			return fmt.Errorf("tokens[%d] %s: role must be viewer or operator", i, t.Name)
		}
	}
	if len(c.Drones) == 0 {
		return fmt.Errorf("no drones configured")
	}
	seen := map[string]bool{}
	for i, d := range c.Drones {
		if d.ID == "" || d.MAVLink == "" {
			return fmt.Errorf("drones[%d]: id and mavlink are required", i)
		}
		if seen[d.ID] {
			return fmt.Errorf("drones: duplicate id %q", d.ID)
		}
		seen[d.ID] = true
		if d.Name == "" {
			c.Drones[i].Name = d.ID
		}
		if d.StreamPrefix == "" {
			c.Drones[i].StreamPrefix = d.ID
		}
	}
	return nil
}
