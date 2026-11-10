// Package bridge exposes the Skydroid C12 as two standard MAVLink components:
//
//   - a camera  (component 100): MAVLink Camera Protocol
//   - a gimbal  (component 154): MAVLink Gimbal Protocol v2 (acting as gimbal manager)
//
// Commands arrive from mavlink-router, are translated to Skydroid UDP packets,
// and are acknowledged with COMMAND_ACK. Any MAVLink client (drone-gateway,
// QGroundControl, Mission Planner, the autopilot itself) can drive the camera.
package bridge

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/bluenviron/gomavlib/v3"
	"github.com/bluenviron/gomavlib/v3/pkg/dialects/ardupilotmega"
	"github.com/bluenviron/gomavlib/v3/pkg/dialects/common"
	"github.com/bluenviron/gomavlib/v3/pkg/message"

	"github.com/parthibx24/Drone-Feed/drone/skydroid"
	"github.com/parthibx24/Drone-Feed/internal/mav"
)

// Stream describes one RTSP stream advertised through VIDEO_STREAM_INFORMATION.
type Stream struct {
	Name    string
	URI     string
	Thermal bool
}

// Config for the bridge.
type Config struct {
	// Endpoint is the MAVLink endpoint: a gomavlib EndpointConf (UDP server/client).
	Endpoint gomavlib.EndpointConf
	// SystemID of the vehicle. 0 = adopt the autopilot's ID from its first heartbeat.
	SystemID uint8
	// Gimbal is the Skydroid UDP client.
	Gimbal *skydroid.Client
	// MotionRateHz is how often speed packets are repeated while moving.
	MotionRateHz float64
	// RateTimeout stops rate motion when no new rate command arrives in time
	// (dead-man). Clients streaming rates should send at least every RateTimeout/2.
	RateTimeout time.Duration
	// MaxRate (deg/s) scales normalized manual-control input (-1..1).
	MaxRate float64
	// Streams advertised to ground stations.
	Streams []Stream
	Log     *slog.Logger
}

// Bridge is the running MAVLink camera + gimbal component.
type Bridge struct {
	cfg    Config
	node   *gomavlib.Node
	sender *mav.Sender
	gimbal *skydroid.Client
	log    *slog.Logger
	start  time.Time

	mu       sync.Mutex
	sysID    uint8
	yawLock  bool
	ctrlSys  uint8 // last controlling system/component (for GIMBAL_MANAGER_STATUS)
	ctrlComp uint8

	// motionMu also serialises every speed/stop packet, so a stop can never be
	// overtaken by a motion packet that was decided before it.
	motionMu    sync.Mutex
	yawRate     float64
	pitchRate   float64
	motionUntil time.Time
	moving      bool
	stopsLeft   int // extra stop packets still to send (a lone UDP stop can be lost)

	camMu        sync.Mutex
	recording    bool
	recordStart  time.Time
	imageCount   int32
	zoomPct      float32
	intervalStop chan struct{}
	zoomStop     chan struct{}
}

// New validates cfg and opens the MAVLink endpoint.
func New(cfg Config) (*Bridge, error) {
	if cfg.Gimbal == nil {
		return nil, fmt.Errorf("gimbal client required")
	}
	if cfg.MotionRateHz <= 0 {
		cfg.MotionRateHz = 10
	}
	if cfg.RateTimeout <= 0 {
		cfg.RateTimeout = 600 * time.Millisecond
	}
	if cfg.MaxRate <= 0 {
		cfg.MaxRate = 30
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	node := &gomavlib.Node{
		Endpoints:        []gomavlib.EndpointConf{cfg.Endpoint},
		Dialect:          ardupilotmega.Dialect,
		OutVersion:       gomavlib.V2,
		OutSystemID:      1, // unused: frames are written through mav.Sender
		HeartbeatDisable: true,
	}
	if err := node.Initialize(); err != nil {
		return nil, fmt.Errorf("mavlink endpoint: %w", err)
	}
	return &Bridge{
		cfg: cfg, node: node, sender: &mav.Sender{Node: node}, gimbal: cfg.Gimbal,
		log: cfg.Log, start: time.Now(), sysID: cfg.SystemID, zoomPct: float32(math.NaN()),
	}, nil
}

// Run processes MAVLink until ctx is cancelled.
func (b *Bridge) Run(ctx context.Context) error {
	defer b.node.Close()
	b.stopGimbal()

	go b.motionLoop(ctx)
	go b.heartbeatLoop(ctx)

	events := b.node.Events()
	for {
		select {
		case <-ctx.Done():
			b.stopGimbal()
			return nil
		case evt, ok := <-events:
			if !ok {
				b.stopGimbal()
				return fmt.Errorf("mavlink node closed")
			}
			if fr, ok := evt.(*gomavlib.EventFrame); ok {
				b.handle(fr)
			}
		}
	}
}

func (b *Bridge) system() uint8 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sysID
}

func (b *Bridge) send(comp uint8, msg message.Message) {
	sys := b.system()
	if sys == 0 {
		return // vehicle not identified yet
	}
	if err := b.sender.Send(sys, comp, msg); err != nil {
		b.log.Warn("mavlink send failed", "msg", fmt.Sprintf("%T", msg), "err", err)
	}
}

func (b *Bridge) heartbeatLoop(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b.send(mav.CompCamera, &common.MessageHeartbeat{
				Type: common.MAV_TYPE_CAMERA, Autopilot: common.MAV_AUTOPILOT_INVALID,
				SystemStatus: common.MAV_STATE_ACTIVE, MavlinkVersion: 3,
			})
			b.send(mav.CompGimbal, &common.MessageHeartbeat{
				Type: common.MAV_TYPE_GIMBAL, Autopilot: common.MAV_AUTOPILOT_INVALID,
				SystemStatus: common.MAV_STATE_ACTIVE, MavlinkVersion: 3,
			})
			b.send(mav.CompGimbal, b.gimbalManagerStatus())
		}
	}
}

func (b *Bridge) handle(fr *gomavlib.EventFrame) {
	switch m := fr.Message().(type) {
	case *common.MessageHeartbeat:
		if fr.ComponentID() == mav.CompAutopilot && mav.IsAutopilotHeartbeat(m) {
			b.mu.Lock()
			if b.sysID == 0 {
				b.sysID = fr.SystemID()
				b.log.Info("vehicle identified", "sysid", b.sysID)
			}
			b.mu.Unlock()
		}

	case *common.MessageCommandLong:
		if m.TargetSystem != b.system() {
			return
		}
		p := [7]float32{m.Param1, m.Param2, m.Param3, m.Param4, m.Param5, m.Param6, m.Param7}
		b.dispatch(fr, m.TargetComponent, m.Command, p)

	case *common.MessageCommandInt:
		if m.TargetSystem != b.system() {
			return
		}
		p := [7]float32{m.Param1, m.Param2, m.Param3, m.Param4, float32(m.X), float32(m.Y), m.Z}
		b.dispatch(fr, m.TargetComponent, m.Command, p)

	case *common.MessageGimbalManagerSetPitchyaw:
		if m.TargetSystem != b.system() || m.TargetComponent != mav.CompGimbal {
			return
		}
		b.setController(fr)
		b.applyPitchYaw(m.Pitch, m.Yaw, m.PitchRate, m.YawRate, m.Flags, true)

	case *common.MessageGimbalManagerSetManualControl:
		if m.TargetSystem != b.system() || m.TargetComponent != mav.CompGimbal {
			return
		}
		b.setController(fr)
		pr, yr := float32(0), float32(0)
		if !mav.IsNaN(m.PitchRate) {
			pr = m.PitchRate * float32(b.cfg.MaxRate)
		}
		if !mav.IsNaN(m.YawRate) {
			yr = m.YawRate * float32(b.cfg.MaxRate)
		}
		b.applyPitchYaw(mav.NaN, mav.NaN, pr, yr, m.Flags, true)
	}
}

func (b *Bridge) setController(fr *gomavlib.EventFrame) {
	b.mu.Lock()
	b.ctrlSys, b.ctrlComp = fr.SystemID(), fr.ComponentID()
	b.mu.Unlock()
}

func (b *Bridge) ack(fr *gomavlib.EventFrame, comp uint8, cmd common.MAV_CMD, res common.MAV_RESULT) {
	b.send(comp, &common.MessageCommandAck{
		Command: cmd, Result: res,
		TargetSystem: fr.SystemID(), TargetComponent: fr.ComponentID(),
	})
	if res != common.MAV_RESULT_ACCEPTED {
		b.log.Info("command not accepted", "cmd", cmd.String(), "result", res.String(), "comp", comp)
	}
}

func (b *Bridge) dispatch(fr *gomavlib.EventFrame, comp uint8, cmd common.MAV_CMD, p [7]float32) {
	switch comp {
	case mav.CompCamera:
		b.ack(fr, comp, cmd, b.cameraCommand(cmd, p))
	case mav.CompGimbal:
		b.setController(fr)
		b.ack(fr, comp, cmd, b.gimbalCommand(cmd, p))
	}
}

func (b *Bridge) logSendErr(what string, err error) common.MAV_RESULT {
	if err != nil {
		b.log.Warn("gimbal UDP send failed", "what", what, "err", err)
		return common.MAV_RESULT_FAILED
	}
	return common.MAV_RESULT_ACCEPTED
}

// ---------------------------------------------------------------- gimbal

func (b *Bridge) gimbalCommand(cmd common.MAV_CMD, p [7]float32) common.MAV_RESULT {
	switch cmd {
	case common.MAV_CMD_REQUEST_MESSAGE:
		switch uint32(p[0]) {
		case 280: // GIMBAL_MANAGER_INFORMATION
			b.send(mav.CompGimbal, b.gimbalManagerInformation())
		case 281: // GIMBAL_MANAGER_STATUS
			b.send(mav.CompGimbal, b.gimbalManagerStatus())
		default:
			return common.MAV_RESULT_UNSUPPORTED
		}
		return common.MAV_RESULT_ACCEPTED

	case common.MAV_CMD_DO_GIMBAL_MANAGER_PITCHYAW:
		if dev := uint8(p[6]); dev != 0 && dev != mav.CompGimbal {
			return common.MAV_RESULT_DENIED
		}
		return b.applyPitchYaw(p[0], p[1], p[2], p[3], common.GIMBAL_MANAGER_FLAGS(uint32(p[4])), false)

	case common.MAV_CMD_DO_GIMBAL_MANAGER_CONFIGURE:
		return common.MAV_RESULT_ACCEPTED // single-controller device: accept any controller

	case common.MAV_CMD_DO_MOUNT_CONTROL: // legacy: p1 pitch, p3 yaw, p7 mount mode
		switch int(p[6]) {
		case int(common.MAV_MOUNT_MODE_NEUTRAL):
			return b.applyPitchYaw(mav.NaN, mav.NaN, mav.NaN, mav.NaN, common.GIMBAL_MANAGER_FLAGS_NEUTRAL, false)
		case int(common.MAV_MOUNT_MODE_MAVLINK_TARGETING), 0:
			return b.applyPitchYaw(p[0], p[2], mav.NaN, mav.NaN, b.lockFlags(), false)
		}
		return common.MAV_RESULT_UNSUPPORTED
	}
	return common.MAV_RESULT_UNSUPPORTED
}

func (b *Bridge) lockFlags() common.GIMBAL_MANAGER_FLAGS {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.yawLock {
		return common.GIMBAL_MANAGER_FLAGS_YAW_LOCK
	}
	return 0
}

// applyPitchYaw implements GIMBAL_MANAGER_SET_PITCHYAW / DO_GIMBAL_MANAGER_PITCHYAW
// semantics on the C12: angles in degrees (NaN = ignore), rates in deg/s.
func (b *Bridge) applyPitchYaw(pitch, yaw, pitchRate, yawRate float32, flags common.GIMBAL_MANAGER_FLAGS, streaming bool) common.MAV_RESULT {
	if flags&common.GIMBAL_MANAGER_FLAGS_RETRACT != 0 {
		return common.MAV_RESULT_UNSUPPORTED
	}
	if flags&common.GIMBAL_MANAGER_FLAGS_NEUTRAL != 0 {
		b.setRates(0, 0)
		b.cancelStopRepeats()
		return b.logSendErr("center", b.gimbal.Send(b.gimbal.PTZ(skydroid.PTZCenter)))
	}

	// Yaw follow/lock mode only changes on an actual transition.
	wantLock := flags&common.GIMBAL_MANAGER_FLAGS_YAW_LOCK != 0
	b.mu.Lock()
	changed := wantLock != b.yawLock
	b.yawLock = wantLock
	b.mu.Unlock()
	if changed {
		code := skydroid.PTZFollow
		if wantLock {
			code = skydroid.PTZLock
		}
		if r := b.logSendErr("lock mode", b.gimbal.Send(b.gimbal.PTZ(code))); r != common.MAV_RESULT_ACCEPTED {
			return r
		}
	}

	rp, ry := nz(pitchRate), nz(yawRate)
	if rp != 0 || ry != 0 {
		b.setRates(float64(ry), float64(rp))
		return common.MAV_RESULT_ACCEPTED
	}
	// Explicit zero rates (not NaN) stop motion.
	if !mav.IsNaN(pitchRate) || !mav.IsNaN(yawRate) {
		b.setRates(0, 0)
	}
	// If an angle command follows, don't let queued stop repeats interrupt it.
	if !mav.IsNaN(pitch) || !mav.IsNaN(yaw) {
		b.cancelStopRepeats()
	}

	if !mav.IsNaN(pitch) {
		if r := b.logSendErr("goto pitch", b.gimbal.Send(b.gimbal.GotoPitch(float64(pitch)))); r != common.MAV_RESULT_ACCEPTED {
			return r
		}
	}
	if !mav.IsNaN(yaw) {
		if r := b.logSendErr("goto yaw", b.gimbal.Send(b.gimbal.GotoYaw(float64(yaw)))); r != common.MAV_RESULT_ACCEPTED {
			return r
		}
	}
	return common.MAV_RESULT_ACCEPTED
}

func nz(f float32) float32 {
	if mav.IsNaN(f) {
		return 0
	}
	return f
}

// setRates starts, updates or stops rate motion. Non-zero rates expire after
// RateTimeout unless refreshed (dead-man), so a lost client can't leave the
// gimbal spinning.
func (b *Bridge) setRates(yaw, pitch float64) {
	b.motionMu.Lock()
	defer b.motionMu.Unlock()
	wasMoving := b.moving
	changed := yaw != b.yawRate || pitch != b.pitchRate
	if yaw == 0 && pitch == 0 {
		b.moving = false
	} else {
		b.yawRate, b.pitchRate = yaw, pitch
		b.motionUntil = time.Now().Add(b.cfg.RateTimeout)
		b.moving = true
	}
	stopNow := wasMoving && !b.moving
	// Send immediately on start or change; the motion loop repeats it.
	startNow := b.moving && (!wasMoving || changed)

	if stopNow {
		b.stopLocked()
	} else if startNow {
		b.stopsLeft = 0
		b.sendMotion(yaw, pitch)
	}
}

func (b *Bridge) motionLoop(ctx context.Context) {
	t := time.NewTicker(time.Duration(float64(time.Second) / b.cfg.MotionRateHz))
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		b.motionMu.Lock()
		switch {
		case b.moving && time.Now().After(b.motionUntil):
			b.moving = false
			b.log.Info("gimbal rate timed out, stopping")
			b.stopLocked()
		case b.moving:
			b.sendMotion(b.yawRate, b.pitchRate)
		case b.stopsLeft > 0:
			b.stopsLeft--
			b.sendStop()
		}
		b.motionMu.Unlock()
	}
}

// stopRepeats is how many extra stop packets follow a stop, one per motion tick.
const stopRepeats = 3

// stopLocked stops the gimbal now and schedules repeats. Caller holds motionMu.
func (b *Bridge) stopLocked() {
	b.sendStop()
	b.stopsLeft = stopRepeats
}

// sendMotion sends the current rates. The C12 keeps each axis's last speed, so
// a single-axis move also zeroes the idle axis (e.g. yaw+pitch -> pitch only
// must stop yaw). Caller holds motionMu.
func (b *Bridge) sendMotion(yaw, pitch float64) {
	var cmds []string
	switch {
	case yaw != 0 && pitch != 0:
		cmds = []string{b.gimbal.SpeedYawPitch(yaw, pitch)}
	case yaw != 0:
		cmds = []string{b.gimbal.SpeedYaw(yaw), b.gimbal.SpeedPitch(0)}
	default:
		cmds = []string{b.gimbal.SpeedPitch(pitch), b.gimbal.SpeedYaw(0)}
	}
	for _, c := range cmds {
		b.logSendErr("motion", b.gimbal.Send(c))
	}
}

// sendStop sends zero speed on both axes. Caller holds motionMu.
func (b *Bridge) sendStop() {
	for _, c := range b.gimbal.Stop() {
		b.logSendErr("stop", b.gimbal.Send(c))
	}
}

// cancelStopRepeats drops queued stop repeats before an angle/centre command,
// which the C12 might otherwise abort on a zero-speed packet.
func (b *Bridge) cancelStopRepeats() {
	b.motionMu.Lock()
	b.stopsLeft = 0
	b.motionMu.Unlock()
}

// stopGimbal stops all motion (startup, shutdown, explicit stop requests).
func (b *Bridge) stopGimbal() {
	b.motionMu.Lock()
	defer b.motionMu.Unlock()
	b.moving = false
	b.stopLocked()
}

func (b *Bridge) gimbalManagerInformation() *common.MessageGimbalManagerInformation {
	rad := func(d float64) float32 { return float32(d * math.Pi / 180) }
	return &common.MessageGimbalManagerInformation{
		TimeBootMs: b.bootMs(),
		CapFlags: common.GIMBAL_MANAGER_CAP_FLAGS_HAS_PITCH_AXIS |
			common.GIMBAL_MANAGER_CAP_FLAGS_HAS_PITCH_FOLLOW |
			common.GIMBAL_MANAGER_CAP_FLAGS_HAS_YAW_AXIS |
			common.GIMBAL_MANAGER_CAP_FLAGS_HAS_YAW_FOLLOW |
			common.GIMBAL_MANAGER_CAP_FLAGS_HAS_YAW_LOCK |
			common.GIMBAL_MANAGER_CAP_FLAGS_HAS_NEUTRAL,
		GimbalDeviceId: mav.CompGimbal,
		PitchMin:       rad(-90), PitchMax: rad(90),
		YawMin: rad(-90), YawMax: rad(90),
	}
}

func (b *Bridge) gimbalManagerStatus() *common.MessageGimbalManagerStatus {
	b.mu.Lock()
	defer b.mu.Unlock()
	var flags common.GIMBAL_MANAGER_FLAGS
	if b.yawLock {
		flags |= common.GIMBAL_MANAGER_FLAGS_YAW_LOCK
	}
	return &common.MessageGimbalManagerStatus{
		TimeBootMs: b.bootMs(), Flags: flags, GimbalDeviceId: mav.CompGimbal,
		PrimaryControlSysid: b.ctrlSys, PrimaryControlCompid: b.ctrlComp,
	}
}

func (b *Bridge) bootMs() uint32 { return uint32(time.Since(b.start).Milliseconds()) }
