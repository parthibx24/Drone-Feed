// Package sim is a small simulated ArduCopter for tests and frontend
// development. It speaks enough MAVLink to exercise drone-gateway: heartbeats,
// telemetry, COMMAND_LONG/INT with ACKs, the mission and parameter protocols.
// It can also act as a MAVLink router (like mavlink-router on the Pi), so a
// gimbal-bridge can be attached to it.
//
// It is NOT a flight simulator. Use ArduPilot SITL for anything that depends
// on real flight behaviour.
package sim

import (
	"context"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/bluenviron/gomavlib/v3"
	"github.com/bluenviron/gomavlib/v3/pkg/dialects/ardupilotmega"
	"github.com/bluenviron/gomavlib/v3/pkg/dialects/common"
	"github.com/bluenviron/gomavlib/v3/pkg/message"
)

const (
	modeGuided = 4
	modeAuto   = 3
	modeLand   = 9
	modeRTL    = 6
)

// Received records a command the vehicle saw.
type Received struct {
	Command      common.MAV_CMD
	Confirmation uint8
	Params       [7]float32
	X, Y         int32
	Z            float32
}

type param struct {
	value float32
	typ   common.MAV_PARAM_TYPE
	max   float32
}

// Vehicle is the simulated autopilot (system 1, component 1).
type Vehicle struct {
	node  *gomavlib.Node
	route bool

	mu         sync.Mutex
	Armed      bool
	Mode       uint32
	Lat, Lon   float64
	AltRel     float64
	targetLat  float64
	targetLon  float64
	targetAlt  float64
	PrearmFail bool
	// DropAcks drops the next N ACKs for a command (tests retries).
	DropAcks map[common.MAV_CMD]int
	// DropMissionRequests drops the next N mission item requests.
	DropMissionRequests int
	// DropParamOnce drops these indexes the first time the list is sent.
	DropParamOnce map[uint16]bool
	Mission       []*common.MessageMissionItemInt
	Commands      []Received

	uploadCount uint16
	paramNames  []string
	params      map[string]*param
}

// New creates a vehicle on the given endpoints. With route=true every frame
// received on one endpoint is forwarded to the others.
func New(endpoints []gomavlib.EndpointConf, route bool) (*Vehicle, error) {
	node := &gomavlib.Node{
		Endpoints: endpoints, Dialect: ardupilotmega.Dialect, OutVersion: gomavlib.V2,
		OutSystemID: 1, OutComponentID: 1, HeartbeatDisable: true,
	}
	if err := node.Initialize(); err != nil {
		return nil, err
	}
	v := &Vehicle{
		node: node, route: route, Mode: 0, Lat: 23.8103, Lon: 90.4125,
		DropAcks: map[common.MAV_CMD]int{}, DropParamOnce: map[uint16]bool{},
		params: map[string]*param{
			"RTL_ALT_M":    {value: 15, typ: common.MAV_PARAM_TYPE_REAL32, max: 3000},
			"WP_SPD":       {value: 5, typ: common.MAV_PARAM_TYPE_REAL32, max: 20},
			"FENCE_ENABLE": {value: 0, typ: common.MAV_PARAM_TYPE_INT8, max: 1},
			"MAV_SYSID":    {value: 1, typ: common.MAV_PARAM_TYPE_INT32, max: 255},
		},
	}
	for n := range v.params {
		v.paramNames = append(v.paramNames, n)
	}
	sort.Strings(v.paramNames)
	v.targetLat, v.targetLon = v.Lat, v.Lon
	return v, nil
}

// Close stops the vehicle.
func (v *Vehicle) Close() { v.node.Close() }

func (v *Vehicle) send(m message.Message) { _ = v.node.WriteMessageAll(m) }

// Run processes MAVLink and publishes telemetry until ctx ends.
func (v *Vehicle) Run(ctx context.Context) {
	go v.telemetryLoop(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-v.node.Events():
			if !ok {
				return
			}
			fr, ok := evt.(*gomavlib.EventFrame)
			if !ok {
				continue
			}
			// Handle before routing: WriteFrameExcept re-encodes the frame in
			// place, after which its message is raw bytes.
			v.handle(fr)
			if v.route {
				_ = v.node.WriteFrameExcept(fr.Channel, fr.Frame)
			}
		}
	}
}

func (v *Vehicle) telemetryLoop(ctx context.Context) {
	t := time.NewTicker(200 * time.Millisecond)
	defer t.Stop()
	n := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		n++
		v.mu.Lock()
		v.step(0.2)
		base := common.MAV_MODE_FLAG_CUSTOM_MODE_ENABLED
		if v.Armed {
			base |= common.MAV_MODE_FLAG_SAFETY_ARMED
		}
		hb := &common.MessageHeartbeat{
			Type: common.MAV_TYPE_QUADROTOR, Autopilot: common.MAV_AUTOPILOT_ARDUPILOTMEGA,
			BaseMode: base, CustomMode: v.Mode, SystemStatus: common.MAV_STATE_STANDBY, MavlinkVersion: 3,
		}
		if v.Armed {
			hb.SystemStatus = common.MAV_STATE_ACTIVE
		}
		pos := &common.MessageGlobalPositionInt{
			TimeBootMs: uint32(n * 200), Lat: int32(v.Lat * 1e7), Lon: int32(v.Lon * 1e7),
			Alt: int32((v.AltRel + 10) * 1000), RelativeAlt: int32(v.AltRel * 1000), Hdg: 9000,
		}
		v.mu.Unlock()
		if n%5 == 0 {
			v.send(hb)
			v.send(&common.MessageSysStatus{VoltageBattery: 16400, CurrentBattery: 520, BatteryRemaining: 87})
			v.send(&common.MessageGpsRawInt{FixType: common.GPS_FIX_TYPE_3D_FIX, SatellitesVisible: 14, Eph: 80})
		}
		v.send(pos)
		v.send(&common.MessageAttitude{Roll: 0.01, Pitch: -0.02, Yaw: 1.57})
	}
}

// step moves toward the target at ~5 m/s horizontally and 2 m/s vertically.
func (v *Vehicle) step(dt float64) {
	if !v.Armed {
		return
	}
	const mPerDeg = 111_320.0
	dLat, dLon := v.targetLat-v.Lat, v.targetLon-v.Lon
	dist := math.Hypot(dLat, dLon) * mPerDeg
	if move := 5 * dt; dist > move {
		v.Lat += dLat * move / dist
		v.Lon += dLon * move / dist
	} else {
		v.Lat, v.Lon = v.targetLat, v.targetLon
	}
	if dAlt := v.targetAlt - v.AltRel; math.Abs(dAlt) > 2*dt {
		v.AltRel += math.Copysign(2*dt, dAlt)
	} else {
		v.AltRel = v.targetAlt
	}
	if v.Mode == modeLand && v.AltRel <= 0 {
		v.Armed = false
	}
}

func (v *Vehicle) ack(fr *gomavlib.EventFrame, cmd common.MAV_CMD, res common.MAV_RESULT) {
	v.mu.Lock()
	if v.DropAcks[cmd] > 0 {
		v.DropAcks[cmd]--
		v.mu.Unlock()
		return
	}
	v.mu.Unlock()
	v.send(&common.MessageCommandAck{Command: cmd, Result: res, TargetSystem: fr.SystemID(), TargetComponent: fr.ComponentID()})
}

func (v *Vehicle) handle(fr *gomavlib.EventFrame) {
	switch m := fr.Message().(type) {
	case *common.MessageCommandLong:
		if m.TargetSystem != 1 || m.TargetComponent != 1 {
			return
		}
		p := [7]float32{m.Param1, m.Param2, m.Param3, m.Param4, m.Param5, m.Param6, m.Param7}
		v.mu.Lock()
		v.Commands = append(v.Commands, Received{Command: m.Command, Confirmation: m.Confirmation, Params: p})
		v.mu.Unlock()
		v.ack(fr, m.Command, v.commandLong(m.Command, p))

	case *common.MessageCommandInt:
		if m.TargetSystem != 1 || m.TargetComponent != 1 {
			return
		}
		v.mu.Lock()
		v.Commands = append(v.Commands, Received{Command: m.Command, Params: [7]float32{m.Param1, m.Param2, m.Param3, m.Param4}, X: m.X, Y: m.Y, Z: m.Z})
		res := common.MAV_RESULT_UNSUPPORTED
		switch m.Command {
		case common.MAV_CMD_DO_REPOSITION:
			if v.Armed {
				v.Mode, v.targetLat, v.targetLon, v.targetAlt = modeGuided, float64(m.X)/1e7, float64(m.Y)/1e7, float64(m.Z)
				res = common.MAV_RESULT_ACCEPTED
			} else {
				res = common.MAV_RESULT_DENIED
			}
		case common.MAV_CMD_DO_SET_ROI_LOCATION:
			res = common.MAV_RESULT_ACCEPTED
		}
		v.mu.Unlock()
		v.ack(fr, m.Command, res)

	case *common.MessageMissionCount:
		if m.TargetSystem != 1 {
			return
		}
		v.mu.Lock()
		v.uploadCount = m.Count
		v.Mission = make([]*common.MessageMissionItemInt, 0, m.Count)
		v.mu.Unlock()
		v.requestItem(fr, 0)

	case *common.MessageMissionItemInt:
		if m.TargetSystem != 1 {
			return
		}
		v.mu.Lock()
		expected := uint16(len(v.Mission))
		if m.Seq != expected {
			v.mu.Unlock()
			v.requestItem(fr, expected) // duplicate/out of order: ask again
			return
		}
		cp := *m
		v.Mission = append(v.Mission, &cp)
		done := uint16(len(v.Mission)) == v.uploadCount
		v.mu.Unlock()
		if done {
			v.send(&common.MessageMissionAck{TargetSystem: fr.SystemID(), TargetComponent: fr.ComponentID(), Type: common.MAV_MISSION_ACCEPTED})
		} else {
			v.requestItem(fr, m.Seq+1)
		}

	case *common.MessageMissionRequestList:
		if m.TargetSystem != 1 {
			return
		}
		v.mu.Lock()
		n := uint16(len(v.Mission))
		v.mu.Unlock()
		v.send(&common.MessageMissionCount{TargetSystem: fr.SystemID(), TargetComponent: fr.ComponentID(), Count: n})

	case *common.MessageMissionRequestInt:
		if m.TargetSystem != 1 {
			return
		}
		v.mu.Lock()
		var it *common.MessageMissionItemInt
		if int(m.Seq) < len(v.Mission) {
			cp := *v.Mission[m.Seq]
			it = &cp
		}
		v.mu.Unlock()
		if it != nil {
			it.TargetSystem, it.TargetComponent = fr.SystemID(), fr.ComponentID()
			v.send(it)
		}

	case *common.MessageMissionClearAll:
		if m.TargetSystem != 1 {
			return
		}
		v.mu.Lock()
		v.Mission = nil
		v.mu.Unlock()
		v.send(&common.MessageMissionAck{TargetSystem: fr.SystemID(), TargetComponent: fr.ComponentID(), Type: common.MAV_MISSION_ACCEPTED})

	case *common.MessageParamRequestList:
		if m.TargetSystem != 1 {
			return
		}
		for i, name := range v.paramNames {
			v.mu.Lock()
			drop := v.DropParamOnce[uint16(i)]
			delete(v.DropParamOnce, uint16(i))
			v.mu.Unlock()
			if !drop {
				v.sendParam(name, uint16(i))
			}
		}

	case *common.MessageParamRequestRead:
		if m.TargetSystem != 1 {
			return
		}
		if m.ParamIndex >= 0 && int(m.ParamIndex) < len(v.paramNames) {
			v.sendParam(v.paramNames[m.ParamIndex], uint16(m.ParamIndex))
			return
		}
		for i, n := range v.paramNames {
			if n == m.ParamId {
				v.sendParam(n, uint16(i))
			}
		}

	case *common.MessageParamSet:
		if m.TargetSystem != 1 {
			return
		}
		v.mu.Lock()
		p, ok := v.params[m.ParamId]
		if ok && m.ParamValue <= p.max && m.ParamValue >= 0 {
			p.value = m.ParamValue
		}
		v.mu.Unlock()
		if ok {
			v.sendParam(m.ParamId, 65535)
		}
	}
}

func (v *Vehicle) requestItem(fr *gomavlib.EventFrame, seq uint16) {
	v.mu.Lock()
	if v.DropMissionRequests > 0 {
		v.DropMissionRequests--
		v.mu.Unlock()
		return
	}
	v.mu.Unlock()
	v.send(&common.MessageMissionRequestInt{TargetSystem: fr.SystemID(), TargetComponent: fr.ComponentID(), Seq: seq})
}

func (v *Vehicle) sendParam(name string, index uint16) {
	v.mu.Lock()
	p := v.params[name]
	msg := &common.MessageParamValue{ParamId: name, ParamValue: p.value, ParamType: p.typ,
		ParamCount: uint16(len(v.paramNames)), ParamIndex: index}
	v.mu.Unlock()
	v.send(msg)
}

func (v *Vehicle) commandLong(cmd common.MAV_CMD, p [7]float32) common.MAV_RESULT {
	v.mu.Lock()
	defer v.mu.Unlock()
	switch cmd {
	case common.MAV_CMD_COMPONENT_ARM_DISARM:
		if p[0] == 1 {
			if v.PrearmFail && p[1] != 21196 {
				go v.send(&common.MessageStatustext{Severity: common.MAV_SEVERITY_CRITICAL, Text: "PreArm: RC not found"})
				return common.MAV_RESULT_FAILED
			}
			v.Armed = true
		} else {
			if v.AltRel > 0.5 && p[1] != 21196 {
				return common.MAV_RESULT_FAILED // ArduCopter refuses to disarm in flight
			}
			v.Armed = false
		}
		return common.MAV_RESULT_ACCEPTED
	case common.MAV_CMD_DO_SET_MODE:
		v.Mode = uint32(p[1])
		if v.Mode == modeRTL {
			v.targetLat, v.targetLon = 23.8103, 90.4125
		}
		if v.Mode == modeLand {
			v.targetAlt = 0
		}
		return common.MAV_RESULT_ACCEPTED
	case common.MAV_CMD_NAV_TAKEOFF:
		if !v.Armed || v.Mode != modeGuided {
			return common.MAV_RESULT_FAILED
		}
		v.targetAlt = float64(p[6])
		return common.MAV_RESULT_ACCEPTED
	case common.MAV_CMD_MISSION_START:
		if len(v.Mission) < 2 {
			return common.MAV_RESULT_FAILED
		}
		v.Mode = modeAuto
		return common.MAV_RESULT_ACCEPTED
	case common.MAV_CMD_REQUEST_MESSAGE:
		if uint32(p[0]) == 148 {
			go v.send(&common.MessageAutopilotVersion{FlightSwVersion: 4<<24 | 7<<16 | 0<<8 | 255})
		}
		if uint32(p[0]) == 242 {
			go v.send(&common.MessageHomePosition{Latitude: int32(23.8103 * 1e7), Longitude: int32(90.4125 * 1e7), Altitude: 10000})
		}
		return common.MAV_RESULT_ACCEPTED
	case common.MAV_CMD_SET_MESSAGE_INTERVAL, common.MAV_CMD_DO_CHANGE_SPEED, common.MAV_CMD_DO_PAUSE_CONTINUE,
		common.MAV_CMD_DO_SET_MISSION_CURRENT, common.MAV_CMD_DO_SET_ROI_NONE:
		return common.MAV_RESULT_ACCEPTED
	case common.MAV_CMD_PREFLIGHT_REBOOT_SHUTDOWN:
		if v.Armed {
			return common.MAV_RESULT_DENIED
		}
		return common.MAV_RESULT_ACCEPTED
	}
	return common.MAV_RESULT_UNSUPPORTED
}

// Snapshot returns copies of the recorded state (for tests).
func (v *Vehicle) Snapshot() (armed bool, mode uint32, mission []*common.MessageMissionItemInt, cmds []Received) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.Armed, v.Mode, append([]*common.MessageMissionItemInt(nil), v.Mission...), append([]Received(nil), v.Commands...)
}

// Set runs f with the vehicle locked (for tests to inject faults).
func (v *Vehicle) Set(f func(v *Vehicle)) {
	v.mu.Lock()
	defer v.mu.Unlock()
	f(v)
}
