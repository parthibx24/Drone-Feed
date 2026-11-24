# 07 — Status (2026-11-10)

## Done and verified

| What | How it was verified |
|---|---|
| C12 packets are byte-identical to the old system | 31 golden strings generated from the Python implementation |
| Gateway ↔ autopilot protocols: commands and ACKs, retries, missions, parameters, telemetry | End-to-end test against the simulated drone, with injected packet loss |
| Gateway ↔ gimbal-bridge ↔ C12, including the dead-man stop and WebSocket hold-to-move | End-to-end test, with a socket recording the exact UDP packets |
| Login, sessions, roles, CSRF, rate limit, API tokens | `auth_test.go` |
| Recording (port of `recorder.py`) | `recorder_test.go` with fake ffmpeg |
| No data races | `go test -race ./...` |
| UI type-checks and builds | `npm run build` (strict TypeScript) |
| **Whole system running locally** | sim-vehicle + gimbal-bridge + drone-gateway serving the built UI. Logged in with real bcrypt users. Exercised arm, takeoff, goto, mission write/read, parameter write and camera through the browser API path (cookie + CSRF header). Headless Chrome screenshots of login, fleet, dashboard, mission and messages pages looked correct. |
| **Gateway against the real Pixhawk on star9 (read-only)** | ArduCopter 4.7.0 identified. Telemetry decoded. Mission downloaded. All 1,017 parameters downloaded. Nothing on the drone was changed. |
| **Pi transition install on star9 (2026-11-11)** | `install.sh transition` was run. mavlink-router opened `/dev/ttyAMA0` at 115200 and all endpoints. The gateway connected over UDP 14560 and found the camera (100) and gimbal (154) components. In a single `top` snapshot, mavlink-router and gimbal-bridge used about 8% CPU each; MAVProxy had used about 46%. The old drone-agent kept running. |
| **Real video and recording in the new server** | Both star9 streams were discovered through go2rtc. A 30 s recording held both streams: visible H.265 1280×720 and thermal 384×288. |
| **Flight commands against ArduPilot SITL 4.7.0 (2026-11-11)** | The gateway's Docker image ran against the official ArduCopter 4.7.0 SITL binary over TCP 5760, through the HTTP API. Precondition errors returned 422. Set mode, arm and takeoff to 20 m worked. Goto (`DO_REPOSITION`) 100 m north reached the target at 25 m. A 4-item mission (takeoff, 2 waypoints, RTL) uploaded, read back identically, and ran in AUTO through `MISSION_START`. The drone landed at home and disarmed. Pause held position (0.5 m drift in 5 s) and resume continued the leg. RTL and Land both landed and disarmed. |

### Bugs found and fixed while testing

| Bug | Fix |
|---|---|
| The online timeout of 3 s flapped during parameter downloads on star9 | Raised to 5 s |
| Race condition in telemetry snapshots | Sub-structs are now replaced, never modified in place |
| `${VAR}` config expansion mangled bcrypt hashes (`$2a$12$…`) | Only `${VAR}` is expanded now |
| "Take off" right after "Arm" was refused, because the armed state lags by about 1 s | Flight commands wait up to 2 s for it |

### Found by an independent code review (2026-11-11), fixed

| Bug | Fix |
|---|---|
| Changing gimbal direction mid-hold (yaw → pitch) left the old axis turning | The idle axis is sent 0 explicitly |
| A release racing the motion loop could leave the gimbal moving | All speed/stop packets are sent under one lock; stops are repeated 3 times |
| Holding an arrow key or the pad, then switching window or tab, kept the gimbal moving | Window blur, hidden tab, lost pointer capture and unmount all release |
| Reading a terrain- or MSL-altitude mission (from QGC or Mission Planner) and writing it back made every altitude relative to home | Items keep `alt_frame` (`relative`, `terrain` or `absolute`); the UI labels the altitude accordingly |
| A parameter write could happen after the request had already reported a timeout; a stale `PARAM_VALUE` could be reported as "rejected" | The lock wait respects the deadline, nothing is sent after it, and only an echo with no matching value counts as a rejection |
| A viewer could force repeated full parameter downloads, loading the serial link | `?refresh=1` with a cache present needs the operator role |
| "Set mode → AUTO" in the flight panel started the mission without confirmation | It now asks first, like "Start" in the mission planner |
| One person's failed logins behind a proxy locked out everyone | The rate limit is keyed on address + username |
| `install.sh` disabled MAVProxy even if mavlink-router then crashed, leaving the serial port without an owner | If the router is not up, or has restarted, after 5 s, the script restores MAVProxy and stops |

### Review notes still open

| Note | Risk |
|---|---|
| ACKs are matched by command ID only. A very late ACK from a timed-out command could confirm a repeat of the same command. | Low |
| MAVLink ports on the Pi (UDP 14560, TCP 14551) listen on every interface, without MAVLink signing. Restrict them with Tailscale ACLs and keep the onboard LAN closed. | Medium |
| If the autopilot reboots in under 8 s, stream rates may not be re-requested | Low |

## Not yet verified

| What | Risk if wrong | How to verify |
|---|---|---|
| ArduCopter accepting `DO_SET_MISSION_CURRENT` (jump to a mission item). The other flight commands passed in SITL 4.7.0. | Low | ArduPilot SITL 4.7.0 |
| Flight commands on the real Pixhawk | Medium: SITL does not run star9's parameters | Bench test with props off |
| mavlink-router over a long run: memory, and tlog files while armed | Low (it started cleanly on star9) | Leave it running; check `journalctl -u mavlink-router` |
| gimbal-bridge with the real C12: replies on port 5001, angle signs, zoom behaviour | Medium | Bench test (installed on star9; needs the 2026-11-11 bridge fix re-installed) |
| Real video in the browser UI (WebRTC playback) | Low: discovery and recording worked with the real streams | Open the star9 dashboard |
| The Docker image build | Low: each stage was built separately on a Mac, but Docker was not running here | `docker build .` on the server |
| Recording with real streams and real ffmpeg in the container | Low: the same ffmpeg commands as the Python version | Record once after the server switch |
| Mission photo items triggering the C12 through ArduPilot (`CAM1_TYPE`) | Medium | SITL plus bench |

## Known limitations

1. **The gimbal cannot be pointed from inside a mission.** ArduPilot does not manage the C12 (`MNT1_TYPE` 0), so mission ROI turns the vehicle, not the gimbal.
2. **No gimbal attitude feedback**, and **no thermal palette control** through MAVLink yet.
3. **"Go to" uses the drone's current altitude.** To change altitude, use a mission or the API.
4. **Sessions cannot be revoked one by one.** Logout clears the browser cookie, and sessions expire after `session_ttl`. Removing a user from the config ends that user's sessions immediately.
5. **One gateway per tailnet.** QGC alongside it should use MAVLink system ID 254.

## Deployed

| What | Notes |
|---|---|
| **Server switch on Dokploy (2026-11-11)** | The `dashboard` service of the Dokploy compose app runs commit `fb0a659`. `gateway.yaml` is a Dokploy file mount (`../files/gateway.yaml`), and a one-shot `recordings-perms` service hands the recordings volume to uid 10001. Login, video and telemetry work for star9. Its go2rtc streams are `Kotowali-Thana-SMP_*`, so that is its `stream_prefix`. The old `kotowali_*` streams point to a Pi that is no longer on the tailnet. |

| **Direct Tailscale path to the drones (2026-11-11)** | The server's tailscaled ran behind Docker's NAT on a random port, so every connection to the server, including star9's video and MAVLink, went through the DERP relay in Mumbai (one TCP connection, limited throughput, stalls on 4G loss). `TS_TAILSCALED_EXTRA_ARGS=--port=40000` on the `tailscale` service (with `40000:40000/udp` published) fixed it. The server now reaches star9 directly over its 4G address, 66 ms per ping (the relay path took about 140 ms). |

Open after the switch:
- Behind Traefik every login comes from the proxy's address, so failed logins on a username lock it for everyone for a short while. The fix is to trust `X-Forwarded-For` from the proxy.
- The UI shows the autopilot's sea-level altitude even without a GPS fix, where it is meaningless (8393.8 m was seen indoors).
- The go2rtc API on the public `cams…` domain has no login.
- After the gateway restarted, star9's MAVLink took a while to come back, and it is not known what brought it back. If it happens again, check the gateway logs and `tailscale ping` from the `drone-tailnet` container.

## AI detection (branch `feature/ai-detection`, 2026-11-11, not deployed)

See [09-ai.md](09-ai.md). Built: the detector service, gateway endpoints, event log with snapshots, ground position estimates, Telegram and webhook alerts, auto-record, "Analyze frame" with Claude, click-to-follow, and the UI (boxes on video, AI tab, map markers, Detections page).

| Verified | How |
|---|---|
| Gateway AI logic | `ai_test.go`, with fake go2rtc, webhook and Claude servers |
| Detector logic, both models, H.265 decoding | Unit tests; the x86_64 Docker image built and ran them, and decoded an H.265 clip |
| Whole chain on a laptop | sim-vehicle + gimbal-bridge + gateway + detector on test videos: 42 events with snapshots and ground positions, webhook alerts with cooldown, follow started and cancelled by gimbal input, UI checked in headless Chrome |
| Thermal accuracy | HIT-UAV test split: person precision/recall 0.88/0.88 |

| Not verified | Why it matters |
|---|---|
| Accuracy on C12 footage | Both models were trained on other cameras |
| C12 field of view (`ai.camera`) and gimbal angle signs | Ground positions depend on both; the angle signs are also still open for the bench test |
| Detector CPU on the server | Measured on a Mac only; watch `docker stats` after deploying |
| "Analyze frame" against the real Claude API | Tested against a fake server only; needs an API key |
| Telegram delivery | Same code path as the tested webhook, but never sent to a real bot |

Not built: fire/smoke detection (needs a separate model), flight-log health reports, map stitching (needs mission photo triggering).

## Next steps, in order

1. Link-loss failsafe is handled by the RC link for now (`FS_GCS_ENABLE` stays 0). Give each drone a unique `MAV_SYSID` before a second drone is added.
2. ~~Validate the flight commands against ArduPilot SITL 4.7~~ (done 2026-11-11, except jump to mission item).
3. ~~Do the Pi transition install on star9~~ (done 2026-11-11). Re-run it with the fixed bundle, then do the camera and gimbal bench test (props off).
4. ~~Switch the server's `dashboard` service to this branch~~ (done 2026-11-11 on Dokploy; see Deployed above).
5. Do the Pi final install (retire the old agent).
6. Deploy AI ([05-deployment.md §7](05-deployment.md#7-ai-detection-optional)), then check detection on recordings from the C12 and set the real field of view.
