package gateway

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/bluenviron/gomavlib/v3"
	"github.com/bluenviron/gomavlib/v3/pkg/dialects/common"
	"github.com/bluenviron/gomavlib/v3/pkg/message"

	"github.com/parthibx24/Drone-Feed/internal/mav"
)

const (
	missionStepTimeout = 1500 * time.Millisecond
	missionRetries     = 5
)

// MissionItem is the API representation of one mission step.
//
// Types and the fields they use:
//
//	takeoff      alt_m
//	waypoint     lat, lon, alt_m, [hold_s], [accept_radius_m]
//	loiter_time  seconds, [lat, lon, alt_m]   (no position = loiter where it is)
//	land         [lat, lon]                    (no position = land where it is)
//	rtl
//	speed        speed_m_s
//	delay        seconds
//	photo
//	photo_interval seconds, [count]          (count 0 = until photo_stop)
//	photo_stop
//	video_start
//	video_stop
//	roi          lat, lon, alt_m   (point the vehicle at a location)
//	roi_none
//	raw          command, frame, params[4], x, y, z (MISSION_ITEM_INT fields)
type MissionItem struct {
	Seq  int      `json:"seq,omitempty"`
	Type string   `json:"type"`
	Lat  *float64 `json:"lat,omitempty"`
	Lon  *float64 `json:"lon,omitempty"`
	AltM *float64 `json:"alt_m,omitempty"`
	// AltFrame is what alt_m is measured from: "relative" (above home, the
	// default), "terrain" (above ground) or "absolute" (above mean sea level).
	// Kept on read and write so missions from QGC/Mission Planner round-trip.
	AltFrame      string      `json:"alt_frame,omitempty"`
	HoldS         *float64    `json:"hold_s,omitempty"`
	AcceptRadiusM *float64    `json:"accept_radius_m,omitempty"`
	Seconds       *float64    `json:"seconds,omitempty"`
	SpeedMS       *float64    `json:"speed_m_s,omitempty"`
	Count         *int        `json:"count,omitempty"`
	Command       *uint16     `json:"command,omitempty"`
	Frame         *uint8      `json:"frame,omitempty"`
	Params        *[4]float32 `json:"params,omitempty"`
	X             *int32      `json:"x,omitempty"`
	Y             *int32      `json:"y,omitempty"`
	Z             *float32    `json:"z,omitempty"`
}

// MissionError is a non-ACCEPTED MISSION_ACK.
type MissionError struct{ Result common.MAV_MISSION_RESULT }

func (e *MissionError) Error() string {
	return "mission rejected: " + strings.TrimPrefix(e.Result.String(), "MAV_MISSION_")
}

// nearNullIsland reports coordinates within about 11 km of 0,0. That is open
// ocean; such coordinates only come from a position read while the drone had
// no GPS fix (ArduPilot then reports 0,0).
func nearNullIsland(lat, lon float64) bool {
	return math.Abs(lat) < 0.1 && math.Abs(lon) < 0.1
}

func (d *Drone) validPos(it MissionItem, needAlt bool) error {
	if it.Lat == nil || it.Lon == nil {
		return fmt.Errorf("%s: lat and lon are required", it.Type)
	}
	if *it.Lat < -90 || *it.Lat > 90 || *it.Lon < -180 || *it.Lon > 180 {
		return fmt.Errorf("%s: lat/lon out of range", it.Type)
	}
	if nearNullIsland(*it.Lat, *it.Lon) {
		return fmt.Errorf("%s: lat/lon %.5f,%.5f is next to 0,0, the position a drone reports without GPS; place it on the map again", it.Type, *it.Lat, *it.Lon)
	}
	if needAlt {
		return d.validAlt(it)
	}
	return nil
}

func (d *Drone) validAlt(it MissionItem) error {
	if it.AltM == nil {
		return fmt.Errorf("%s: alt_m is required", it.Type)
	}
	alt := *it.AltM
	if it.AltFrame == "absolute" {
		// Check the height above home; that needs a home position.
		snap := d.Snapshot()
		if snap.Home == nil {
			return fmt.Errorf("%s: absolute (MSL) altitudes need the drone's home position, which is not known yet", it.Type)
		}
		alt -= snap.Home.AltMSLM
	}
	if alt <= 0 || alt > d.gw.Limits.MaxAltM {
		return fmt.Errorf("%s: altitude must be > 0 and <= %.0f m above %s (limits.max_alt_m)", it.Type, d.gw.Limits.MaxAltM, altRef(it.AltFrame))
	}
	return nil
}

func altRef(frame string) string {
	if frame == "terrain" {
		return "ground"
	}
	return "home"
}

// altFrames maps alt_frame names to the MAVLink frame used on upload.
var altFrames = map[string]common.MAV_FRAME{
	"":         common.MAV_FRAME_GLOBAL_RELATIVE_ALT_INT,
	"relative": common.MAV_FRAME_GLOBAL_RELATIVE_ALT_INT,
	"terrain":  common.MAV_FRAME_GLOBAL_TERRAIN_ALT_INT,
	"absolute": common.MAV_FRAME_GLOBAL_INT,
}

// altFrameName maps a downloaded item's frame back to an alt_frame name;
// ok is false for frames the typed items can't represent.
func altFrameName(f common.MAV_FRAME) (name string, ok bool) {
	switch f {
	case common.MAV_FRAME_GLOBAL_RELATIVE_ALT, common.MAV_FRAME_GLOBAL_RELATIVE_ALT_INT:
		return "", true
	case common.MAV_FRAME_GLOBAL_TERRAIN_ALT, common.MAV_FRAME_GLOBAL_TERRAIN_ALT_INT:
		return "terrain", true
	case common.MAV_FRAME_GLOBAL, common.MAV_FRAME_GLOBAL_INT:
		return "absolute", true
	}
	return "", false
}

func val(p *float64) float32 {
	if p == nil {
		return 0
	}
	return float32(*p)
}

func degE7(p *float64) int32 {
	if p == nil {
		return 0
	}
	return int32(math.Round(*p * 1e7))
}

// BuildMission converts API items to MISSION_ITEM_INT messages. Index 0 is a
// home placeholder (ArduPilot overwrites item 0 with home), so user items
// start at sequence 1.
func (d *Drone) BuildMission(items []MissionItem) ([]*common.MessageMissionItemInt, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("mission is empty")
	}
	if len(items) > d.gw.Limits.MaxMissionItems {
		return nil, fmt.Errorf("mission has %d items; limit is %d", len(items), d.gw.Limits.MaxMissionItems)
	}
	snap := d.Snapshot()
	home := &common.MessageMissionItemInt{
		Seq: 0, Frame: common.MAV_FRAME_GLOBAL, Command: common.MAV_CMD_NAV_WAYPOINT, Autocontinue: 1,
		MissionType: common.MAV_MISSION_TYPE_MISSION,
	}
	if snap.Home != nil {
		home.X, home.Y, home.Z = int32(snap.Home.Lat*1e7), int32(snap.Home.Lon*1e7), float32(snap.Home.AltMSLM)
	}
	out := []*common.MessageMissionItemInt{home}

	missionFrame := common.MAV_FRAME_MISSION
	for i, it := range items {
		// Positional items use the item's altitude frame; others override it.
		frame, ok := altFrames[it.AltFrame]
		if !ok {
			return nil, fmt.Errorf("item %d: alt_frame must be relative, terrain or absolute", i)
		}
		mi := &common.MessageMissionItemInt{
			Seq: uint16(i + 1), Autocontinue: 1, MissionType: common.MAV_MISSION_TYPE_MISSION, Frame: frame,
		}
		var err error
		switch it.Type {
		case "takeoff":
			err = d.validAlt(it)
			mi.Command, mi.Z = common.MAV_CMD_NAV_TAKEOFF, val(it.AltM)
		case "waypoint":
			err = d.validPos(it, true)
			mi.Command = common.MAV_CMD_NAV_WAYPOINT
			mi.Param1, mi.Param2 = val(it.HoldS), val(it.AcceptRadiusM)
			mi.X, mi.Y, mi.Z = degE7(it.Lat), degE7(it.Lon), val(it.AltM)
		case "loiter_time":
			if it.Seconds == nil || *it.Seconds <= 0 {
				err = fmt.Errorf("loiter_time: seconds > 0 required")
			} else if it.Lat != nil || it.Lon != nil {
				err = d.validPos(it, true)
			}
			mi.Command, mi.Param1 = common.MAV_CMD_NAV_LOITER_TIME, val(it.Seconds)
			mi.X, mi.Y, mi.Z = degE7(it.Lat), degE7(it.Lon), val(it.AltM)
		case "land":
			if it.Lat != nil || it.Lon != nil {
				err = d.validPos(it, false)
			}
			mi.Command, mi.X, mi.Y = common.MAV_CMD_NAV_LAND, degE7(it.Lat), degE7(it.Lon)
		case "rtl":
			mi.Command = common.MAV_CMD_NAV_RETURN_TO_LAUNCH
		case "speed":
			if it.SpeedMS == nil || *it.SpeedMS <= 0 || *it.SpeedMS > d.gw.Limits.MaxSpeedMS {
				err = fmt.Errorf("speed: speed_m_s must be > 0 and <= %.0f", d.gw.Limits.MaxSpeedMS)
			}
			mi.Frame, mi.Command = missionFrame, common.MAV_CMD_DO_CHANGE_SPEED
			mi.Param1, mi.Param2, mi.Param3 = 1, val(it.SpeedMS), -1
		case "delay":
			if it.Seconds == nil || *it.Seconds < 0 {
				err = fmt.Errorf("delay: seconds >= 0 required")
			}
			mi.Frame, mi.Command, mi.Param1 = missionFrame, common.MAV_CMD_CONDITION_DELAY, val(it.Seconds)
		case "photo":
			mi.Frame, mi.Command, mi.Param3 = missionFrame, common.MAV_CMD_IMAGE_START_CAPTURE, 1
		case "photo_interval":
			if it.Seconds == nil || *it.Seconds <= 0 {
				err = fmt.Errorf("photo_interval: seconds > 0 required")
			}
			mi.Frame, mi.Command, mi.Param2 = missionFrame, common.MAV_CMD_IMAGE_START_CAPTURE, val(it.Seconds)
			if it.Count != nil {
				mi.Param3 = float32(*it.Count)
			}
		case "photo_stop":
			mi.Frame, mi.Command = missionFrame, common.MAV_CMD_IMAGE_STOP_CAPTURE
		case "video_start":
			mi.Frame, mi.Command = missionFrame, common.MAV_CMD_VIDEO_START_CAPTURE
		case "video_stop":
			mi.Frame, mi.Command = missionFrame, common.MAV_CMD_VIDEO_STOP_CAPTURE
		case "roi":
			err = d.validPos(it, true)
			mi.Command = common.MAV_CMD_DO_SET_ROI_LOCATION
			mi.X, mi.Y, mi.Z = degE7(it.Lat), degE7(it.Lon), val(it.AltM)
		case "roi_none":
			mi.Frame, mi.Command = missionFrame, common.MAV_CMD_DO_SET_ROI_NONE
		case "raw":
			if it.Command == nil {
				err = fmt.Errorf("raw: command is required")
				break
			}
			mi.Command = common.MAV_CMD(*it.Command)
			if it.Frame != nil {
				mi.Frame = common.MAV_FRAME(*it.Frame)
			}
			if it.Params != nil {
				mi.Param1, mi.Param2, mi.Param3, mi.Param4 = it.Params[0], it.Params[1], it.Params[2], it.Params[3]
			}
			if it.X != nil {
				mi.X = *it.X
			}
			if it.Y != nil {
				mi.Y = *it.Y
			}
			if it.Z != nil {
				mi.Z = *it.Z
			}
		default:
			err = fmt.Errorf("unknown item type %q", it.Type)
		}
		if err != nil {
			return nil, fmt.Errorf("item %d: %w", i, err)
		}
		out = append(out, mi)
	}
	return out, nil
}

func (d *Drone) missionFilter(sys uint8) func(*gomavlib.EventFrame) bool {
	return func(fr *gomavlib.EventFrame) bool {
		if fr.SystemID() != sys || fr.ComponentID() != mav.CompAutopilot {
			return false
		}
		switch m := fr.Message().(type) {
		case *common.MessageMissionRequestInt:
			return m.MissionType == common.MAV_MISSION_TYPE_MISSION && d.addressedToUs(m.TargetSystem, m.TargetComponent)
		case *common.MessageMissionRequest:
			return m.MissionType == common.MAV_MISSION_TYPE_MISSION && d.addressedToUs(m.TargetSystem, m.TargetComponent)
		case *common.MessageMissionAck:
			return m.MissionType == common.MAV_MISSION_TYPE_MISSION && d.addressedToUs(m.TargetSystem, m.TargetComponent)
		case *common.MessageMissionCount:
			return m.MissionType == common.MAV_MISSION_TYPE_MISSION && d.addressedToUs(m.TargetSystem, m.TargetComponent)
		case *common.MessageMissionItemInt:
			return m.MissionType == common.MAV_MISSION_TYPE_MISSION && d.addressedToUs(m.TargetSystem, m.TargetComponent)
		}
		return false
	}
}

// UploadMission runs the MAVLink mission upload handshake:
// MISSION_COUNT → (MISSION_REQUEST_INT → MISSION_ITEM_INT)* → MISSION_ACK.
func (d *Drone) UploadMission(ctx context.Context, items []*common.MessageMissionItemInt) error {
	sys, err := d.targetSystem()
	if err != nil {
		return err
	}
	d.missionMu.Lock()
	defer d.missionMu.Unlock()

	ch, cancel := d.subscribe(d.missionFilter(sys))
	defer cancel()

	n := uint16(len(items))
	var last message.Message = &common.MessageMissionCount{
		TargetSystem: sys, TargetComponent: mav.CompAutopilot, Count: n, MissionType: common.MAV_MISSION_TYPE_MISSION,
	}
	if err := d.write(last); err != nil {
		return err
	}
	sent := map[uint16]bool{}
	retries := 0
	timer := time.NewTimer(missionStepTimeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			if retries++; retries > missionRetries {
				return fmt.Errorf("mission upload: %w (got %d/%d item requests)", ErrTimeout, len(sent), n)
			}
			if err := d.write(last); err != nil {
				return err
			}
			timer.Reset(missionStepTimeout)
		case fr := <-ch:
			var seq uint16
			switch m := fr.Message().(type) {
			case *common.MessageMissionRequestInt:
				seq = m.Seq
			case *common.MessageMissionRequest:
				seq = m.Seq
			case *common.MessageMissionAck:
				if m.Type != common.MAV_MISSION_ACCEPTED {
					return &MissionError{Result: m.Type}
				}
				if len(sent) < int(n) {
					continue // stale ACK from an earlier transaction
				}
				d.log.Info("mission uploaded", "items", n)
				return nil
			default:
				continue
			}
			if seq >= n {
				return fmt.Errorf("mission upload: vehicle requested item %d of %d", seq, n)
			}
			item := *items[seq]
			item.TargetSystem, item.TargetComponent = sys, mav.CompAutopilot
			last = &item
			sent[seq] = true
			retries = 0
			if err := d.write(last); err != nil {
				return err
			}
			timer.Reset(missionStepTimeout)
		}
	}
}

// DownloadMission reads the mission currently stored on the vehicle.
func (d *Drone) DownloadMission(ctx context.Context) ([]*common.MessageMissionItemInt, error) {
	sys, err := d.targetSystem()
	if err != nil {
		return nil, err
	}
	d.missionMu.Lock()
	defer d.missionMu.Unlock()

	ch, cancel := d.subscribe(d.missionFilter(sys))
	defer cancel()

	await := func(req message.Message, want func(message.Message) bool) (message.Message, error) {
		for attempt := 0; attempt <= missionRetries; attempt++ {
			if err := d.write(req); err != nil {
				return nil, err
			}
			timer := time.NewTimer(missionStepTimeout)
			for {
				select {
				case <-ctx.Done():
					timer.Stop()
					return nil, ctx.Err()
				case <-timer.C:
				case fr := <-ch:
					if ack, ok := fr.Message().(*common.MessageMissionAck); ok && ack.Type != common.MAV_MISSION_ACCEPTED {
						timer.Stop()
						return nil, &MissionError{Result: ack.Type}
					}
					if want(fr.Message()) {
						timer.Stop()
						return fr.Message(), nil
					}
					continue
				}
				break
			}
		}
		return nil, fmt.Errorf("mission download: %w", ErrTimeout)
	}

	m, err := await(&common.MessageMissionRequestList{
		TargetSystem: sys, TargetComponent: mav.CompAutopilot, MissionType: common.MAV_MISSION_TYPE_MISSION,
	}, func(m message.Message) bool { _, ok := m.(*common.MessageMissionCount); return ok })
	if err != nil {
		return nil, err
	}
	count := m.(*common.MessageMissionCount).Count
	items := make([]*common.MessageMissionItemInt, 0, count)
	for seq := uint16(0); seq < count; seq++ {
		s := seq
		m, err := await(&common.MessageMissionRequestInt{
			TargetSystem: sys, TargetComponent: mav.CompAutopilot, Seq: s, MissionType: common.MAV_MISSION_TYPE_MISSION,
		}, func(m message.Message) bool { it, ok := m.(*common.MessageMissionItemInt); return ok && it.Seq == s })
		if err != nil {
			return nil, err
		}
		items = append(items, m.(*common.MessageMissionItemInt))
	}
	_ = d.write(&common.MessageMissionAck{
		TargetSystem: sys, TargetComponent: mav.CompAutopilot, Type: common.MAV_MISSION_ACCEPTED,
		MissionType: common.MAV_MISSION_TYPE_MISSION,
	})
	return items, nil
}

// ClearMission deletes the mission on the vehicle.
func (d *Drone) ClearMission(ctx context.Context) error {
	sys, err := d.targetSystem()
	if err != nil {
		return err
	}
	d.missionMu.Lock()
	defer d.missionMu.Unlock()
	ch, cancel := d.subscribe(d.missionFilter(sys))
	defer cancel()
	req := &common.MessageMissionClearAll{TargetSystem: sys, TargetComponent: mav.CompAutopilot, MissionType: common.MAV_MISSION_TYPE_MISSION}
	for attempt := 0; attempt <= missionRetries; attempt++ {
		if err := d.write(req); err != nil {
			return err
		}
		timer := time.NewTimer(missionStepTimeout)
	wait:
		for {
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
				break wait
			case fr := <-ch:
				if ack, ok := fr.Message().(*common.MessageMissionAck); ok {
					timer.Stop()
					if ack.Type != common.MAV_MISSION_ACCEPTED {
						return &MissionError{Result: ack.Type}
					}
					return nil
				}
			}
		}
	}
	return fmt.Errorf("mission clear: %w", ErrTimeout)
}

// DescribeMission converts downloaded items back to the API representation.
// Item 0 (home) is omitted.
func DescribeMission(items []*common.MessageMissionItemInt) []MissionItem {
	out := make([]MissionItem, 0, len(items))
	for _, m := range items {
		if m.Seq == 0 {
			continue
		}
		lat, lon, alt := float64(m.X)/1e7, float64(m.Y)/1e7, float64(m.Z)
		p1, p2 := float64(m.Param1), float64(m.Param2)
		it := MissionItem{Seq: int(m.Seq)}
		pos := func() { it.Lat, it.Lon, it.AltM = &lat, &lon, &alt }
		cmd := m.Command
		frameName, frameOK := altFrameName(m.Frame)
		switch cmd {
		case common.MAV_CMD_NAV_TAKEOFF, common.MAV_CMD_NAV_WAYPOINT, common.MAV_CMD_NAV_LOITER_TIME,
			common.MAV_CMD_NAV_LAND, common.MAV_CMD_DO_SET_ROI_LOCATION:
			if frameOK {
				it.AltFrame = frameName
			} else {
				cmd = 0 // a frame the typed items can't express: keep it as raw
			}
		}
		switch cmd {
		case common.MAV_CMD_NAV_TAKEOFF:
			it.Type, it.AltM = "takeoff", &alt
		case common.MAV_CMD_NAV_WAYPOINT:
			it.Type = "waypoint"
			pos()
			if p1 > 0 {
				it.HoldS = &p1
			}
			if p2 > 0 {
				it.AcceptRadiusM = &p2
			}
		case common.MAV_CMD_NAV_LOITER_TIME:
			it.Type, it.Seconds = "loiter_time", &p1
			if m.X != 0 || m.Y != 0 {
				pos()
			}
		case common.MAV_CMD_NAV_LAND:
			it.Type = "land"
			if m.X != 0 || m.Y != 0 {
				it.Lat, it.Lon = &lat, &lon
			}
		case common.MAV_CMD_NAV_RETURN_TO_LAUNCH:
			it.Type = "rtl"
		case common.MAV_CMD_DO_CHANGE_SPEED:
			it.Type, it.SpeedMS = "speed", &p2
		case common.MAV_CMD_CONDITION_DELAY:
			it.Type, it.Seconds = "delay", &p1
		case common.MAV_CMD_IMAGE_START_CAPTURE:
			if m.Param3 == 1 {
				it.Type = "photo"
			} else {
				c := int(m.Param3)
				it.Type, it.Seconds, it.Count = "photo_interval", &p2, &c
			}
		case common.MAV_CMD_IMAGE_STOP_CAPTURE:
			it.Type = "photo_stop"
		case common.MAV_CMD_VIDEO_START_CAPTURE:
			it.Type = "video_start"
		case common.MAV_CMD_VIDEO_STOP_CAPTURE:
			it.Type = "video_stop"
		case common.MAV_CMD_DO_SET_ROI_LOCATION:
			it.Type = "roi"
			pos()
		case common.MAV_CMD_DO_SET_ROI_NONE:
			it.Type = "roi_none"
		default:
			cmd, fr := uint16(m.Command), uint8(m.Frame)
			params := [4]float32{m.Param1, m.Param2, m.Param3, m.Param4}
			x, y, z := m.X, m.Y, m.Z
			it.Type, it.Command, it.Frame, it.Params, it.X, it.Y, it.Z = "raw", &cmd, &fr, &params, &x, &y, &z
		}
		out = append(out, it)
	}
	return out
}
