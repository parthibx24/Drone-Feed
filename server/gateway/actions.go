package gateway

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/bluenviron/gomavlib/v3/pkg/dialects/common"

	"github.com/parthibx24/Drone-Feed/internal/mav"
)

// ErrPrecondition means the vehicle state does not allow the action.
var ErrPrecondition = errors.New("precondition failed")

func precondition(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrPrecondition, fmt.Sprintf(format, a...))
}

const forceArmMagic = 21196 // MAV_CMD_COMPONENT_ARM_DISARM param2 to bypass checks

// armedWait covers the gap between an accepted ARM and the next heartbeat
// reporting it (ArduPilot sends heartbeats at 1 Hz).
const armedWait = 2 * time.Second

// waitArmed reports whether the vehicle is (or within armedWait becomes) armed.
func (d *Drone) waitArmed(ctx context.Context) bool {
	deadline := time.Now().Add(armedWait)
	for {
		if d.Snapshot().Armed {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// ---------------------------------------------------------------- flight

func (d *Drone) Arm(ctx context.Context, force bool) error {
	var p2 float32
	if force {
		p2 = forceArmMagic
	}
	_, err := d.CommandLong(ctx, mav.CompAutopilot, common.MAV_CMD_COMPONENT_ARM_DISARM, [7]float32{1, p2})
	return err
}

func (d *Drone) Disarm(ctx context.Context, force bool) error {
	var p2 float32
	if force {
		p2 = forceArmMagic
	}
	_, err := d.CommandLong(ctx, mav.CompAutopilot, common.MAV_CMD_COMPONENT_ARM_DISARM, [7]float32{0, p2})
	return err
}

func (d *Drone) SetMode(ctx context.Context, mode string) error {
	n, ok := mav.CopterModeNumber(mode)
	if !ok {
		return precondition("unknown flight mode %q", mode)
	}
	_, err := d.CommandLong(ctx, mav.CompAutopilot, common.MAV_CMD_DO_SET_MODE,
		[7]float32{float32(common.MAV_MODE_FLAG_CUSTOM_MODE_ENABLED), float32(n)})
	return err
}

// Takeoff switches to GUIDED (if needed) and climbs to altM. The vehicle must
// already be armed: arming is always a separate, explicit operator action.
func (d *Drone) Takeoff(ctx context.Context, altM float64) error {
	if altM <= 0 || altM > d.gw.Limits.MaxAltM {
		return precondition("alt_m must be > 0 and <= %.0f", d.gw.Limits.MaxAltM)
	}
	if !d.waitArmed(ctx) {
		return precondition("vehicle must be armed before takeoff")
	}
	if d.Snapshot().Mode != "GUIDED" {
		if err := d.SetMode(ctx, "GUIDED"); err != nil {
			return err
		}
	}
	_, err := d.CommandLong(ctx, mav.CompAutopilot, common.MAV_CMD_NAV_TAKEOFF,
		[7]float32{0, 0, 0, mav.NaN, 0, 0, float32(altM)})
	return err
}

func (d *Drone) Land(ctx context.Context) error { return d.SetMode(ctx, "LAND") }
func (d *Drone) RTL(ctx context.Context) error  { return d.SetMode(ctx, "RTL") }

// Goto flies to a point (switching to GUIDED) using MAV_CMD_DO_REPOSITION.
func (d *Drone) Goto(ctx context.Context, lat, lon, altM float64, speed *float64) error {
	if lat < -90 || lat > 90 || lon < -180 || lon > 180 || (lat == 0 && lon == 0) {
		return precondition("invalid lat/lon")
	}
	if altM <= 0 || altM > d.gw.Limits.MaxAltM {
		return precondition("alt_m must be > 0 and <= %.0f", d.gw.Limits.MaxAltM)
	}
	sp := float32(-1)
	if speed != nil {
		if *speed <= 0 || *speed > d.gw.Limits.MaxSpeedMS {
			return precondition("speed_m_s must be > 0 and <= %.0f", d.gw.Limits.MaxSpeedMS)
		}
		sp = float32(*speed)
	}
	if !d.waitArmed(ctx) {
		return precondition("vehicle must be armed and flying")
	}
	_, err := d.CommandInt(ctx, mav.CompAutopilot, common.MAV_CMD_DO_REPOSITION, common.MAV_FRAME_GLOBAL_RELATIVE_ALT_INT,
		[4]float32{sp, float32(common.MAV_DO_REPOSITION_FLAGS_CHANGE_MODE), 0, mav.NaN},
		int32(math.Round(lat*1e7)), int32(math.Round(lon*1e7)), float32(altM))
	return err
}

func (d *Drone) ChangeSpeed(ctx context.Context, ms float64) error {
	if ms <= 0 || ms > d.gw.Limits.MaxSpeedMS {
		return precondition("speed_m_s must be > 0 and <= %.0f", d.gw.Limits.MaxSpeedMS)
	}
	_, err := d.CommandLong(ctx, mav.CompAutopilot, common.MAV_CMD_DO_CHANGE_SPEED, [7]float32{1, float32(ms), -1})
	return err
}

// Pause holds position in AUTO/GUIDED; Resume continues.
func (d *Drone) Pause(ctx context.Context) error {
	_, err := d.CommandLong(ctx, mav.CompAutopilot, common.MAV_CMD_DO_PAUSE_CONTINUE, [7]float32{0})
	return err
}

func (d *Drone) Resume(ctx context.Context) error {
	_, err := d.CommandLong(ctx, mav.CompAutopilot, common.MAV_CMD_DO_PAUSE_CONTINUE, [7]float32{1})
	return err
}

// MissionStart switches to AUTO and starts the uploaded mission.
func (d *Drone) MissionStart(ctx context.Context) error {
	if !d.waitArmed(ctx) {
		return precondition("vehicle must be armed to start a mission")
	}
	_, err := d.CommandLong(ctx, mav.CompAutopilot, common.MAV_CMD_MISSION_START, [7]float32{})
	return err
}

func (d *Drone) SetMissionCurrent(ctx context.Context, seq int) error {
	if seq < 1 {
		return precondition("seq must be >= 1 (item 0 is home)")
	}
	_, err := d.CommandLong(ctx, mav.CompAutopilot, common.MAV_CMD_DO_SET_MISSION_CURRENT, [7]float32{float32(seq)})
	return err
}

// SetROI points the vehicle (and gimbal, if ArduPilot controls one) at a location.
func (d *Drone) SetROI(ctx context.Context, lat, lon, altM float64) error {
	_, err := d.CommandInt(ctx, mav.CompAutopilot, common.MAV_CMD_DO_SET_ROI_LOCATION, common.MAV_FRAME_GLOBAL_RELATIVE_ALT_INT,
		[4]float32{}, int32(math.Round(lat*1e7)), int32(math.Round(lon*1e7)), float32(altM))
	return err
}

func (d *Drone) ClearROI(ctx context.Context) error {
	_, err := d.CommandLong(ctx, mav.CompAutopilot, common.MAV_CMD_DO_SET_ROI_NONE, [7]float32{})
	return err
}

// RebootAutopilot reboots the flight controller. Refused while armed.
func (d *Drone) RebootAutopilot(ctx context.Context) error {
	if d.Snapshot().Armed {
		return precondition("refusing to reboot an armed vehicle")
	}
	_, err := d.CommandLong(ctx, mav.CompAutopilot, common.MAV_CMD_PREFLIGHT_REBOOT_SHUTDOWN, [7]float32{1})
	return err
}

// ---------------------------------------------------------------- gimbal

func (d *Drone) gimbalFlags() common.GIMBAL_MANAGER_FLAGS {
	st := d.Snapshot()
	if st.Gimbal != nil && st.Gimbal.YawLock {
		return common.GIMBAL_MANAGER_FLAGS_YAW_LOCK
	}
	return 0
}

// GimbalAttitude points the gimbal at absolute angles (deg; pitch negative = down).
// Pass NaN to leave an axis unchanged.
func (d *Drone) GimbalAttitude(ctx context.Context, pitch, yaw float32) error {
	_, err := d.CommandLong(ctx, mav.CompGimbal, common.MAV_CMD_DO_GIMBAL_MANAGER_PITCHYAW,
		[7]float32{pitch, yaw, mav.NaN, mav.NaN, float32(d.gimbalFlags()), 0, float32(mav.CompGimbal)})
	return err
}

func (d *Drone) GimbalCenter(ctx context.Context) error {
	_, err := d.CommandLong(ctx, mav.CompGimbal, common.MAV_CMD_DO_GIMBAL_MANAGER_PITCHYAW,
		[7]float32{mav.NaN, mav.NaN, mav.NaN, mav.NaN, float32(common.GIMBAL_MANAGER_FLAGS_NEUTRAL), 0, float32(mav.CompGimbal)})
	return err
}

// GimbalMode selects yaw "follow" (gimbal turns with the drone) or "lock"
// (gimbal holds its heading).
func (d *Drone) GimbalMode(ctx context.Context, mode string) error {
	var flags common.GIMBAL_MANAGER_FLAGS
	switch mode {
	case "follow":
	case "lock":
		flags = common.GIMBAL_MANAGER_FLAGS_YAW_LOCK
	default:
		return precondition("gimbal mode must be follow or lock")
	}
	_, err := d.CommandLong(ctx, mav.CompGimbal, common.MAV_CMD_DO_GIMBAL_MANAGER_PITCHYAW,
		[7]float32{mav.NaN, mav.NaN, mav.NaN, mav.NaN, float32(flags), 0, float32(mav.CompGimbal)})
	return err
}

// GimbalRate streams one rate update (deg/s). It is not acknowledged; the
// bridge stops the gimbal if updates stop arriving (dead-man), so callers
// must repeat it while the operator holds the control (>= 4 Hz).
func (d *Drone) GimbalRate(pitchRate, yawRate float64) error {
	sys, err := d.targetSystem()
	if err != nil {
		return err
	}
	lim := 30.0
	pitchRate = math.Max(-lim, math.Min(lim, pitchRate))
	yawRate = math.Max(-lim, math.Min(lim, yawRate))
	return d.write(&common.MessageGimbalManagerSetPitchyaw{
		TargetSystem: sys, TargetComponent: mav.CompGimbal, Flags: d.gimbalFlags(),
		GimbalDeviceId: mav.CompGimbal, Pitch: mav.NaN, Yaw: mav.NaN,
		PitchRate: float32(pitchRate), YawRate: float32(yawRate),
	})
}

// ---------------------------------------------------------------- camera

func (d *Drone) Photo(ctx context.Context) error {
	_, err := d.CommandLong(ctx, mav.CompCamera, common.MAV_CMD_IMAGE_START_CAPTURE, [7]float32{0, 0, 1, 0})
	return err
}

func (d *Drone) PhotoInterval(ctx context.Context, seconds float64, count int) error {
	if seconds <= 0 {
		return precondition("seconds must be > 0")
	}
	_, err := d.CommandLong(ctx, mav.CompCamera, common.MAV_CMD_IMAGE_START_CAPTURE, [7]float32{0, float32(seconds), float32(count), 0})
	return err
}

func (d *Drone) PhotoStop(ctx context.Context) error {
	_, err := d.CommandLong(ctx, mav.CompCamera, common.MAV_CMD_IMAGE_STOP_CAPTURE, [7]float32{})
	return err
}

func (d *Drone) Video(ctx context.Context, start bool) error {
	cmd := common.MAV_CMD_VIDEO_STOP_CAPTURE
	if start {
		cmd = common.MAV_CMD_VIDEO_START_CAPTURE
	}
	_, err := d.CommandLong(ctx, mav.CompCamera, cmd, [7]float32{})
	return err
}

// Zoom: kind is "step" (value = +n/-n steps), "continuous" (-1, 0, 1) or
// "range" (0..100 %).
func (d *Drone) Zoom(ctx context.Context, kind string, value float64) error {
	var t common.CAMERA_ZOOM_TYPE
	switch kind {
	case "step":
		t = common.ZOOM_TYPE_STEP
	case "continuous":
		t = common.ZOOM_TYPE_CONTINUOUS
		if value != -1 && value != 0 && value != 1 {
			return precondition("continuous zoom value must be -1, 0 or 1")
		}
	case "range":
		t = common.ZOOM_TYPE_RANGE
		if value < 0 || value > 100 {
			return precondition("range zoom value must be 0-100")
		}
	default:
		return precondition("zoom type must be step, continuous or range")
	}
	_, err := d.CommandLong(ctx, mav.CompCamera, common.MAV_CMD_SET_CAMERA_ZOOM, [7]float32{float32(t), float32(value)})
	return err
}
