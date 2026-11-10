# 01 — Architecture

## The design in one sentence

Keep the drone thin, run everything else in **one central server**, and speak standard MAVLink between them.

## Data flow

```
┌──────────── Drone: Raspberry Pi Zero 2 W ─────────────┐           ┌──────────── central-drone-server ─────────────┐
│                                                       │           │                                               │
│ Pixhawk 6C ─UART─► mavlink-router ─UDP 14560──────────┼───────────┼─► drone-gateway :8091  (one container)        │
│ ArduCopter 4.7        │  ▲      TCP 14551 (QGC)        │ Tailscale │     • React UI (login, fleet, dashboard,      │
│                       ▼  │                            │  (4G)     │       mission, parameters, messages)          │
│              gimbal-bridge (UDP 14556)                │           │     • REST + WebSocket API                    │◄── browsers
│              camera comp 100 / gimbal comp 154        │           │     • login, roles, audit log                 │
│                       │                               │           │     • server-side recording (ffmpeg)          │
│ Skydroid C12 ◄─UDP 5000┘                              │           │                │ discovery / ingest           │
│   └─RTSP──► go2rtc :8554 ─────────────────────────────┼───────────┼─► go2rtc (central) ──── WebRTC / MP4 ─────────┼──► browsers
└───────────────────────────────────────────────────────┘           └───────────────────────────────────────────────┘
```

## One server instead of two

Before this change the central server ran two things:

- the Drone-Feed Python backend (UI, recordings, and forwarding to each Pi's agent);
- (planned) a separate MAVLink gateway.

They are now **one Go program, drone-gateway**. Combining them means:

- no extra hop for every request and every telemetry update;
- one login system and one audit log;
- one container to deploy.

The Python backend was retired. Its recording feature was ported to Go with the same behaviour and response shapes. Video stays on go2rtc, unchanged.

## Why each piece

### mavlink-router replaces MAVProxy (drone)

| | MAVProxy (before) | mavlink-router (now) |
|---|---|---|
| Language | Python | C++ |
| CPU on the Pi Zero | about 46% | low single digits (expected; not yet measured on star9) |
| Telemetry log | `/tmp`, which is RAM: about 23 MB/h until the Pi froze | SD card, only while armed, pruned automatically |
| Routing | copies everything everywhere | routes by target system and component |

### gimbal-bridge replaces the Python drone-agent's gimbal code (drone)

- **Standard MAVLink.** It speaks the Camera Protocol, and Gimbal Protocol v2 as gimbal manager. So QGroundControl, Mission Planner, the gateway and ArduPilot missions can all drive the C12 without knowing Skydroid's protocol.
- **Safety: dead-man stop.** Hold-to-move motion stops by itself if updates stop arriving, so a dropped 4G link cannot leave the gimbal spinning.
- **No more lock-up.** The old agent's re-entrant lock deadlock (star9-docs issue 4) cannot happen here.

### drone-gateway (server)

- **Commands are acknowledged.** Every command waits for `COMMAND_ACK` and retries lost packets. The UI shows what the autopilot actually decided (`ACCEPTED`, `DENIED`, `FAILED`, ...).
- **Missions follow the MAVLink handshake**, with home at item 0, so the first waypoint is no longer lost.
- **Parameters are fetched completely.** Lost values are re-requested, and writes are checked against the vehicle's echo.
- **Telemetry crosses 4G once per drone** and is fanned out to any number of viewers.
- **Security:**
  - web logins with bcrypt passwords and signed session cookies;
  - viewer and operator roles;
  - API tokens for scripts;
  - CSRF protection;
  - a login rate limit;
  - an audit log line for every action;
  - server-side limits on altitude, speed and mission size.
- **Updates happen in one place:** change the server and every drone gets the new behaviour without SSH.

## What stays on the drone if the link drops

Uploaded missions run on the Pixhawk. ArduPilot's own failsafes (GCS link loss, battery, fence) act without the server. The gateway sends a 1 Hz ground-station heartbeat precisely so that ArduPilot's GCS failsafe can detect a lost link; see [05-deployment.md](05-deployment.md#ardupilot-parameters).

## Repository layout

| Path | What |
|---|---|
| `frontend/` | React + TypeScript UI (Vite) |
| `server/` | drone-gateway: `cmd/`, `gateway/` (links, API, auth, web), `recorder/` |
| `drone/` | Pi side: `cmd/gimbal-bridge`, `bridge/`, `skydroid/`, `deploy/` (mavlink-router config, systemd units, install scripts) |
| `internal/` | Shared Go code: `mav/` (MAVLink helpers), `sim/` (simulated drone) |
| `tools/sim-vehicle` | Fake drone for development |
| `deploy/` | Server config example, Tailscale ACL example |
| `scripts/build.sh` | Tests, cross-compiles, builds the Pi bundle |
| `Dockerfile` | The central server image (UI + gateway + ffmpeg) |

## Ports

| Where | Port | Proto | Who | Purpose |
|---|---|---|---|---|
| Pi | 14560 | UDP | mavlink-router | drone-gateway link (gateway connects in) |
| Pi | 14551 | TCP | mavlink-router | ad-hoc QGC / Mission Planner (same port as MAVProxy used) |
| Pi | 14556 | UDP (localhost) | gimbal-bridge | MAVLink from the router |
| Pi | 14550 | UDP (localhost) | old drone-agent | legacy endpoint, transition period only |
| Pi | 5000 / 5001 | UDP | gimbal-bridge | Skydroid C12 control (5001 while the old agent still holds 5000) |
| Pi | 1984, 8554, 8555 | TCP/UDP | go2rtc | video (unchanged) |
| Server | 8091 | TCP | drone-gateway | UI + REST + WebSocket (same port the old Drone-Feed used) |

## MAVLink identities

| Who | System ID | Component ID |
|---|---|---|
| Pixhawk | `MAV_SYSID` (1 on star9 today) | 1 |
| C12 camera (gimbal-bridge) | same as the vehicle | 100 (`MAV_COMP_ID_CAMERA`) |
| C12 gimbal (gimbal-bridge) | same as the vehicle | 154 (`MAV_COMP_ID_GIMBAL`) |
| drone-gateway | 255 (configurable) | 190 (configurable) |
