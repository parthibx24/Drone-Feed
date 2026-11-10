package gateway

import (
	"math"
	"time"
)

// Telemetry is the JSON snapshot served by the API. Pointer fields are null
// until the corresponding MAVLink message has been received, so clients never
// mistake "unknown" for zero.
type Telemetry struct {
	DroneID           string   `json:"drone_id"`
	Name              string   `json:"name"`
	Online            bool     `json:"online"`
	LastHeartbeatAgeS *float64 `json:"last_heartbeat_age_s"`
	SystemID          uint8    `json:"system_id,omitempty"`
	Firmware          string   `json:"firmware,omitempty"`
	VehicleType       string   `json:"vehicle_type,omitempty"`

	Armed        bool   `json:"armed"`
	Mode         string `json:"mode"`
	SystemStatus string `json:"system_status,omitempty"`
	LandedState  string `json:"landed_state,omitempty"`

	Position *Position     `json:"position"`
	Attitude *Attitude     `json:"attitude"`
	Speed    *Speed        `json:"speed"`
	Battery  *Battery      `json:"battery"`
	GPS      *GPS          `json:"gps"`
	Home     *Home         `json:"home"`
	Mission  *MissionState `json:"mission"`
	EKF      *EKF          `json:"ekf"`
	Camera   *Peripheral   `json:"camera"`
	Gimbal   *GimbalState  `json:"gimbal"`

	UpdatedAt time.Time `json:"updated_at"`
}

type Position struct {
	Lat        float64  `json:"lat"`
	Lon        float64  `json:"lon"`
	AltMSLM    float64  `json:"alt_msl_m"`
	AltRelM    float64  `json:"alt_rel_m"`
	HeadingDeg *float64 `json:"heading_deg"`
	VNorthMS   float64  `json:"v_north_m_s"`
	VEastMS    float64  `json:"v_east_m_s"`
	VDownMS    float64  `json:"v_down_m_s"`
}

type Attitude struct {
	RollDeg  float64 `json:"roll_deg"`
	PitchDeg float64 `json:"pitch_deg"`
	YawDeg   float64 `json:"yaw_deg"`
}

type Speed struct {
	GroundMS    float64 `json:"ground_m_s"`
	AirMS       float64 `json:"air_m_s"`
	ClimbMS     float64 `json:"climb_m_s"`
	ThrottlePct int     `json:"throttle_pct"`
}

type Battery struct {
	VoltageV     *float64 `json:"voltage_v"`
	CurrentA     *float64 `json:"current_a"`
	RemainingPct *int     `json:"remaining_pct"`
	ConsumedMAh  *int     `json:"consumed_mah"`
}

type GPS struct {
	FixType    string   `json:"fix_type"`
	Satellites int      `json:"satellites"`
	HDOP       *float64 `json:"hdop"`
}

type Home struct {
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	AltMSLM float64 `json:"alt_msl_m"`
}

type MissionState struct {
	CurrentSeq int    `json:"current_seq"`
	Total      int    `json:"total"`
	State      string `json:"state,omitempty"`
}

type EKF struct {
	Flags   uint16 `json:"flags"`
	Healthy bool   `json:"healthy"`
}

type Peripheral struct {
	Present  bool      `json:"present"`
	LastSeen time.Time `json:"last_seen"`
}

type GimbalState struct {
	Present  bool      `json:"present"`
	YawLock  bool      `json:"yaw_lock"`
	LastSeen time.Time `json:"last_seen"`
}

// StatusText is one autopilot message (e.g. "PreArm: RC not found").
type StatusText struct {
	Time     time.Time `json:"time"`
	Severity string    `json:"severity"`
	Text     string    `json:"text"`
}

func f64(v float64) *float64 { return &v }
func ip(v int) *int          { return &v }

func deg(rad float32) float64 { return float64(rad) * 180 / math.Pi }

func round(v float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Round(v*p) / p
}
