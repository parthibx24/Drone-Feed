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
| GET | `/drones` | V | `[{id, name, online, armed, mode, stream_prefix, telemetry}]` |
| GET | `/limits` | V | `{max_alt_m, max_speed_m_s, max_mission_items}` |
| GET | `/drones/{id}` | V | Full telemetry snapshot (below) |
| GET | `/drones/{id}/statustext` | V | Last 100 autopilot messages |
| GET | `/drones/{id}/modes` | V | Accepted flight mode names |
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
  "camera": {"present": true, "last_seen": "..."},
  "gimbal": {"present": true, "yaw_lock": false, "last_seen": "..."},
  "updated_at": "..."
}
```

### WebSocket messages

**Server → client:**

| Message | When |
|---|---|
| `{"type": "telemetry", "data": <snapshot>}` | At `telemetry_hz` |
| `{"type": "statustext", "data": {time, severity, text}}` | As it happens |
| `{"type": "link", "data": {"online": bool}}` | When the drone goes online or offline |
| `{"type": "mission_item_reached", "data": {"seq": n}}` | As it happens |

**Client → server (operator only):**
- `{"type": "gimbal_rate", "pitch_deg_s": -10, "yaw_deg_s": 0}` sets hold-to-move rates.
- Send it at 4 Hz or more while the control is held, then send zeros on release. The gimbal also stops by itself if updates stop.

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

## Video and recordings

| Method | Path | Role | Notes |
|---|---|---|---|
| GET | `/drones/{id}/cameras` | V | `{go2rtc_url, stream_prefix, streams[], discovered}`. Streams are go2rtc's `<prefix>_*`, falling back to `<prefix>_camera` and `<prefix>_thermal`. |
| POST | `/drones/{id}/recording/start` | O | Waits for frames from every stream, then records them all into one MKV without re-encoding |
| POST | `/drones/{id}/recording/stop` | O | Finalises the file. Returns duration, size and metadata. |
| GET | `/drones/{id}/recording` | V | This drone's recording state |
| GET | `/recordings/status` | V | `{active_count, recordings, by_drone}`; `by_drone` is keyed by stream prefix |
| GET | `/recordings?sort_by=time\|duration\|size\|name&order=desc\|asc&name=` | V | Library listing with ffprobe metadata |
| GET / HEAD | `/recordings/download/{path}` | V | The file |
| DELETE | `/recordings/{path}` | O | 409 while that file is still recording |

## Raw command (disabled by default)

| Method | Path | Role | Body |
|---|---|---|---|
| POST | `/drones/{id}/command` | O | `{"command": 183, "component": 1, "params": [1, 1500, null, null, null, null, null]}` |

Requires `allow_raw_commands: true`. A `null` param is sent as NaN, which MAVLink treats as "unused".
