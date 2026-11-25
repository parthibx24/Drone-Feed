# 04 — API Reference

Base URL: `http://<server>:8091/api/v1`. The web UI is served from the same address.

**Authentication**, either of:

- **Browser login** (the UI): `POST /api/auth/login` sets an HttpOnly session cookie. State-changing requests made with the cookie must also send the header `X-Drone-Feed: 1` (CSRF protection), or they get 403. WebSockets use the cookie automatically.
- **API token** (scripts): send `Authorization: Bearer <token>`, or `?token=<token>` on WebSockets. No CSRF header is needed.

| Method | Path | Notes |
|---|---|---|
| POST | `/api/auth/login` | `{"username", "password"}` → `{"username", "role"}` and a session cookie. 401 on wrong credentials; 429 after 10 failures in 15 minutes from one address. |
| POST | `/api/auth/logout` | Clears the cookie |
| GET | `/api/auth/me` | `{"username", "role", "auth_enabled"}`, or 401 |


**Roles:**
- **V** (viewer): read access and telemetry.
- **O** (operator): everything.

All request and response bodies are JSON.

## Responses

| Status | Meaning | Body |
|---|---|---|
| 200 | Done. For commands, the autopilot answered `ACCEPTED`. | `{"ok": true, ...}` |
| 400 | Malformed JSON or missing field | `{"ok": false, "error": "..."}` |
| 401 / 403 | Bad token / operator role needed | |
| 404 | Unknown drone | |
| 409 | The vehicle refused | `{"ok": false, "error": "...", "result": "DENIED"}` |
| 422 | Precondition or limit failed; nothing was sent | |
| 503 | Drone offline | |
| 504 | No answer from the vehicle after retries | |

## Fleet and telemetry

| Method | Path | Role | Notes |
|---|---|---|---|
| GET | `/healthz` (no `/api/v1` prefix) | none | Liveness |
| GET | `/drones` | V | `[{id, name, online, armed, mode, stream_prefix, telemetry, capabilities}]` |
| GET | `/limits` | V | `{max_alt_m, max_speed_m_s, max_mission_items}` |
| GET | `/drones/{id}` | V | Full telemetry snapshot (below) |
| GET | `/drones/{id}/statustext` | V | Last 100 autopilot messages |
| GET | `/drones/{id}/modes` | V | Accepted flight mode names |
| GET | `/drones/{id}/capabilities` | V | What the drone offers ([below](#capabilities)) |
| WS | `/drones/{id}/ws` | V | Live stream (below). Cookie or `?token=`. |

### Telemetry snapshot

Fields are `null` until the matching MAVLink message has been received.

```json
{
  "drone_id": "kotowali", "name": "Kotowali (star9)", "online": true, "last_heartbeat_age_s": 0.4,
  "system_id": 1, "firmware": "4.7.0", "vehicle_type": "QUADROTOR",
  "armed": false, "mode": "STABILIZE", "system_status": "STANDBY", "landed_state": "ON_GROUND",
  "position": {"lat": 23.81, "lon": 90.41, "alt_msl_m": 12.3, "alt_rel_m": 0.1, "heading_deg": 90,
               "v_north_m_s": 0, "v_east_m_s": 0, "v_down_m_s": 0},
  "attitude": {"roll_deg": 0.5, "pitch_deg": -1.1, "yaw_deg": 89.9},
  "speed": {"ground_m_s": 0, "air_m_s": 0, "climb_m_s": 0, "throttle_pct": 0},
  "battery": {"voltage_v": 16.4, "current_a": 5.2, "remaining_pct": 87, "consumed_mah": 120},
  "gps": {"fix_type": "3D_FIX", "satellites": 14, "hdop": 0.8},
  "home": {"lat": 23.81, "lon": 90.41, "alt_msl_m": 10},
  "mission": {"current_seq": 2, "total": 7, "state": "ACTIVE"},
  "ekf": {"flags": 831, "healthy": true},
  "camera": {"present": true, "last_seen": "...", "source": null},
  "gimbal": {"present": true, "yaw_lock": false, "last_seen": "..."},
  "updated_at": "..."
}
```

`camera.source` is the lens the video shows (`"wide"`, `"zoom"` or `"thermal"`) on cameras that can switch it, such as the Mavic 3T through dji-bridge, which names its `VIDEO_STREAM_INFORMATION` after the lens. It is `null` for cameras with one view (star9), and goes back to `null` 5 s after the last report. The dashboard shows a Wide / Zoom / Thermal selector only while it is set.

### WebSocket messages

**Server → client:**

| Message | When |
|---|---|
| `{"type": "telemetry", "data": <snapshot>}` | At `telemetry_hz` |
| `{"type": "statustext", "data": {time, severity, text}}` | As it happens |
| `{"type": "link", "data": {"online": bool}}` | When the drone goes online or offline |
| `{"type": "mission_item_reached", "data": {"seq": n}}` | As it happens |
| `{"type": "capabilities", "data": <capabilities>}` | After the first telemetry, then whenever they change |

**Client → server (operator only):**
- `{"type": "gimbal_rate", "pitch_deg_s": -10, "yaw_deg_s": 0}` sets hold-to-move rates.
- Send it at 4 Hz or more while the control is held, then send zeros on release. The gimbal also stops by itself if updates stop.

### Capabilities

```json
{
  "autopilot": "dji",
  "model": "M3T",
  "features": ["aircraft", "arm", "gimbal", "goto", "land", "mission", "mission_start", "pause", "photo", "recording", "resume", "roi", "rtl", "speaker", "takeoff", "zoom"],
  "modes": [],
  "mission_items": ["takeoff", "waypoint", "…"],
  "mission_alt_frames": ["relative"],
  "note": "…"
}
```

`autopilot` is the profile (`ardupilot`, `dji` or `mavlink`); `model` is set when the aircraft reports one; `modes` lists the modes `POST /mode` accepts (empty without `set_mode`); `note` says why fewer features than usual are offered. How they are worked out: [10-drone-types.md](10-drone-types.md).

A route whose feature the drone's profile rules out answers **409** `"<drone> does not offer this (<feature>)"` before anything is sent:

| Feature | Routes |
|---|---|
| `arm` | `arm`, `disarm` |
| `set_mode` | `mode` |
| `takeoff`, `land`, `rtl`, `pause`, `resume`, `goto`, `speed`, `reboot` | the route of the same name |
| `roi` | `POST` and `DELETE /roi` |
| `mission` | `GET`, `PUT` and `DELETE /mission` |
| `mission_start`, `mission_jump` | `/mission/start`, `/mission/current` |
| `params` | every `/params` route, reads included |
| `gimbal` | `/gimbal/*` |
| `photo`, `camera_video`, `zoom` | `/camera/photo*`, `/camera/video/*`, `/camera/zoom` |

Gimbal and camera routes are refused only when the profile rules them out, not while the component simply hasn't been seen.

## Flight

| Method | Path | Role | Body | Notes |
|---|---|---|---|---|
| POST | `/drones/{id}/arm` | O | `{"force": false}` | `force` bypasses pre-arm checks. Avoid it. |
| POST | `/drones/{id}/disarm` | O | `{"force": false}` | ArduCopter refuses in flight unless forced |
| POST | `/drones/{id}/mode` | O | `{"mode": "LOITER"}` | Any name from `/modes` |
| POST | `/drones/{id}/takeoff` | O | `{"alt_m": 10}` | Must be armed. Switches to GUIDED. |
| POST | `/drones/{id}/land` | O | none | LAND mode |
| POST | `/drones/{id}/rtl` | O | none | RTL mode |
| POST | `/drones/{id}/goto` | O | `{"lat", "lon", "alt_m", "speed_m_s"?}` | GUIDED reposition. Altitude above home. |
| POST | `/drones/{id}/speed` | O | `{"speed_m_s": 8}` | |
| POST | `/drones/{id}/pause` / `resume` | O | none | Holds position in AUTO/GUIDED, then continues |
| POST | `/drones/{id}/roi` | O | `{"lat", "lon", "alt_m"}` | Points the vehicle at a location |
| DELETE | `/drones/{id}/roi` | O | none | |
| POST | `/drones/{id}/reboot` | O | none | Flight controller. Refused while armed. |

## Missions

| Method | Path | Role | Body |
|---|---|---|---|
| GET | `/drones/{id}/mission` | V | Returns `{"items": [...]}` downloaded from the vehicle |
| PUT | `/drones/{id}/mission` | O | `{"items": [...]}`; replaces the mission (see the item types in [03-drone-gateway.md](03-drone-gateway.md#mission-item-types)) |
| DELETE | `/drones/{id}/mission` | O | none |
| POST | `/drones/{id}/mission/start` | O | none. Must be armed; switches to AUTO. |
| POST | `/drones/{id}/mission/current` | O | `{"seq": 3}`; jump to an item |

Example body:

```json
{"items": [
  {"type": "takeoff", "alt_m": 20},
  {"type": "speed", "speed_m_s": 6},
  {"type": "waypoint", "lat": 23.8110, "lon": 90.4130, "alt_m": 30, "hold_s": 3},
  {"type": "photo"},
  {"type": "waypoint", "lat": 23.8120, "lon": 90.4140, "alt_m": 30},
  {"type": "rtl"}
]}
```

Items with an altitude can carry `"alt_frame"`:
- `"relative"`: above home. This is the default, used when the field is absent.
- `"terrain"`: above the ground.
- `"absolute"`: above mean sea level. It is checked against `max_alt_m` relative to home, so it needs a known home position.

Reading a mission returns the frame it was written with. A mission from QGC or Mission Planner therefore keeps its altitude meaning when it is edited and written back.

## Parameters

| Method | Path | Role | Notes |
|---|---|---|---|
| GET | `/drones/{id}/params` | V | Cached list (the first request downloads it). `?refresh=1` forces a new download (about 1,000 params, ~20 s on star9); once a cache exists that needs the operator role. |
| GET | `/drones/{id}/params/{name}` | V | Read one. 504 if it does not exist. |
| PUT | `/drones/{id}/params/{name}` | O | `{"value": 3000}`. 409 if the vehicle keeps a different value. |

## Gimbal and camera

| Method | Path | Role | Body |
|---|---|---|---|
| POST | `/drones/{id}/gimbal/attitude` | O | `{"pitch_deg": -45, "yaw_deg": 0}` (either field may be omitted) |
| POST | `/drones/{id}/gimbal/rate` | O | `{"pitch_deg_s": -10, "yaw_deg_s": 0}`. **Not acknowledged; repeat at 4 Hz or more.** |
| POST | `/drones/{id}/gimbal/center` | O | none |
| POST | `/drones/{id}/gimbal/mode` | O | `{"mode": "follow"}` or `"lock"` |
| POST | `/drones/{id}/camera/photo` | O | none |
| POST | `/drones/{id}/camera/photo/interval` | O | `{"seconds": 2, "count": 0}` (0 = until stopped) |
| POST | `/drones/{id}/camera/photo/stop` | O | none |
| POST | `/drones/{id}/camera/video/start` / `stop` | O | none |
| POST | `/drones/{id}/camera/zoom` | O | `{"type": "step", "value": 1}`, `{"type": "continuous", "value": -1}`, or `{"type": "range", "value": 50}` |
| POST | `/drones/{id}/camera/source` | O | `{"source": "wide"}`, `"zoom"` or `"thermal"`: switches the lens the video shows (`MAV_CMD_SET_CAMERA_SOURCE`, param2 4 / 1 / 2). Only for cameras that report `camera.source`; a fixed camera answers 409 UNSUPPORTED. |

## Speaker

For drones with a `speaker` (or a `bridge`, whose speaker is `{bridge.api}/speaker`) in the config; others answer 404 `{"error": "no speaker"}`. Not MAVLink: the gateway checks the login and role and proxies to the drone's speaker API (dji-bridge for the Mavic 3T), passing its status codes and JSON through. Unreachable speaker service: 502; no answer: 504; the service refused the gateway's token: 502 (never 401, which would sign the user out).

| Method | Path | Role | Notes |
|---|---|---|---|
| GET | `/drones/{id}/speaker` | V | `{present, status: idle\|playing\|transmitting\|error\|unknown, volume, languages, can_play}` |
| POST | `/drones/{id}/speaker/say` | O | `{"text": "...", "lang": "en"\|"bn", "loop": false}`. Text up to 500 characters. Spoken once, or repeated until stop. 409 `{"error": reason, "result": "refused"\|"failed"}` when the pilot hasn't allowed StarWatch control or the speaker failed. |
| POST | `/drones/{id}/speaker/stop` | O | Stops a message or loop |
| POST | `/drones/{id}/speaker/volume` | O | `{"volume": 0..100}` |
| GET | `/drones/{id}/speaker/live` | O | WebSocket, push-to-talk. Send binary PCM (signed 16-bit little-endian, 16 kHz, mono) in real time, then `{"type": "end"}`; receive one `{"type": "result", "outcome": "done"\|"refused"\|"failed", "reason"}`. Frames up to 64 kB, sessions up to 5 minutes. |

## Aircraft features

For drones with a `bridge` in the config (the Mavic 3T); others answer 404 `{"error": "no aircraft features"}`, so the dashboard shows nothing for them. Not MAVLink: the gateway checks the login and role and proxies to the drone's aircraft API (dji-bridge), passing its status codes and JSON through. Unreachable service: 502 `"aircraft service unreachable"`; no answer within 15 s: 504; the service refused the gateway's token: 502.

| Method | Path | Role | Notes |
|---|---|---|---|
| GET | `/drones/{id}/features` | V | The aircraft's state, below |
| POST | `/drones/{id}/aircraft-command` | O | One command, below. Body up to 4 kB, forwarded unchanged. 200 `{"ok": true, "kind"}`; the aircraft's or the pilot's refusal comes back as it is, usually 409 or 502 `{"error": reason}`. Any other `kind` is a 400 that never reaches the bridge: flight, gimbal and camera commands stay on the MAVLink routes above. |

`GET /features`:

```json
{
  "features": ["thermal_measure", "thermal_palette", "thermal_gain", "isotherm", "obstacle_avoidance", "rtk", "health"],
  "thermal": {
    "palette": "white_hot", "palettes": ["white_hot", "black_hot", "iron_red"], "gain": "high",
    "isotherm": {"enabled": false, "low_c": 30, "high_c": 40},
    "measure": "spot",
    "spot": {"x": 0.62, "y": 0.45, "temp_c": 36.4},
    "area": null
  },
  "avoidance": {"mode": "brake", "horizontal": true, "up": true, "down": true},
  "rtk": {"present": true, "enabled": true, "fix": "fixed", "sats": 27},
  "online": true
}
```

- `features` lists what the aircraft offers; the dashboard shows a control only when its feature is listed.
- `thermal`, `avoidance` and `rtk` are `null` when the aircraft has no such thing or hasn't reported it yet. `gain` is `high`, `low` or `auto`; `measure` is `off`, `spot` or `area`; `mode` is `brake`, `bypass` or `off`; `fix` is `none`, `single`, `float` or `fixed`.
- Positions and sizes (`x`, `y`, `width`, `height`) are fractions (0..1) of the thermal image, from its top-left corner.
- `online` is false while the aircraft, or the pilot app carrying it, is not connected.

Commands (`kind` plus the fields of that kind):

| `kind` | Fields |
|---|---|
| `set_metering` | `mode`: `off`, `spot` or `area` |
| `meter_spot` | `x`, `y` |
| `meter_area` | `x`, `y`, `width`, `height` |
| `thermal_palette` | `palette`: one of `thermal.palettes` |
| `thermal_gain` | `gain`: `high` or `low` |
| `thermal_isotherm` | `enabled`, `low_c`, `high_c` |
| `obstacle_avoidance` | `mode`: `brake`, `bypass` or `off` |
| `rtk_enable` | `enabled` |

Each command is audited with its kind (`msg="aircraft command" kind=...`) as well as by the usual audit line.

## Video and recordings

| Method | Path | Role | Notes |
|---|---|---|---|
| GET | `/drones/{id}/cameras` | V | `{go2rtc_url, stream_prefix, streams[], discovered}`. Streams are go2rtc's `<prefix>_*`, falling back to `<prefix>_camera` and `<prefix>_thermal`. |
| POST | `/drones/{id}/recording/start` | O | Waits for frames from every stream, then records them all into one MKV without re-encoding. Once stopped, the MKV is copied (no re-encoding) into an MP4 with its index at the front, and removed once the copy is checked: the listing then shows the `.mp4`, and a link to the `.mkv` still finds it. |
| POST | `/drones/{id}/recording/stop` | O | Finalises the file. Returns duration, size and metadata. |
| GET | `/drones/{id}/recording` | V | This drone's recording state |
| GET | `/recordings/status` | V | `{active_count, recordings, by_drone}`; `by_drone` is keyed by stream prefix |
| GET | `/recordings?sort_by=time\|duration\|size\|name&order=desc\|asc&name=` | V | Library listing with ffprobe metadata |
| GET / HEAD | `/recordings/download/{path}` | V | The file, with range requests (the browser's player seeks with them) |
| DELETE | `/recordings/{path}` | O | 409 while that file is still recording |

## Raw command (disabled by default)

| Method | Path | Role | Body |
|---|---|---|---|
| POST | `/drones/{id}/command` | O | `{"command": 183, "component": 1, "params": [1, 1500, null, null, null, null, null]}` |

Requires `allow_raw_commands: true`. A `null` param is sent as NaN, which MAVLink treats as "unused".
