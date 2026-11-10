package bridge

import (
	"math"
	"time"

	"github.com/bluenviron/gomavlib/v3/pkg/dialects/common"

	"github.com/parthibx24/Drone-Feed/drone/skydroid"
	"github.com/parthibx24/Drone-Feed/internal/mav"
)

const (
	maxZoomSteps       = 10
	zoomStepGap        = 150 * time.Millisecond
	continuousZoomTick = 300 * time.Millisecond
	continuousZoomMax  = 10 * time.Second
)

func (b *Bridge) cameraCommand(cmd common.MAV_CMD, p [7]float32) common.MAV_RESULT {
	switch cmd {
	case common.MAV_CMD_REQUEST_MESSAGE:
		return b.cameraRequest(uint32(p[0]), uint8(p[1]))
	case common.MAV_CMD_REQUEST_CAMERA_INFORMATION:
		return b.cameraRequest(259, 0)
	case common.MAV_CMD_REQUEST_CAMERA_SETTINGS:
		return b.cameraRequest(260, 0)
	case common.MAV_CMD_REQUEST_CAMERA_CAPTURE_STATUS:
		return b.cameraRequest(262, 0)
	case common.MAV_CMD_REQUEST_VIDEO_STREAM_INFORMATION:
		return b.cameraRequest(269, uint8(p[0]))

	case common.MAV_CMD_SET_CAMERA_MODE:
		return common.MAV_RESULT_ACCEPTED // the C12 photographs and records in any mode

	case common.MAV_CMD_IMAGE_START_CAPTURE: // p2 interval s, p3 count (0 = until stopped)
		interval, count := float64(p[1]), int(p[2])
		if count == 1 || interval <= 0 {
			return b.takePhoto()
		}
		b.startInterval(time.Duration(interval*float64(time.Second)), count)
		return common.MAV_RESULT_ACCEPTED

	case common.MAV_CMD_IMAGE_STOP_CAPTURE:
		b.stopInterval()
		return common.MAV_RESULT_ACCEPTED

	case common.MAV_CMD_DO_DIGICAM_CONTROL: // legacy mission command; p5 = 1 shoots
		if p[4] == 1 {
			return b.takePhoto()
		}
		return common.MAV_RESULT_ACCEPTED

	case common.MAV_CMD_VIDEO_START_CAPTURE:
		if err := b.gimbal.SendCRLF(b.gimbal.StartRecord()); err != nil {
			return b.logSendErr("record start", err)
		}
		b.camMu.Lock()
		if !b.recording {
			b.recording, b.recordStart = true, time.Now()
		}
		b.camMu.Unlock()
		return common.MAV_RESULT_ACCEPTED

	case common.MAV_CMD_VIDEO_STOP_CAPTURE:
		if err := b.gimbal.SendCRLF(b.gimbal.StopRecord()); err != nil {
			return b.logSendErr("record stop", err)
		}
		b.camMu.Lock()
		b.recording = false
		b.camMu.Unlock()
		return common.MAV_RESULT_ACCEPTED

	case common.MAV_CMD_SET_CAMERA_ZOOM: // p1 zoom type, p2 value
		return b.zoom(common.CAMERA_ZOOM_TYPE(uint32(p[0])), float64(p[1]))
	}
	return common.MAV_RESULT_UNSUPPORTED
}

func (b *Bridge) takePhoto() common.MAV_RESULT {
	if err := b.gimbal.SendCRLF(b.gimbal.TakePicture()); err != nil {
		return b.logSendErr("photo", err)
	}
	b.camMu.Lock()
	b.imageCount++
	b.camMu.Unlock()
	return common.MAV_RESULT_ACCEPTED
}

func (b *Bridge) startInterval(every time.Duration, count int) {
	b.stopInterval()
	stop := make(chan struct{})
	b.camMu.Lock()
	b.intervalStop = stop
	b.camMu.Unlock()
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for n := 0; count == 0 || n < count; n++ {
			b.takePhoto()
			select {
			case <-stop:
				return
			case <-t.C:
			}
		}
	}()
}

func (b *Bridge) stopInterval() {
	b.camMu.Lock()
	if b.intervalStop != nil {
		close(b.intervalStop)
		b.intervalStop = nil
	}
	b.camMu.Unlock()
}

func (b *Bridge) zoom(kind common.CAMERA_ZOOM_TYPE, v float64) common.MAV_RESULT {
	switch kind {
	case common.ZOOM_TYPE_STEP:
		n := int(math.Round(v))
		if n == 0 {
			return common.MAV_RESULT_ACCEPTED
		}
		cmd := b.gimbal.ZoomIn()
		if n < 0 {
			cmd, n = b.gimbal.ZoomOut(), -n
		}
		if n > maxZoomSteps {
			n = maxZoomSteps
		}
		go func() {
			for i := 0; i < n; i++ {
				b.logSendErr("zoom step", b.gimbal.Send(cmd))
				time.Sleep(zoomStepGap)
			}
		}()
		b.setZoomPct(float32(math.NaN()))
		return common.MAV_RESULT_ACCEPTED

	case common.ZOOM_TYPE_CONTINUOUS:
		b.stopContinuousZoom()
		if v == 0 {
			return common.MAV_RESULT_ACCEPTED
		}
		cmd := b.gimbal.ZoomIn()
		if v < 0 {
			cmd = b.gimbal.ZoomOut()
		}
		stop := make(chan struct{})
		b.camMu.Lock()
		b.zoomStop = stop
		b.camMu.Unlock()
		go func() {
			t := time.NewTicker(continuousZoomTick)
			defer t.Stop()
			deadline := time.After(continuousZoomMax)
			for {
				b.logSendErr("zoom continuous", b.gimbal.Send(cmd))
				select {
				case <-stop:
					return
				case <-deadline:
					return
				case <-t.C:
				}
			}
		}()
		b.setZoomPct(float32(math.NaN()))
		return common.MAV_RESULT_ACCEPTED

	case common.ZOOM_TYPE_RANGE: // 0..100 %
		if v < 0 || v > 100 {
			return common.MAV_RESULT_DENIED
		}
		level := int(math.Round(v / 100 * skydroid.ZoomLevelMax))
		cmd, err := b.gimbal.ZoomLevel(level)
		if err != nil {
			return common.MAV_RESULT_DENIED
		}
		if err := b.gimbal.SendCRLF(cmd); err != nil {
			return b.logSendErr("zoom level", err)
		}
		b.setZoomPct(float32(v))
		return common.MAV_RESULT_ACCEPTED
	}
	return common.MAV_RESULT_UNSUPPORTED
}

func (b *Bridge) stopContinuousZoom() {
	b.camMu.Lock()
	if b.zoomStop != nil {
		close(b.zoomStop)
		b.zoomStop = nil
	}
	b.camMu.Unlock()
}

func (b *Bridge) setZoomPct(v float32) {
	b.camMu.Lock()
	b.zoomPct = v
	b.camMu.Unlock()
}

// cameraRequest answers MAV_CMD_REQUEST_MESSAGE for camera messages.
func (b *Bridge) cameraRequest(msgID uint32, streamID uint8) common.MAV_RESULT {
	switch msgID {
	case 259: // CAMERA_INFORMATION
		info := &common.MessageCameraInformation{
			TimeBootMs: b.bootMs(),
			Flags: common.CAMERA_CAP_FLAGS_CAPTURE_IMAGE | common.CAMERA_CAP_FLAGS_CAPTURE_VIDEO |
				common.CAMERA_CAP_FLAGS_CAN_CAPTURE_IMAGE_IN_VIDEO_MODE |
				common.CAMERA_CAP_FLAGS_CAN_CAPTURE_VIDEO_IN_IMAGE_MODE |
				common.CAMERA_CAP_FLAGS_HAS_BASIC_ZOOM,
			GimbalDeviceId: mav.CompGimbal,
		}
		copy(info.VendorName[:], "Skydroid")
		copy(info.ModelName[:], "C12")
		if len(b.cfg.Streams) > 0 {
			info.Flags |= common.CAMERA_CAP_FLAGS_HAS_VIDEO_STREAM
		}
		b.send(mav.CompCamera, info)

	case 260: // CAMERA_SETTINGS
		b.camMu.Lock()
		mode := common.CAMERA_MODE_IMAGE
		if b.recording {
			mode = common.CAMERA_MODE_VIDEO
		}
		z := b.zoomPct
		b.camMu.Unlock()
		b.send(mav.CompCamera, &common.MessageCameraSettings{
			TimeBootMs: b.bootMs(), ModeId: mode, Zoomlevel: z, Focuslevel: mav.NaN,
		})

	case 262: // CAMERA_CAPTURE_STATUS
		b.camMu.Lock()
		st := &common.MessageCameraCaptureStatus{TimeBootMs: b.bootMs(), ImageCount: b.imageCount, AvailableCapacity: mav.NaN}
		if b.recording {
			st.VideoStatus = 1
			st.RecordingTimeMs = uint32(time.Since(b.recordStart).Milliseconds())
		}
		if b.intervalStop != nil {
			st.ImageStatus = 3 // interval capture in progress
		}
		b.camMu.Unlock()
		b.send(mav.CompCamera, st)

	case 269: // VIDEO_STREAM_INFORMATION (streamID 0 = all)
		if len(b.cfg.Streams) == 0 {
			return common.MAV_RESULT_UNSUPPORTED
		}
		for i, s := range b.cfg.Streams {
			id := uint8(i + 1)
			if streamID != 0 && streamID != id {
				continue
			}
			flags := common.VIDEO_STREAM_STATUS_FLAGS_RUNNING
			if s.Thermal {
				flags |= common.VIDEO_STREAM_STATUS_FLAGS_THERMAL
			}
			b.send(mav.CompCamera, &common.MessageVideoStreamInformation{
				StreamId: id, Count: uint8(len(b.cfg.Streams)), Type: common.VIDEO_STREAM_TYPE_RTSP,
				Flags: flags, Name: s.Name, Uri: s.URI, Encoding: common.VIDEO_STREAM_ENCODING_H265,
			})
		}

	default:
		return common.MAV_RESULT_UNSUPPORTED
	}
	return common.MAV_RESULT_ACCEPTED
}
