// Package skydroid implements the Skydroid C12 gimbal/camera UDP protocol.
//
// The command formats are a byte-for-byte port of skydroid_protocol.py from the
// original drone-agent (itself derived from the SkydroidCameraFPV APK). The
// golden tests in protocol_test.go were generated from that Python code.
//
// Wire format: ASCII "#TPUG..." (gimbal) or "#TPUD..." (camera) followed by a
// two-character uppercase hex checksum: sum of all bytes & 0xFF.
package skydroid

import (
	"fmt"
	"math"
)

// SpeedEncoding selects how deg/s maps to the signed 8-bit speed field.
type SpeedEncoding string

const (
	// SpeedLegacy sends 0.1 deg/s units (speed * 10). Used on the fleet today.
	SpeedLegacy SpeedEncoding = "legacy"
	// SpeedAPK sends 0.5 deg/s units (speed / 0.5), as the Skydroid FPV app does.
	SpeedAPK SpeedEncoding = "apk"
)

// ParseSpeedEncoding validates a speed encoding name.
func ParseSpeedEncoding(s string) (SpeedEncoding, error) {
	switch SpeedEncoding(s) {
	case SpeedLegacy, SpeedAPK:
		return SpeedEncoding(s), nil
	}
	return "", fmt.Errorf("unknown speed encoding %q (use legacy or apk)", s)
}

// ThermalPalette codes accepted by the IMG command.
type ThermalPalette string

const (
	PaletteWhiteHot ThermalPalette = "01"
	PaletteSepia    ThermalPalette = "03"
	PaletteIronbow  ThermalPalette = "04"
	PaletteRainbow  ThermalPalette = "05"
	PaletteNight    ThermalPalette = "06"
	PaletteAurora   ThermalPalette = "07"
	PaletteRedHot   ThermalPalette = "08"
	PaletteJungle   ThermalPalette = "09"
	PaletteMedical  ThermalPalette = "0A"
	PaletteBlackHot ThermalPalette = "0B"
	PaletteGloryHot ThermalPalette = "0C"
)

// Palettes maps lowercase palette names to codes.
var Palettes = map[string]ThermalPalette{
	"white_hot": PaletteWhiteHot, "sepia": PaletteSepia, "ironbow": PaletteIronbow,
	"rainbow": PaletteRainbow, "night": PaletteNight, "aurora": PaletteAurora,
	"red_hot": PaletteRedHot, "jungle": PaletteJungle, "medical": PaletteMedical,
	"black_hot": PaletteBlackHot, "glory_hot": PaletteGloryHot,
}

// PTZ key codes for the PTZ command.
const (
	PTZStop   = "00"
	PTZUp     = "01"
	PTZDown   = "02"
	PTZLeft   = "03"
	PTZRight  = "04"
	PTZCenter = "05"
	PTZFollow = "06"
	PTZLock   = "07"
)

// Limits from SkydroidGimbalControlCore.
const (
	speedMin       = -127
	speedMax       = 127
	angleMinCentiD = -9000
	angleMaxCentiD = 9000
	gotoMoveSpeed  = "10"
	// ZoomLevelMax is the highest absolute zoom level the C12 accepts.
	ZoomLevelMax = 67
)

const hexChars = "0123456789ABCDEF"

// roundHalfEven matches Python's round() so outputs are identical to the
// original implementation.
func roundHalfEven(v float64) int { return int(math.RoundToEven(v)) }

func byteHex(v int) string {
	u := v & 0xFF
	return string([]byte{hexChars[u>>4], hexChars[u&0xF]})
}

func shortHex(v int) string {
	u := v & 0xFFFF
	return string([]byte{hexChars[(u>>12)&0xF], hexChars[(u>>8)&0xF], hexChars[(u>>4)&0xF], hexChars[u&0xF]})
}

// AddCRC appends the checksum (sum of ASCII bytes & 0xFF, as 2 hex chars).
func AddCRC(cmd string) string {
	sum := 0
	for i := 0; i < len(cmd); i++ {
		sum += int(cmd[i])
	}
	return cmd + byteHex(sum)
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func speedHex(degPerS float64, enc SpeedEncoding) string {
	var v int
	if enc == SpeedAPK {
		v = roundHalfEven(degPerS / 0.5)
	} else {
		v = roundHalfEven(degPerS * 10)
	}
	return byteHex(clamp(v, speedMin, speedMax))
}

func angleHex(deg float64) string {
	return shortHex(clamp(roundHalfEven(deg*100), angleMinCentiD, angleMaxCentiD))
}

// Encoder builds command strings for one speed encoding.
type Encoder struct{ Speed SpeedEncoding }

// SpeedYaw: positive = right.
func (e Encoder) SpeedYaw(dps float64) string { return AddCRC("#TPUG2wGSY" + speedHex(dps, e.Speed)) }

// SpeedPitch: positive = up.
func (e Encoder) SpeedPitch(dps float64) string { return AddCRC("#TPUG2wGSP" + speedHex(dps, e.Speed)) }

// SpeedYawPitch moves both axes in one packet.
func (e Encoder) SpeedYawPitch(yawDps, pitchDps float64) string {
	return AddCRC("#TPUG4wGSM" + speedHex(yawDps, e.Speed) + speedHex(pitchDps, e.Speed))
}

// Stop returns the packets that zero both axes (the protocol has no stop).
func (e Encoder) Stop() []string { return []string{e.SpeedYaw(0), e.SpeedPitch(0)} }

// GotoYaw moves to an absolute yaw angle in degrees (clamped to ±90).
func (Encoder) GotoYaw(deg float64) string {
	return AddCRC("#TPUG6wGAY" + angleHex(deg) + gotoMoveSpeed)
}

// GotoPitch moves to an absolute pitch angle in degrees (negative = down).
func (Encoder) GotoPitch(deg float64) string {
	return AddCRC("#TPUG6wGAP" + angleHex(deg) + gotoMoveSpeed)
}

// PTZ sends a PTZ key (see PTZ* constants).
func (Encoder) PTZ(code string) string { return AddCRC("#TPUG2wPTZ" + code) }

// TakePicture triggers a still capture.
func (Encoder) TakePicture() string { return AddCRC("#TPUD2wCAP01") }

// StartRecord starts video recording on the camera's SD card.
func (Encoder) StartRecord() string { return AddCRC("#TPUD2wREC01") }

// StopRecord stops video recording.
func (Encoder) StopRecord() string { return AddCRC("#TPUD2wREC00") }

// ZoomIn is one zoom step in.
func (Encoder) ZoomIn() string { return AddCRC("#TPUD2wDZM0A") }

// ZoomOut is one zoom step out.
func (Encoder) ZoomOut() string { return AddCRC("#TPUD2wDZM0B") }

// ZoomLevel sets absolute zoom 0..ZoomLevelMax.
func (Encoder) ZoomLevel(level int) (string, error) {
	if level < 0 || level > ZoomLevelMax {
		return "", fmt.Errorf("zoom level must be 0-%d", ZoomLevelMax)
	}
	return AddCRC(fmt.Sprintf("#TPUD2wDZM0%d", level)), nil
}

// ThermalPalette selects the thermal colour palette.
func (Encoder) ThermalPalette(p ThermalPalette) string { return AddCRC("#TPUD2wIMG" + string(p)) }

// RecordStateQuery asks whether the camera is recording.
func (Encoder) RecordStateQuery() string { return AddCRC("#TPUD2rREC00") }

// VersionQuery asks for the camera firmware version.
func (Encoder) VersionQuery() string { return AddCRC("#TPUD2rVER00") }
