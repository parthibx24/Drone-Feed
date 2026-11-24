package gateway

// What each drone can do, so one dashboard serves different kinds of
// aircraft: StarWatch's ArduPilot drones, DJI aircraft through dji-bridge,
// and other MAVLink autopilots. The dashboard shows a control only when its
// feature is listed; the gateway refuses commands for features the drone's
// profile cannot carry out.
//
// A drone's profile comes from drones[].autopilot, or, when that is unset,
// from drones[].bridge (dji) or the autopilot heartbeat (ardupilot until a
// heartbeat says otherwise). drones[].features adds or removes features on
// top of the profile.

import (
	"fmt"
	"net/http"
	"slices"
	"sort"
	"time"

	"github.com/bluenviron/gomavlib/v3/pkg/dialects/common"

	"github.com/parthibx24/Drone-Feed/internal/mav"
)

// Profiles (drones[].autopilot).
const (
	ProfileArduPilot = "ardupilot" // ArduCopter: every MAVLink route
	ProfileDJI       = "dji"       // a DJI aircraft through dji-bridge
	ProfileMAVLink   = "mavlink"   // another MAVLink autopilot (PX4, …): standard protocols only
)

// Feature names, shared with the dashboard (frontend/src/types/gateway.ts).
const (
	FeatArm          = "arm"
	FeatSetMode      = "set_mode"
	FeatTakeoff      = "takeoff"
	FeatLand         = "land"
	FeatRTL          = "rtl"
	FeatPause        = "pause"
	FeatResume       = "resume"
	FeatGoto         = "goto"
	FeatSpeed        = "speed"
	FeatROI          = "roi"
	FeatReboot       = "reboot"
	FeatMission      = "mission"       // read, write and clear the mission
	FeatMissionStart = "mission_start" // start it from the dashboard
	FeatMissionJump  = "mission_jump"  // jump to an item
	FeatParams       = "params"
	FeatGimbal       = "gimbal"
	FeatZoom         = "zoom"
	FeatPhoto        = "photo"
	FeatCameraVideo  = "camera_video" // recording on the aircraft's own card
	FeatCameraSource = "camera_source"
	FeatRecording    = "recording" // server-side recording of the video
	FeatSpeaker      = "speaker"
	FeatAircraft     = "aircraft" // the aircraft API: thermal, obstacle avoidance, RTK (GET /features)
	FeatAI           = "ai"
	FeatRawCommand   = "raw_command"
)

// knownFeatures validates drones[].features.
var knownFeatures = map[string]bool{
	FeatArm: true, FeatSetMode: true, FeatTakeoff: true, FeatLand: true, FeatRTL: true, FeatPause: true,
	FeatResume: true, FeatGoto: true, FeatSpeed: true, FeatROI: true, FeatReboot: true, FeatMission: true,
	FeatMissionStart: true, FeatMissionJump: true, FeatParams: true, FeatGimbal: true, FeatZoom: true,
	FeatPhoto: true, FeatCameraVideo: true, FeatCameraSource: true, FeatRecording: true, FeatSpeaker: true,
	FeatAircraft: true, FeatAI: true, FeatRawCommand: true,
}

// presenceFeatures are shown only once the component that does them has
// been seen (an ArduPilot drone may fly without a gimbal or camera; DJI
// links report them in their command kinds instead). The gateway does not
// refuse them: the component answers for itself.
var presenceFeatures = map[string]func(capInput) bool{
	FeatGimbal:      func(in capInput) bool { return in.gimbalSeen },
	FeatZoom:        func(in capInput) bool { return in.cameraSeen },
	FeatPhoto:       func(in capInput) bool { return in.cameraSeen },
	FeatCameraVideo: func(in capInput) bool { return in.cameraSeen },
}

// missionItemTypes are the gateway's mission item types (mission.go).
var missionItemTypes = []string{
	"takeoff", "waypoint", "loiter_time", "land", "rtl", "speed", "delay", "photo", "photo_interval",
	"photo_stop", "video_start", "video_stop", "roi", "roi_none", "raw",
}

// djiKinds maps dji-bridge command kinds (GET {bridge.api} → capabilities)
// to the features they make possible.
var djiKinds = map[string][]string{
	"takeoff":         {FeatTakeoff, FeatArm}, // the bridge arms only links that can take off
	"land":            {FeatLand},
	"return_home":     {FeatRTL},
	"pause":           {FeatPause},
	"resume_mission":  {FeatResume},
	"goto":            {FeatGoto},
	"upload_mission":  {FeatMission},
	"start_mission":   {FeatMissionStart},
	"look_at":         {FeatROI},
	"gimbal_rate":     {FeatGimbal},
	"gimbal_angle":    {FeatGimbal},
	"set_zoom":        {FeatZoom},
	"take_photo":      {FeatPhoto},
	"start_recording": {FeatCameraVideo},
}

// djiAll is every feature a DJI link can offer: assumed while the bridge
// cannot be asked, so the pilot's safety commands are never hidden.
var djiAll = func() []string {
	var out []string
	for _, fs := range djiKinds {
		out = append(out, fs...)
	}
	return out
}()

// Capabilities is GET /api/v1/drones/{id}/capabilities (also in the drone
// list and pushed over the WebSocket when it changes).
type Capabilities struct {
	// Autopilot is the profile: ardupilot, dji or mavlink.
	Autopilot string `json:"autopilot"`
	// Model is the aircraft model when its link reports one (DJI: "M3T").
	Model string `json:"model,omitempty"`
	// Features the dashboard may offer, sorted.
	Features []string `json:"features"`
	// Modes an operator may select (set_mode), in the autopilot's names.
	Modes []string `json:"modes"`
	// MissionItems are the item types the drone accepts, and
	// MissionAltFrames what their altitudes may be measured from.
	MissionItems     []string `json:"mission_items"`
	MissionAltFrames []string `json:"mission_alt_frames"`
	// Note says why the set is smaller than usual, for the dashboard to show.
	Note string `json:"note,omitempty"`
}

// Has reports whether f is listed.
func (c Capabilities) Has(f string) bool { return slices.Contains(c.Features, f) }

// capInput is everything the capabilities depend on.
type capInput struct {
	profile                         string
	cameraSeen, gimbalSeen, lensNow bool
	speaker, aircraft, ai, raw      bool
	bridge                          *bridgeStatus // dji: nil while the bridge has not answered lately
	enable, disable                 []string
}

// bridgeStatus is the aircraft as dji-bridge last reported it.
type bridgeStatus struct {
	at     time.Time
	model  string
	online bool
	kinds  []string
}

// profileFeatures is what the profile allows, before presence and the
// drone's own additions and removals.
func profileFeatures(in capInput) (features []string, note string) {
	switch in.profile {
	case ProfileDJI:
		kinds := djiAll
		if in.bridge == nil {
			note = "The aircraft service is not answering: commands the aircraft's link cannot carry out are refused by it."
		} else {
			kinds = nil
			for _, k := range in.bridge.kinds {
				kinds = append(kinds, djiKinds[k]...)
			}
			if !slices.Contains(in.bridge.kinds, "takeoff") && !slices.Contains(in.bridge.kinds, "land") {
				note = "The aircraft's link offers no flight commands (DJI Pilot 2). Open StarWatch Pilot on the remote controller to fly from here."
			}
		}
		return kinds, note
	case ProfileMAVLink:
		// Standard MAVLink only: ArduCopter mode numbers would mean
		// something else to another autopilot, so no modes, take-off,
		// land, RTL or go-to unless drones[].features enables them.
		return []string{FeatArm, FeatPause, FeatResume, FeatMission, FeatMissionStart, FeatMissionJump,
			FeatParams, FeatGimbal, FeatZoom, FeatPhoto, FeatCameraVideo}, ""
	default: // ardupilot
		return []string{FeatArm, FeatSetMode, FeatTakeoff, FeatLand, FeatRTL, FeatPause, FeatResume, FeatGoto,
			FeatSpeed, FeatROI, FeatReboot, FeatMission, FeatMissionStart, FeatMissionJump, FeatParams,
			FeatGimbal, FeatZoom, FeatPhoto, FeatCameraVideo}, ""
	}
}

// allowed is the set the gateway enforces: the profile plus the drone's
// additions, minus its removals, ignoring which components have been seen.
func allowed(in capInput) (map[string]bool, string) {
	fs, note := profileFeatures(in)
	set := map[string]bool{FeatRecording: true}
	for _, f := range fs {
		set[f] = true
	}
	if in.lensNow {
		set[FeatCameraSource] = true
	}
	if in.speaker {
		set[FeatSpeaker] = true
	}
	if in.aircraft {
		set[FeatAircraft] = true
	}
	if in.ai {
		set[FeatAI] = true
	}
	if in.raw && in.profile != ProfileDJI {
		set[FeatRawCommand] = true
	}
	for _, f := range in.enable {
		set[f] = true
	}
	for _, f := range in.disable {
		delete(set, f)
	}
	return set, note
}

// deriveCapabilities is what the dashboard is told.
func deriveCapabilities(in capInput) Capabilities {
	set, note := allowed(in)
	c := Capabilities{Autopilot: in.profile, Note: note, Features: []string{}, Modes: []string{}}
	enabled := func(f string) bool { return slices.Contains(in.enable, f) }
	for f := range set {
		// DJI: the bridge's list already says what the camera and gimbal do.
		if seen, ok := presenceFeatures[f]; ok && in.profile != ProfileDJI && !seen(in) && !enabled(f) {
			continue
		}
		c.Features = append(c.Features, f)
	}
	sort.Strings(c.Features)
	if in.bridge != nil {
		c.Model = in.bridge.model
	}
	if set[FeatSetMode] && in.profile == ProfileArduPilot {
		c.Modes = mav.CopterModeNames()
	}
	c.MissionItems, c.MissionAltFrames = []string{}, []string{}
	if set[FeatMission] {
		switch in.profile {
		case ProfileDJI:
			// dji-bridge turns the mission into a DJI wayline: heights above
			// take-off only, and no raw MAVLink items.
			c.MissionItems = slices.DeleteFunc(slices.Clone(missionItemTypes), func(t string) bool { return t == "raw" })
			c.MissionAltFrames = []string{"relative"}
		default:
			c.MissionItems = slices.Clone(missionItemTypes)
			c.MissionAltFrames = []string{"relative", "terrain", "absolute"}
		}
	}
	return c
}

// profile resolves the drone's profile.
func (d *Drone) profile() string {
	switch {
	case d.cfg.Autopilot != "":
		return d.cfg.Autopilot
	case d.cfg.Bridge != nil:
		return ProfileDJI
	}
	d.mu.RLock()
	ap := d.autopilot
	d.mu.RUnlock()
	switch ap {
	case common.MAV_AUTOPILOT_ARDUPILOTMEGA, 0:
		// 0: not identified yet. Assume ArduPilot, as before profiles existed.
		return ProfileArduPilot
	}
	return ProfileMAVLink
}

func (g *Gateway) capInput(d *Drone) capInput {
	snap := d.Snapshot()
	d.mu.RLock()
	in := capInput{
		cameraSeen: !d.camSeen.IsZero(),
		gimbalSeen: !d.gimbalSeen.IsZero(),
	}
	d.mu.RUnlock()
	in.profile = d.profile()
	in.lensNow = snap.Camera != nil && snap.Camera.Source != nil
	in.speaker = d.cfg.Speaker != nil
	in.aircraft = d.cfg.Bridge != nil
	in.ai = g.ai != nil
	in.raw = g.cfg.AllowRawCommands
	in.enable, in.disable = d.cfg.Features.Enable, d.cfg.Features.Disable
	if in.profile == ProfileDJI {
		in.bridge = d.bridgeNow()
	}
	return in
}

// Capabilities is what the dashboard may offer for d.
func (g *Gateway) Capabilities(d *Drone) Capabilities { return deriveCapabilities(g.capInput(d)) }

// supports reports whether the gateway lets f through to d. Only what the
// profile rules out is refused: components that may be missing answer for
// themselves.
func (g *Gateway) supports(d *Drone, f string) bool {
	set, _ := allowed(g.capInput(d))
	return set[f]
}

// routeFeatures are the routes the gateway refuses when the drone's profile
// rules their feature out (route checks r.Pattern against this). Viewers'
// reads are refused too: a parameter download from a drone without
// parameters only waits for a timeout.
var routeFeatures = map[string]string{
	"POST /api/v1/drones/{id}/arm":                   FeatArm,
	"POST /api/v1/drones/{id}/disarm":                FeatArm,
	"POST /api/v1/drones/{id}/mode":                  FeatSetMode,
	"POST /api/v1/drones/{id}/takeoff":               FeatTakeoff,
	"POST /api/v1/drones/{id}/land":                  FeatLand,
	"POST /api/v1/drones/{id}/rtl":                   FeatRTL,
	"POST /api/v1/drones/{id}/pause":                 FeatPause,
	"POST /api/v1/drones/{id}/resume":                FeatResume,
	"POST /api/v1/drones/{id}/reboot":                FeatReboot,
	"POST /api/v1/drones/{id}/goto":                  FeatGoto,
	"POST /api/v1/drones/{id}/speed":                 FeatSpeed,
	"POST /api/v1/drones/{id}/roi":                   FeatROI,
	"DELETE /api/v1/drones/{id}/roi":                 FeatROI,
	"GET /api/v1/drones/{id}/mission":                FeatMission,
	"PUT /api/v1/drones/{id}/mission":                FeatMission,
	"DELETE /api/v1/drones/{id}/mission":             FeatMission,
	"POST /api/v1/drones/{id}/mission/start":         FeatMissionStart,
	"POST /api/v1/drones/{id}/mission/current":       FeatMissionJump,
	"GET /api/v1/drones/{id}/params":                 FeatParams,
	"GET /api/v1/drones/{id}/params/{name}":          FeatParams,
	"PUT /api/v1/drones/{id}/params/{name}":          FeatParams,
	"POST /api/v1/drones/{id}/gimbal/attitude":       FeatGimbal,
	"POST /api/v1/drones/{id}/gimbal/rate":           FeatGimbal,
	"POST /api/v1/drones/{id}/gimbal/center":         FeatGimbal,
	"POST /api/v1/drones/{id}/gimbal/mode":           FeatGimbal,
	"POST /api/v1/drones/{id}/camera/photo":          FeatPhoto,
	"POST /api/v1/drones/{id}/camera/photo/interval": FeatPhoto,
	"POST /api/v1/drones/{id}/camera/photo/stop":     FeatPhoto,
	"POST /api/v1/drones/{id}/camera/video/start":    FeatCameraVideo,
	"POST /api/v1/drones/{id}/camera/video/stop":     FeatCameraVideo,
	"POST /api/v1/drones/{id}/camera/zoom":           FeatZoom,
}

// refuseUnsupported answers 409 and returns true when r's route needs a
// feature d's profile rules out.
func (g *Gateway) refuseUnsupported(w http.ResponseWriter, r *http.Request, d *Drone) bool {
	f, ok := routeFeatures[r.Pattern]
	if !ok || g.supports(d, f) {
		return false
	}
	writeErr(w, http.StatusConflict, fmt.Sprintf("%s does not offer this (%s)", d.cfg.Name, f))
	return true
}
