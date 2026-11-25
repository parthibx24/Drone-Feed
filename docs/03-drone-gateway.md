# 03 — drone-gateway

**Code:**
- [`server/cmd/drone-gateway/main.go`](../server/cmd/drone-gateway/main.go)
- [`server/gateway/`](../server/gateway/)
- [`server/recorder/`](../server/recorder/)

**Configuration:** [`deploy/server/gateway.example.yaml`](../deploy/server/gateway.example.yaml)

## Responsibilities

| Area | File | What it does |
|---|---|---|
| Link | [`drone.go`](../server/gateway/drone.go) | One MAVLink node per drone. Sends the 1 Hz GCS heartbeat, identifies the autopilot, tracks online/offline, parses telemetry, and requests stream rates on every (re)connect. |
| Commands | [`command.go`](../server/gateway/command.go) | `COMMAND_LONG` / `COMMAND_INT`, waits for `COMMAND_ACK`, retries up to 3 times, and handles `IN_PROGRESS`. |
| Actions | [`actions.go`](../server/gateway/actions.go) | Arm, mode, takeoff, land, RTL, goto, speed, pause/resume, ROI, reboot, gimbal, camera. Checks preconditions and limits first. |
| Missions | [`mission.go`](../server/gateway/mission.go) | Validates and builds missions; upload, download and clear handshakes. |
| Parameters | [`params.go`](../server/gateway/params.go) | Full list with gap recovery, single read, write with echo check. |
| API | [`server.go`](../server/gateway/server.go), [`ws.go`](../server/gateway/ws.go) | REST, WebSocket, roles, CORS, audit log. |
| Login | [`auth.go`](../server/gateway/auth.go) | Users with bcrypt passwords, HMAC-signed session cookies, CSRF header check, login rate limit. |
| Web UI | [`web.go`](../server/gateway/web.go) | Serves the built React app from `web_dir`, with fallback for client-side routes. |
| Video and recordings | [`media.go`](../server/gateway/media.go), [`recorder/`](../server/recorder/) | Camera discovery in go2rtc, and server-side ffmpeg recording (port of the old `recorder.py`). Recording goes to MKV, which survives a crash; a finished recording is then copied into a faststart MP4 for the browser ([`webcopy.go`](../server/recorder/webcopy.go)), one file at a time. At start-up the gateway queues every older MKV the same way, newest first. If a copy fails or doesn't match (track count, length within 2 % or 1 s), the MKV is kept. |
| Config | [`config.go`](../server/gateway/config.go) | YAML with `${ENV}` expansion; defaults and validation. |

## How a command is executed

1. **Precondition check** (for example "must be armed to take off", altitude ≤ `max_alt_m`). Failure → HTTP 422; nothing is sent.
2. Is the drone online (autopilot heartbeat within 5 s, the same as ArduPilot's `FS_GCS_TIMEOUT` default)? If not → HTTP 503.
3. Send the command and wait 1.5 s for the matching `COMMAND_ACK`.
4. No ACK → resend with the confirmation counter incremented. After 3 attempts → HTTP 504.
5. ACK `IN_PROGRESS` → keep waiting, up to 10 s.
6. ACK `ACCEPTED` → HTTP 200. Any other result → HTTP 409 with `"result": "DENIED"`, `"FAILED"`, and so on.

Commands to one drone are serialised, so an ACK can never be credited to the wrong request.

## How a mission upload works

1. The API items are validated: coordinates, `alt_m` ≤ `max_alt_m`, speed ≤ `max_speed_m_s`, item count ≤ `max_mission_items`. Lat/lon 0,0 is rejected as an obvious mistake.
2. Item 0 is a home placeholder (ArduPilot overwrites it). Your items start at sequence 1.
3. The upload follows the MAVLink handshake:
   - send `MISSION_COUNT`,
   - answer each `MISSION_REQUEST_INT` (or the legacy `MISSION_REQUEST`) with that `MISSION_ITEM_INT`,
   - finish on `MISSION_ACK`.

   Any step that goes unanswered for 1.5 s is resent, up to 5 times.
4. A non-accepted `MISSION_ACK` is returned as HTTP 409 with the reason, for example `MISSION_NO_SPACE`.

Download (`GET .../mission`) reverses this and translates items back into the same JSON types.

### Mission item types

| Type | Fields | MAVLink command |
|---|---|---|
| `takeoff` | `alt_m` | `NAV_TAKEOFF` |
| `waypoint` | `lat`, `lon`, `alt_m`, optional `hold_s`, `accept_radius_m` | `NAV_WAYPOINT` |
| `loiter_time` | `seconds`, optional `lat`/`lon`/`alt_m` | `NAV_LOITER_TIME` |
| `land` | optional `lat`/`lon` | `NAV_LAND` |
| `rtl` | none | `NAV_RETURN_TO_LAUNCH` |
| `speed` | `speed_m_s` | `DO_CHANGE_SPEED` |
| `delay` | `seconds` | `CONDITION_DELAY` |
| `photo` | none | `IMAGE_START_CAPTURE` (1 photo) |
| `photo_interval` | `seconds`, optional `count` | `IMAGE_START_CAPTURE` |
| `photo_stop` | none | `IMAGE_STOP_CAPTURE` |
| `video_start` / `video_stop` | none | `VIDEO_START_CAPTURE` / `VIDEO_STOP_CAPTURE` |
| `roi` / `roi_none` | `lat`, `lon`, `alt_m` / none | `DO_SET_ROI_LOCATION` / `DO_SET_ROI_NONE` |
| `raw` | `command`, `frame`, `params[4]`, `x`, `y`, `z` | anything else |

Altitudes are metres **above home** (`GLOBAL_RELATIVE_ALT`).

Camera items are executed by ArduPilot during the mission. ArduPilot only forwards them to the bridge if its camera backend is configured; see [05-deployment.md](05-deployment.md#ardupilot-parameters).

## Telemetry requested on connect

On every (re)connect the gateway asks for these rates with `MAV_CMD_SET_MESSAGE_INTERVAL`. They are deliberately modest because they cross 4G.

| Message | Hz |
|---|---|
| `GLOBAL_POSITION_INT`, `ATTITUDE` | 4 |
| `VFR_HUD` | 2 |
| `SYS_STATUS`, `GPS_RAW_INT`, `BATTERY_STATUS`, `EXTENDED_SYS_STATE`, `MISSION_CURRENT`, `EKF_STATUS_REPORT` | 1 |
| `HOME_POSITION` | 0.2 |

Override them with `stream_rates` in the config. `stream_rates: {}` requests nothing, which is observe-only mode.

ArduPilot applies these rates to the serial port the Pi uses, so every consumer on that port sees them.

## Configuration reference

| Key | Default | Meaning |
|---|---|---|
| `listen` | `:8091` | HTTP address (the same port the old Drone-Feed used) |
| `web_dir` | none | Built React UI. Empty means API only. |
| `recordings_dir` | `recordings` | Where recordings are stored |
| `auth.session_secret` | random at startup | Signs login cookies. Set it, or everyone is logged out on restart. |
| `auth.session_ttl` | `12h` | Login lifetime |
| `auth.users[]` | none | `username`, `password_hash` (bcrypt, from `drone-gateway hash-password`), `role` |
| `go2rtc.public_url` | none | go2rtc URL that browsers play from |
| `go2rtc.api_url` | `public_url` | go2rtc URL this server uses for discovery and mp4/hls ingest |
| `go2rtc.rtsp_url` | `rtsp://<api host>:8554` | RTSP base for recording ingest |
| `go2rtc.ingest_mode` | `rtsp` | `rtsp`, `mp4` or `hls` |
| `gcs_system_id` / `gcs_component_id` | `255` / `190` | Gateway MAVLink identity |
| `cors_origins` | none | Allowed browser origins; `"*"` = any |
| `tokens[]` | none | API tokens for scripts: `name`, `token`, `role`. **No users and no tokens means the UI and API are open** (logged as a warning). |
| `allow_raw_commands` | `false` | Enables `POST .../command` |
| `telemetry_hz` | `5` | WebSocket push rate |
| `limits.max_alt_m` | `120` | Takeoff, goto and mission altitude cap |
| `limits.max_speed_m_s` | `15` | Goto, speed and mission speed cap |
| `limits.max_mission_items` | `500` | |
| `stream_rates` | table above | Message name → Hz |
| `drones[]` | (required) | `id`, `name`, `mavlink` (endpoint), optional `system_id`, `stream_prefix` (go2rtc `<prefix>_*` streams, default the id), optional `bridge` and `speaker` (below) |
| `drones[].autopilot` | `dji` with `bridge`, else from the heartbeat (`ardupilot` until one arrives) | The drone's profile, which decides what the dashboard offers and the gateway lets through: `ardupilot`, `dji` (through dji-bridge, needs `bridge`) or `mavlink` (another autopilot: standard protocols only). See [10-drone-types.md](10-drone-types.md). |
| `drones[].features.enable` / `.disable` | none | Features to add to or remove from the profile for this drone, e.g. `disable: [params]`. Unknown names are rejected. |
| `drones[].bridge.api` | none | The aircraft's API beyond MAVLink: thermal measurement, palette, gain and isotherm, obstacle avoidance, RTK (the dashboard's Thermal and Aircraft tabs) and the speaker. For the Mavic 3T: `http://dji-bridge:8090/api/v1/aircraft/<SERIAL>`. No `bridge` = no such tabs. |
| `drones[].bridge.token` | (required with `api`) | Sent upstream as `Authorization: Bearer`; use `${VAR}` (dji-bridge's `api.token`). Browsers never see it. |
| `drones[].speaker.api` | `{bridge.api}/speaker` | A loudspeaker MAVLink can't reach: the base URL of its speaker API, which the gateway proxies the dashboard's Speaker tab to. Only needed without a `bridge`, or to point the speaker elsewhere. No `speaker` and no `bridge` = no Speaker tab. |
| `drones[].speaker.token` | (required with `api`) | Sent upstream as `Authorization: Bearer`; use `${VAR}` (dji-bridge's `api.token`). Browsers never see it. |

### From `speaker` to `bridge`

A drone configured with only `speaker` keeps working as before: Speaker tab, no aircraft features. To give it the Thermal and Aircraft tabs, name the aircraft API once and drop the speaker entry, which then follows the bridge:

```yaml
# before
- id: mavic3t
  speaker: {api: http://dji-bridge:8090/api/v1/aircraft/<SERIAL>/speaker, token: "${BRIDGE_API_TOKEN}"}
# after
- id: mavic3t
  bridge: {api: http://dji-bridge:8090/api/v1/aircraft/<SERIAL>, token: "${BRIDGE_API_TOKEN}"}
```

Keeping both is fine too: `speaker` then wins for the speaker. The bridge must offer `GET /features` and `POST /commands` for the new tabs (dji-bridge does from the release that adds them); until it does, the tabs stay hidden because the gateway passes the bridge's 404 through.

Endpoint syntax for `drones[].mavlink`:

| Form | Use |
|---|---|
| `udp:HOST:PORT` | Production: the router's UDP server on the Pi |
| `tcp:HOST:PORT` | For example the router's TCP 14551; reconnects automatically |
| `udp-server:ADDR:PORT`, `tcp-server:ADDR:PORT` | The drone connects in |
| `serial:DEVICE:BAUD` | A directly attached flight controller |

## Logs

Logs go to stderr as text, or as JSON with `-json-logs` (for Loki). Every POST, PUT and DELETE produces one `audit=true` line with:

- `actor` (the username or token name),
- `drone`,
- `path`,
- `status`,
- `error`,
- `duration_ms`.
