# 02 — gimbal-bridge

**Code:**
- [`cmd/gimbal-bridge/main.go`](../drone/cmd/gimbal-bridge/main.go)
- [`drone/bridge/`](../drone/bridge/)
- [`drone/skydroid/`](../drone/skydroid/)

## What it does

gimbal-bridge joins the drone's MAVLink network as two components belonging to the vehicle's system ID:

- **Camera, component 100.** It answers the MAVLink Camera Protocol.
- **Gimbal, component 154.** It acts as a Gimbal Protocol v2 gimbal manager.

Each command it accepts is translated into Skydroid C12 UDP packets and answered with `COMMAND_ACK`. It keeps no state that matters across restarts.

The system ID is learned from the autopilot's heartbeat, so no per-drone configuration is needed. Until the autopilot has been seen, the bridge stays silent.

## Commands it understands

### Camera (component 100)

| MAVLink | C12 action |
|---|---|
| `MAV_CMD_IMAGE_START_CAPTURE`, count 1 | Photo |
| `MAV_CMD_IMAGE_START_CAPTURE`, interval > 0 | Photo every N seconds, for `count` photos (0 = until stopped) |
| `MAV_CMD_IMAGE_STOP_CAPTURE` | Stop interval capture |
| `MAV_CMD_DO_DIGICAM_CONTROL`, shoot = 1 | Photo (legacy mission command) |
| `MAV_CMD_VIDEO_START_CAPTURE` / `STOP` | Start / stop recording on the camera's SD card |
| `MAV_CMD_SET_CAMERA_ZOOM`, `ZOOM_TYPE_STEP` | ±N zoom steps (at most 10 per command) |
| `MAV_CMD_SET_CAMERA_ZOOM`, `ZOOM_TYPE_CONTINUOUS` | Zoom in or out until 0 is sent, at most 10 s |
| `MAV_CMD_SET_CAMERA_ZOOM`, `ZOOM_TYPE_RANGE` | Absolute zoom, 0–100 % mapped to C12 levels 0–67 |
| `MAV_CMD_SET_CAMERA_MODE` | Accepted with no effect (the C12 can photograph and record in any mode) |
| `MAV_CMD_REQUEST_MESSAGE` | Replies with `CAMERA_INFORMATION`, `CAMERA_SETTINGS`, `CAMERA_CAPTURE_STATUS` or `VIDEO_STREAM_INFORMATION` (the RTSP URLs, so QGC can show video automatically) |

### Gimbal (component 154)

| MAVLink | C12 action |
|---|---|
| `MAV_CMD_DO_GIMBAL_MANAGER_PITCHYAW` with angles | Absolute pitch/yaw (`GAP`/`GAY`). Negative pitch is down. Clamped to ±90°. |
| … with rates | Speed motion, repeated at 10 Hz. Stops when rates are 0, or after the dead-man timeout. |
| … with flag `NEUTRAL` | Centre (PTZ 05) |
| … with flag `YAW_LOCK` set or cleared | Lock (PTZ 07) or follow (PTZ 06), sent only when it changes |
| `GIMBAL_MANAGER_SET_PITCHYAW` (message, no ACK) | Same as the command; used for streaming hold-to-move |
| `GIMBAL_MANAGER_SET_MANUAL_CONTROL` | Normalised rates (−1..1) × `max-rate` |
| `MAV_CMD_DO_MOUNT_CONTROL` | Legacy: pitch/yaw angles, or neutral |
| `MAV_CMD_REQUEST_MESSAGE` | Replies with `GIMBAL_MANAGER_INFORMATION` or `GIMBAL_MANAGER_STATUS` |

It also publishes `GIMBAL_MANAGER_STATUS` at 1 Hz, which is how the gateway shows the current lock state.

**Not supported:**
- **Retract.** The C12 has no stow position.
- **Thermal palette changes.** There is no standard MAVLink command for this; it can be added as a vendor extension.
- **Gimbal attitude feedback.** The C12's attitude-push format is undocumented, so the bridge does not publish `GIMBAL_DEVICE_ATTITUDE_STATUS`.

## The dead-man stop

A non-zero rate keeps the gimbal moving for `rate-timeout` (default 600 ms). Clients must resend at least every 300 ms (≥ 4 Hz is comfortable) for as long as the operator holds the control. If updates stop, the bridge sends a stop by itself.

The gateway's WebSocket also sends an explicit zero when a client disconnects. In the browser, releasing the control, switching window or tab, or leaving the page all count as a release.

Further protections:

- **Each stop is sent 4 times**, once immediately and then on the next 3 motion ticks, because a single UDP packet can be lost. The repeats are cancelled if a point-at-angle or centre command follows, so they cannot cut it short.
- **The idle axis is zeroed explicitly.** The C12 keeps each axis's last speed. When motion changes from yaw to pitch (or from both axes to one), the bridge sends 0 for the axis that should stop.
- **Packet order is guaranteed.** All speed and stop packets are sent under one lock, so a stop can never be overtaken by a motion packet that was decided before it.

## Wire-protocol fidelity

[`drone/skydroid`](../drone/skydroid/) is a direct port of the old `skydroid_protocol.py`. Its tests compare 31 command strings against output generated from the Python code, so the C12 receives byte-identical packets to what it receives today.

## Settings

Set each one as a flag, or as an environment variable in `/etc/drone-stack/gimbal-bridge.env`.

| Variable / flag | Default | Meaning |
|---|---|---|
| `MAVLINK` | `udp-server:127.0.0.1:14556` | Where mavlink-router sends to |
| `SYSID` | `0` | Vehicle system ID; 0 = learn it from the autopilot |
| `GIMBAL_HOST` / `GIMBAL_PORT` | `192.168.144.108` / `5000` | The C12 |
| `LOCAL_PORT` | `5000` (5001 during transition) | Local UDP port; the C12 replies here |
| `SPEED_ENCODING` | `legacy` | `legacy` = 0.1 °/s units, `apk` = 0.5 °/s units |
| `MOTION_RATE_HZ` | `10` | Speed packet repeat rate |
| `RATE_TIMEOUT` | `600ms` | Dead-man timeout |
| `MAX_RATE` | `30` | °/s at full-scale manual control |
| `VIDEO_VISIBLE` / `VIDEO_THERMAL` | install script fills these in | RTSP URLs advertised to ground stations |
| `LOG_LEVEL` | `info` | |

## Resource use

It is a single static binary of about 4.4 MB. Expected memory use is around 10 MB; the systemd unit caps it at 48 MB.
