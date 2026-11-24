package gateway

import (
	"math"
	"sync"
	"time"
)

// gimbalTracker estimates where the camera points. The C12 reports no
// attitude, so the estimate is the last commanded absolute angle plus the rate
// commands sent since, each counted for at most the bridge's dead-man time.
type gimbalTracker struct {
	mu        sync.Mutex
	known     bool    // an absolute angle was commanded since start-up
	pitch     float64 // deg, negative = down
	yaw       float64 // deg relative to the drone's nose, positive = right
	ratePitch float64 // deg/s currently commanded
	rateYaw   float64
	rateAt    time.Time
}

// rateDeadman mirrors gimbal-bridge's -rate-timeout default: a rate command
// moves the gimbal for at most this long unless it is repeated.
const rateDeadman = 600 * time.Millisecond

func (t *gimbalTracker) advance(now time.Time) {
	if t.rateAt.IsZero() || (t.ratePitch == 0 && t.rateYaw == 0) {
		t.rateAt = now
		return
	}
	dt := now.Sub(t.rateAt)
	expired := dt > rateDeadman
	if expired {
		dt = rateDeadman
	}
	s := dt.Seconds()
	t.pitch = clamp(t.pitch+t.ratePitch*s, -90, 30)
	t.yaw = wrap180(t.yaw + t.rateYaw*s)
	if expired {
		t.ratePitch, t.rateYaw = 0, 0 // the bridge's dead-man has stopped it
	}
	t.rateAt = now
}

func (t *gimbalTracker) setAbsolute(pitch, yaw float32) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.advance(time.Now())
	if !math.IsNaN(float64(pitch)) {
		t.pitch = clamp(float64(pitch), -90, 30)
		t.known = true
	}
	if !math.IsNaN(float64(yaw)) {
		t.yaw = wrap180(float64(yaw))
	}
	t.ratePitch, t.rateYaw = 0, 0
}

func (t *gimbalTracker) setRate(pitch, yaw float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.advance(time.Now())
	t.ratePitch, t.rateYaw = pitch, yaw
}

// estimate returns the current pointing estimate and whether it is usable.
func (t *gimbalTracker) estimate() (pitch, yaw float64, known bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.advance(time.Now())
	return t.pitch, t.yaw, t.known
}

// GeoEstimate is an approximate ground position of a detected object.
type GeoEstimate struct {
	Lat       float64 `json:"lat"`
	Lon       float64 `json:"lon"`
	DistanceM float64 `json:"distance_m"`
	Approx    bool    `json:"approx"`
}

// DronePose is the drone's position when an event was recorded.
type DronePose struct {
	Lat        float64  `json:"lat"`
	Lon        float64  `json:"lon"`
	AltRelM    float64  `json:"alt_rel_m"`
	HeadingDeg *float64 `json:"heading_deg"`
}

// maxGeoDistanceM drops estimates for rays that are nearly horizontal: a few
// degrees of gimbal error there moves the point by hundreds of metres.
const maxGeoDistanceM = 1500

// groundPoint projects the bottom-centre of box (normalised x, y, w, h) onto
// flat ground at the take-off height. hfov is the horizontal field of view and
// aspect is width/height of the frame. It returns nil when the ray does not hit
// the ground usefully.
func groundPoint(p DronePose, camPitch, camYaw, hfovDeg, aspect float64, box [4]float64) *GeoEstimate {
	if p.HeadingDeg == nil || p.AltRelM < 2 || aspect <= 0 {
		return nil
	}
	u := box[0] + box[2]/2 // feet: bottom centre
	v := box[1] + box[3]
	tanH := math.Tan(toRad(hfovDeg / 2))
	tanV := tanH / aspect
	alpha := math.Atan((u - 0.5) * 2 * tanH) // right of centre: positive
	beta := math.Atan((v - 0.5) * 2 * tanV)  // below centre: positive
	depression := toRad(-camPitch) + beta
	if depression < toRad(5) || depression > toRad(90) {
		if depression > toRad(90) && depression < toRad(100) {
			depression = toRad(90) // straight down, beyond nadir by a little
		} else {
			return nil
		}
	}
	dist := p.AltRelM / math.Tan(depression)
	if dist > maxGeoDistanceM {
		return nil
	}
	bearing := toRad(*p.HeadingDeg + camYaw + toDeg(alpha))
	north, east := dist*math.Cos(bearing), dist*math.Sin(bearing)
	const earthR = 6378137.0
	lat := p.Lat + toDeg(north/earthR)
	lon := p.Lon + toDeg(east/(earthR*math.Cos(toRad(p.Lat))))
	return &GeoEstimate{Lat: round7(lat), Lon: round7(lon), DistanceM: math.Round(dist), Approx: true}
}

func toRad(d float64) float64 { return d * math.Pi / 180 }
func toDeg(r float64) float64 { return r * 180 / math.Pi }

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

func wrap180(d float64) float64 {
	d = math.Mod(d+180, 360)
	if d < 0 {
		d += 360
	}
	return d - 180
}

func round7(v float64) float64 { return math.Round(v*1e7) / 1e7 }
