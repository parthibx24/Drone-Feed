// Package mav holds small MAVLink helpers shared by gimbal-bridge and drone-gateway.
package mav

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/bluenviron/gomavlib/v3"
	"github.com/bluenviron/gomavlib/v3/pkg/dialects/common"
	"github.com/bluenviron/gomavlib/v3/pkg/frame"
	"github.com/bluenviron/gomavlib/v3/pkg/message"
)

// Standard component IDs used by this stack.
const (
	CompAutopilot = uint8(common.MAV_COMP_ID_AUTOPILOT1) // 1
	CompCamera    = uint8(common.MAV_COMP_ID_CAMERA)     // 100
	CompGimbal    = uint8(common.MAV_COMP_ID_GIMBAL)     // 154
	CompGateway   = uint8(common.MAV_COMP_ID_MISSIONPLANNER)
)

// CopterModes maps ArduCopter custom_mode numbers to names (ArduCopter 4.7).
var CopterModes = map[uint32]string{
	0: "STABILIZE", 1: "ACRO", 2: "ALT_HOLD", 3: "AUTO", 4: "GUIDED", 5: "LOITER",
	6: "RTL", 7: "CIRCLE", 9: "LAND", 11: "DRIFT", 13: "SPORT", 14: "FLIP",
	15: "AUTOTUNE", 16: "POSHOLD", 17: "BRAKE", 18: "THROW", 19: "AVOID_ADSB",
	20: "GUIDED_NOGPS", 21: "SMART_RTL", 22: "FLOWHOLD", 23: "FOLLOW", 24: "ZIGZAG",
	25: "SYSTEMID", 26: "AUTOROTATE", 27: "AUTO_RTL", 28: "TURTLE",
}

// CopterModeName returns the mode name or MODE_<n>.
func CopterModeName(n uint32) string {
	if s, ok := CopterModes[n]; ok {
		return s
	}
	return "MODE_" + strconv.Itoa(int(n))
}

// CopterModeNumber resolves a mode name (case-insensitive).
func CopterModeNumber(name string) (uint32, bool) {
	name = strings.ToUpper(strings.TrimSpace(name))
	for n, s := range CopterModes {
		if s == name {
			return n, true
		}
	}
	return 0, false
}

// CopterModeNames returns all mode names sorted by number.
func CopterModeNames() []string {
	keys := make([]int, 0, len(CopterModes))
	for k := range CopterModes {
		keys = append(keys, int(k))
	}
	sort.Ints(keys)
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = CopterModes[uint32(k)]
	}
	return out
}

// NaN is the MAVLink "ignore this field" value.
var NaN = float32(math.NaN())

// IsNaN reports whether f is NaN.
func IsNaN(f float32) bool { return f != f }

// Sender writes frames on a node under arbitrary system/component IDs, keeping
// a separate sequence counter per component. gomavlib's own Write* helpers
// always use the node's OutSystemID/OutComponentID, which is not enough for a
// process that hosts more than one MAVLink component.
type Sender struct {
	Node *gomavlib.Node

	mu  sync.Mutex
	seq map[uint8]uint8
}

// Send writes msg to every channel as sysID/compID.
func (s *Sender) Send(sysID, compID uint8, msg message.Message) error {
	return s.send(nil, sysID, compID, msg)
}

// SendTo writes msg to one channel as sysID/compID.
func (s *Sender) SendTo(ch *gomavlib.Channel, sysID, compID uint8, msg message.Message) error {
	return s.send(ch, sysID, compID, msg)
}

func (s *Sender) send(ch *gomavlib.Channel, sysID, compID uint8, msg message.Message) error {
	s.mu.Lock()
	if s.seq == nil {
		s.seq = map[uint8]uint8{}
	}
	seq := s.seq[compID]
	s.seq[compID] = seq + 1
	s.mu.Unlock()

	fr := &frame.V2Frame{SequenceNumber: seq, SystemID: sysID, ComponentID: compID, Message: msg}
	if err := s.Node.FixFrame(fr); err != nil {
		return err
	}
	if ch != nil {
		return s.Node.WriteFrameTo(ch, fr)
	}
	return s.Node.WriteFrameAll(fr)
}

// IsAutopilotHeartbeat reports whether hb comes from a flight controller
// (not a GCS, camera, gimbal or other peripheral).
func IsAutopilotHeartbeat(hb *common.MessageHeartbeat) bool {
	if hb.Autopilot == common.MAV_AUTOPILOT_INVALID {
		return false
	}
	switch hb.Type {
	case common.MAV_TYPE_GCS, common.MAV_TYPE_GIMBAL, common.MAV_TYPE_CAMERA,
		common.MAV_TYPE_ONBOARD_CONTROLLER, common.MAV_TYPE_ADSB, common.MAV_TYPE_ODID:
		return false
	}
	return true
}
