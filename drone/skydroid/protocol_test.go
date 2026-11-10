package skydroid

import "testing"

// Golden strings generated from the original Python skydroid_protocol.py.
func TestGoldenAgainstPython(t *testing.T) {
	L := Encoder{Speed: SpeedLegacy}
	A := Encoder{Speed: SpeedAPK}
	mustZoom := func(l int) string {
		s, err := L.ZoomLevel(l)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	cases := map[string][2]string{
		"legacy_yaw_3":         {L.SpeedYaw(3), "#TPUG2wGSY1E75"},
		"legacy_yaw_-3":        {L.SpeedYaw(-3), "#TPUG2wGSYE276"},
		"legacy_pitch_12.5":    {L.SpeedPitch(12.5), "#TPUG2wGSP7D71"},
		"legacy_pitch_-30":     {L.SpeedPitch(-30), "#TPUG2wGSP815F"},
		"legacy_yawpitch_3_-2": {L.SpeedYawPitch(3, -2), "#TPUG4wGSM1EECF3"},
		"legacy_yaw_0":         {L.SpeedYaw(0), "#TPUG2wGSY005F"},
		"legacy_yaw_99":        {L.SpeedYaw(99), "#TPUG2wGSY7F7C"},
		"apk_yaw_3":            {A.SpeedYaw(3), "#TPUG2wGSY0665"},
		"apk_pitch_-3":         {A.SpeedPitch(-3), "#TPUG2wGSPFA7D"},
		"apk_yawpitch_10_5":    {A.SpeedYawPitch(10, 5), "#TPUG4wGSM140ACB"},
		"goto_yaw_30":          {L.GotoYaw(30), "#TPUG6wGAY0BB8103E"},
		"goto_pitch_-45":       {L.GotoPitch(-45), "#TPUG6wGAPEE6C104C"},
		"goto_pitch_-120":      {L.GotoPitch(-120), "#TPUG6wGAPDCD8104C"},
		"goto_yaw_12.34":       {L.GotoYaw(12.34), "#TPUG6wGAY04D2102C"},
		"ptz_center":           {L.PTZ(PTZCenter), "#TPUG2wPTZ056F"},
		"ptz_follow":           {L.PTZ(PTZFollow), "#TPUG2wPTZ0670"},
		"ptz_lock":             {L.PTZ(PTZLock), "#TPUG2wPTZ0771"},
		"ptz_stop":             {L.PTZ(PTZStop), "#TPUG2wPTZ006A"},
		"ptz_down":             {L.PTZ(PTZDown), "#TPUG2wPTZ026C"},
		"photo":                {L.TakePicture(), "#TPUD2wCAP013E"},
		"rec_start":            {L.StartRecord(), "#TPUD2wREC0144"},
		"rec_stop":             {L.StopRecord(), "#TPUD2wREC0043"},
		"zoom_in":              {L.ZoomIn(), "#TPUD2wDZM0A65"},
		"zoom_out":             {L.ZoomOut(), "#TPUD2wDZM0B66"},
		"zoom_level_0":         {mustZoom(0), "#TPUD2wDZM0054"},
		"zoom_level_10":        {mustZoom(10), "#TPUD2wDZM01085"},
		"zoom_level_67":        {mustZoom(67), "#TPUD2wDZM06791"},
		"palette_ironbow":      {L.ThermalPalette(PaletteIronbow), "#TPUD2wIMG044A"},
		"palette_black_hot":    {L.ThermalPalette(PaletteBlackHot), "#TPUD2wIMG0B58"},
		"query_record":         {L.RecordStateQuery(), "#TPUD2rREC003E"},
		"query_version":        {L.VersionQuery(), "#TPUD2rVER0051"},
	}
	for name, c := range cases {
		if c[0] != c[1] {
			t.Errorf("%s: got %q want %q", name, c[0], c[1])
		}
	}
}

func TestZoomLevelRange(t *testing.T) {
	if _, err := (Encoder{}).ZoomLevel(68); err == nil {
		t.Fatal("expected error for level 68")
	}
	if _, err := (Encoder{}).ZoomLevel(-1); err == nil {
		t.Fatal("expected error for level -1")
	}
}
