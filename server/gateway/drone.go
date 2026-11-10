package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/bluenviron/gomavlib/v3"
	"github.com/bluenviron/gomavlib/v3/pkg/dialects/ardupilotmega"
	"github.com/bluenviron/gomavlib/v3/pkg/dialects/common"
	"github.com/bluenviron/gomavlib/v3/pkg/message"

	"github.com/parthibx24/Drone-Feed/internal/mav"
)

const (
	// 5 s matches ArduPilot's FS_GCS_TIMEOUT default. 3 s flapped on star9 while a
	// parameter download saturated the 115200-baud serial link.
	onlineTimeout    = 5 * time.Second
	reconnectGap     = 8 * time.Second
	statusTextBuffer = 100
	peripheralTimout = 5 * time.Second
)

var (
	ErrOffline       = errors.New("drone offline (no autopilot heartbeat)")
	ErrTimeout       = errors.New("no response from vehicle")
	ErrNotIdentified = errors.New("autopilot not identified yet")
	ErrRejected      = errors.New("rejected by vehicle")
)

// Event is pushed to WebSocket clients as it happens.
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

type subscription struct {
	match func(*gomavlib.EventFrame) bool
	ch    chan *gomavlib.EventFrame
}

// Drone is one MAVLink link to one vehicle.
type Drone struct {
	cfg  DroneConfig
	gw   *Config
	log  *slog.Logger
	node *gomavlib.Node

	mu         sync.RWMutex
	st         Telemetry
	target     uint8
	lastHB     time.Time
	camSeen    time.Time
	gimbalSeen time.Time
	statusText []StatusText
	params     map[string]Param
	paramTotal int

	subMu  sync.Mutex
	subs   map[int]*subscription
	nextID int

	listenMu  sync.Mutex
	listeners map[chan Event]struct{}

	cmdMu     sync.Mutex
	missionMu sync.Mutex
	paramSem  chan struct{} // one parameter transaction at a time; see lockParams
}

func newDrone(cfg DroneConfig, gw *Config, log *slog.Logger) (*Drone, error) {
	ep, err := mav.ParseEndpoint(cfg.MAVLink)
	if err != nil {
		return nil, err
	}
	node := &gomavlib.Node{
		Endpoints:              []gomavlib.EndpointConf{ep},
		Dialect:                ardupilotmega.Dialect,
		OutVersion:             gomavlib.V2,
		OutSystemID:            gw.GCSSystemID,
		OutComponentID:         gw.GCSComponentID,
		HeartbeatPeriod:        time.Second, // 1 Hz GCS heartbeat feeds ArduPilot's GCS failsafe
		HeartbeatSystemType:    int(common.MAV_TYPE_GCS),
		HeartbeatAutopilotType: int(common.MAV_AUTOPILOT_INVALID),
	}
	if err := node.Initialize(); err != nil {
		return nil, fmt.Errorf("drone %s: %w", cfg.ID, err)
	}
	d := &Drone{
		cfg: cfg, gw: gw, node: node, log: log.With("drone", cfg.ID),
		target: cfg.SystemID, params: map[string]Param{},
		subs: map[int]*subscription{}, listeners: map[chan Event]struct{}{},
		paramSem: make(chan struct{}, 1),
	}
	d.st.DroneID, d.st.Name, d.st.Mode = cfg.ID, cfg.Name, "UNKNOWN"
	return d, nil
}

func (d *Drone) run(ctx context.Context) {
	go d.linkMonitor(ctx)
	events := d.node.Events()
	for {
		select {
		case <-ctx.Done():
			d.node.Close()
			return
		case evt, ok := <-events:
			if !ok {
				return
			}
			switch e := evt.(type) {
			case *gomavlib.EventFrame:
				d.handleFrame(e)
			case *gomavlib.EventChannelOpen:
				d.log.Info("mavlink channel open", "channel", e.Channel.String())
			case *gomavlib.EventChannelClose:
				d.log.Warn("mavlink channel closed", "channel", e.Channel.String(), "err", e.Error)
			case *gomavlib.EventParseError:
				d.log.Debug("mavlink parse error", "err", e.Error)
			}
		}
	}
}

// linkMonitor reports online/offline transitions.
func (d *Drone) linkMonitor(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	prev := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		now := d.Online()
		if now != prev {
			if now {
				d.log.Info("drone online")
			} else {
				d.log.Warn("drone offline")
			}
			d.publish(Event{Type: "link", Data: map[string]bool{"online": now}})
			prev = now
		}
	}
}

// Online reports whether an autopilot heartbeat arrived recently.
func (d *Drone) Online() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return !d.lastHB.IsZero() && time.Since(d.lastHB) < onlineTimeout
}

// Snapshot returns a copy of the current telemetry.
func (d *Drone) Snapshot() Telemetry {
	d.mu.RLock()
	defer d.mu.RUnlock()
	st := d.st
	if !d.lastHB.IsZero() {
		age := round(time.Since(d.lastHB).Seconds(), 2)
		st.LastHeartbeatAgeS = &age
		st.Online = age < onlineTimeout.Seconds()
	}
	if st.Camera != nil {
		c := *st.Camera
		c.Present = time.Since(d.camSeen) < peripheralTimout
		st.Camera = &c
	}
	if st.Gimbal != nil {
		g := *st.Gimbal
		g.Present = time.Since(d.gimbalSeen) < peripheralTimout
		st.Gimbal = &g
	}
	return st
}

// StatusTexts returns recent autopilot messages, oldest first.
func (d *Drone) StatusTexts() []StatusText {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return append([]StatusText(nil), d.statusText...)
}

func (d *Drone) targetSystem() (uint8, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.target == 0 {
		return 0, ErrNotIdentified
	}
	if d.lastHB.IsZero() || time.Since(d.lastHB) >= onlineTimeout {
		return 0, ErrOffline
	}
	return d.target, nil
}

func (d *Drone) write(msg message.Message) error { return d.node.WriteMessageAll(msg) }

// ------------------------------------------------------------ subscriptions

func (d *Drone) subscribe(match func(*gomavlib.EventFrame) bool) (<-chan *gomavlib.EventFrame, func()) {
	s := &subscription{match: match, ch: make(chan *gomavlib.EventFrame, 64)}
	d.subMu.Lock()
	id := d.nextID
	d.nextID++
	d.subs[id] = s
	d.subMu.Unlock()
	return s.ch, func() {
		d.subMu.Lock()
		delete(d.subs, id)
		d.subMu.Unlock()
	}
}

func (d *Drone) deliver(fr *gomavlib.EventFrame) {
	d.subMu.Lock()
	defer d.subMu.Unlock()
	for _, s := range d.subs {
		if s.match(fr) {
			select {
			case s.ch <- fr:
			default: // subscriber not keeping up; it will retry
			}
		}
	}
}

// fromAutopilot matches frames from the identified vehicle's autopilot.
func (d *Drone) fromAutopilot(fr *gomavlib.EventFrame) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return fr.SystemID() == d.target && fr.ComponentID() == mav.CompAutopilot
}

// addressedToUs is true for messages with our IDs as target, or no target.
func (d *Drone) addressedToUs(sys, comp uint8) bool {
	return (sys == 0 || sys == d.gw.GCSSystemID) && (comp == 0 || comp == d.gw.GCSComponentID)
}

// ------------------------------------------------------------ WS listeners

func (d *Drone) listen() (chan Event, func()) {
	ch := make(chan Event, 32)
	d.listenMu.Lock()
	d.listeners[ch] = struct{}{}
	d.listenMu.Unlock()
	return ch, func() {
		d.listenMu.Lock()
		delete(d.listeners, ch)
		d.listenMu.Unlock()
	}
}

func (d *Drone) publish(e Event) {
	d.listenMu.Lock()
	defer d.listenMu.Unlock()
	for ch := range d.listeners {
		select {
		case ch <- e:
		default:
		}
	}
}

// ------------------------------------------------------------ frame handling

func (d *Drone) handleFrame(fr *gomavlib.EventFrame) {
	sys, comp := fr.SystemID(), fr.ComponentID()

	if hb, ok := fr.Message().(*common.MessageHeartbeat); ok {
		d.handleHeartbeat(sys, comp, hb)
	}

	d.mu.RLock()
	target := d.target
	d.mu.RUnlock()
	if target == 0 || sys != target {
		return // not our vehicle (another GCS, or not identified yet)
	}

	if comp == mav.CompAutopilot {
		d.handleAutopilot(fr)
	} else if gs, ok := fr.Message().(*common.MessageGimbalManagerStatus); ok {
		d.mu.Lock()
		g := &GimbalState{}
		if d.st.Gimbal != nil {
			*g = *d.st.Gimbal
		}
		g.YawLock = gs.Flags&common.GIMBAL_MANAGER_FLAGS_YAW_LOCK != 0
		d.st.Gimbal = g
		d.mu.Unlock()
	}
	d.deliver(fr)
}

func (d *Drone) handleHeartbeat(sys, comp uint8, hb *common.MessageHeartbeat) {
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()

	switch {
	case comp == mav.CompAutopilot && mav.IsAutopilotHeartbeat(hb):
		if d.target == 0 {
			d.target = sys
			d.log.Info("autopilot identified", "sysid", sys, "type", hb.Type.String())
		}
		if sys != d.target {
			return
		}
		reconnect := d.lastHB.IsZero() || now.Sub(d.lastHB) > reconnectGap
		d.lastHB = now
		d.st.SystemID = sys
		d.st.VehicleType = strings.TrimPrefix(hb.Type.String(), "MAV_TYPE_")
		d.st.Armed = hb.BaseMode&common.MAV_MODE_FLAG_SAFETY_ARMED != 0
		d.st.Mode = mav.CopterModeName(hb.CustomMode)
		d.st.SystemStatus = strings.TrimPrefix(hb.SystemStatus.String(), "MAV_STATE_")
		d.st.UpdatedAt = now
		if reconnect {
			go d.onConnect()
		}

	case sys == d.target && hb.Type == common.MAV_TYPE_CAMERA:
		d.camSeen = now
		if d.st.Camera == nil {
			d.log.Info("camera component found", "compid", comp)
		}
		d.st.Camera = &Peripheral{Present: true, LastSeen: now}

	case sys == d.target && hb.Type == common.MAV_TYPE_GIMBAL:
		d.gimbalSeen = now
		g := &GimbalState{}
		if d.st.Gimbal != nil {
			*g = *d.st.Gimbal
		} else {
			d.log.Info("gimbal component found", "compid", comp)
		}
		g.LastSeen = now
		d.st.Gimbal = g
	}
}

func (d *Drone) handleAutopilot(fr *gomavlib.EventFrame) {
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	st := &d.st

	switch m := fr.Message().(type) {
	// Sub-structs are replaced, never modified in place: Snapshot() hands out
	// shallow copies that JSON encoders read without holding the lock.
	case *common.MessageGlobalPositionInt:
		p := &Position{}
		p.Lat, p.Lon = float64(m.Lat)/1e7, float64(m.Lon)/1e7
		p.AltMSLM, p.AltRelM = float64(m.Alt)/1000, float64(m.RelativeAlt)/1000
		p.VNorthMS, p.VEastMS, p.VDownMS = float64(m.Vx)/100, float64(m.Vy)/100, float64(m.Vz)/100
		if m.Hdg != 65535 {
			p.HeadingDeg = f64(float64(m.Hdg) / 100)
		}
		st.Position = p

	case *common.MessageAttitude:
		st.Attitude = &Attitude{
			RollDeg: round(deg(m.Roll), 2), PitchDeg: round(deg(m.Pitch), 2), YawDeg: round(deg(m.Yaw), 2),
		}

	case *common.MessageVfrHud:
		st.Speed = &Speed{
			GroundMS: round(float64(m.Groundspeed), 2), AirMS: round(float64(m.Airspeed), 2),
			ClimbMS: round(float64(m.Climb), 2), ThrottlePct: int(m.Throttle),
		}

	case *common.MessageSysStatus:
		b := &Battery{}
		if st.Battery != nil {
			*b = *st.Battery
		}
		st.Battery = b
		if m.VoltageBattery != 65535 {
			b.VoltageV = f64(float64(m.VoltageBattery) / 1000)
		}
		if m.CurrentBattery >= 0 {
			b.CurrentA = f64(float64(m.CurrentBattery) / 100)
		}
		if m.BatteryRemaining >= 0 {
			b.RemainingPct = ip(int(m.BatteryRemaining))
		}

	case *common.MessageBatteryStatus:
		if m.Id != 0 {
			break // primary battery only
		}
		b := &Battery{}
		if st.Battery != nil {
			*b = *st.Battery
		}
		st.Battery = b
		if m.CurrentConsumed >= 0 {
			b.ConsumedMAh = ip(int(m.CurrentConsumed))
		}

	case *common.MessageGpsRawInt:
		g := &GPS{FixType: strings.TrimPrefix(m.FixType.String(), "GPS_FIX_TYPE_"), Satellites: int(m.SatellitesVisible)}
		if m.Eph != 65535 {
			g.HDOP = f64(float64(m.Eph) / 100)
		}
		st.GPS = g

	case *common.MessageHomePosition:
		st.Home = &Home{Lat: float64(m.Latitude) / 1e7, Lon: float64(m.Longitude) / 1e7, AltMSLM: float64(m.Altitude) / 1000}

	case *common.MessageExtendedSysState:
		st.LandedState = strings.TrimPrefix(m.LandedState.String(), "MAV_LANDED_STATE_")

	case *common.MessageMissionCurrent:
		st.Mission = &MissionState{CurrentSeq: int(m.Seq), Total: int(m.Total)}
		if m.MissionState != 0 {
			st.Mission.State = strings.TrimPrefix(m.MissionState.String(), "MISSION_STATE_")
		}

	case *common.MessageMissionItemReached:
		go d.publish(Event{Type: "mission_item_reached", Data: map[string]int{"seq": int(m.Seq)}})

	case *ardupilotmega.MessageEkfStatusReport:
		// Healthy = attitude, horizontal velocity and absolute position all OK.
		const need = 0x01 | 0x02 | 0x10
		st.EKF = &EKF{Flags: uint16(m.Flags), Healthy: uint16(m.Flags)&need == need}

	case *common.MessageStatustext:
		t := StatusText{Time: now, Severity: strings.TrimPrefix(m.Severity.String(), "MAV_SEVERITY_"), Text: m.Text}
		d.statusText = append(d.statusText, t)
		if len(d.statusText) > statusTextBuffer {
			d.statusText = d.statusText[len(d.statusText)-statusTextBuffer:]
		}
		go d.publish(Event{Type: "statustext", Data: t})

	case *common.MessageParamValue:
		d.params[m.ParamId] = Param{Name: m.ParamId, Value: m.ParamValue, Type: uint8(m.ParamType), Index: m.ParamIndex}
		if m.ParamCount > 0 && m.ParamIndex != 65535 {
			d.paramTotal = int(m.ParamCount)
		}

	case *common.MessageAutopilotVersion:
		v := m.FlightSwVersion
		st.Firmware = fmt.Sprintf("%d.%d.%d", v>>24&0xff, v>>16&0xff, v>>8&0xff)
	}
	st.UpdatedAt = now
}

// onConnect runs after the first heartbeat (or after a link outage): it asks
// for the telemetry we need at the rates we want, plus one-off info.
func (d *Drone) onConnect() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	d.log.Info("link up: configuring telemetry streams")
	for name, hz := range d.gw.StreamRates {
		interval := float32(-1)
		if hz > 0 {
			interval = float32(1e6 / hz)
		}
		id := streamMessageIDs[name]
		if _, err := d.CommandLong(ctx, mav.CompAutopilot, common.MAV_CMD_SET_MESSAGE_INTERVAL,
			[7]float32{float32(id), interval}); err != nil {
			d.log.Warn("set message interval failed", "message", name, "err", err)
		}
	}
	for _, id := range []float32{148, 242} { // AUTOPILOT_VERSION, HOME_POSITION
		if _, err := d.CommandLong(ctx, mav.CompAutopilot, common.MAV_CMD_REQUEST_MESSAGE, [7]float32{id}); err != nil {
			d.log.Warn("request message failed", "id", id, "err", err)
		}
	}
}
