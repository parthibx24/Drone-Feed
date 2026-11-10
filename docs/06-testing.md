# 06 — Testing

## Automated tests

Run `go test ./...`. Add `-race` to also check for data races.

| Test | File | What it proves |
|---|---|---|
| Skydroid golden test | [`drone/skydroid/protocol_test.go`](../drone/skydroid/protocol_test.go) | 31 command strings are byte-identical to the old Python `skydroid_protocol.py` |
| Config | [`server/gateway/config_test.go`](../server/gateway/config_test.go) | `stream_rates: {}` requests nothing (safe observe mode); bad roles, empty tokens, unknown keys and duplicate IDs are rejected |
| Login and web | [`server/gateway/auth_test.go`](../server/gateway/auth_test.go) | Login, logout, cookie flags, CSRF header required for cookie writes, viewer vs operator, tampered and revoked sessions, login rate limit, API tokens, open mode, UI serving with client-side routes, camera discovery, recording ingest URLs |
| Recorder | [`server/recorder/recorder_test.go`](../server/recorder/recorder_test.go) | Port of `recorder.py`, using fake ffmpeg/ffprobe: wait → record → stop → list → delete, cancel while waiting, timeout without frames (no file left behind), path traversal blocked, unsafe names sanitised |
| End-to-end | [`server/gateway/integration_test.go`](../server/gateway/integration_test.go) | The full chain, gateway → simulated autopilot/router → real gimbal-bridge → fake C12 socket, driven through the HTTP API |

### What the end-to-end test covers

| Area | Checks |
|---|---|
| Link | Autopilot identified. Firmware 4.7.0 read. Camera and gimbal components discovered. |
| Auth | 401 without a token, 403 for a viewer sending commands, 404 for an unknown drone |
| Command results | A pre-arm failure is reported as HTTP 409 `FAILED`, not as success |
| Retries | A dropped `COMMAND_ACK` is recovered by a resend with `confirmation=1` |
| Preconditions | Takeoff while disarmed → 422. Altitude over the limit → 422. Unknown mode → 422. |
| Flight | Takeoff switches to GUIDED and climbs. Goto sends a correct `DO_REPOSITION`. Mission start → AUTO. Land. |
| Missions | 6-item upload with a dropped `MISSION_REQUEST` succeeds. Home is at item 0. Download returns the same items. A 0,0 waypoint is rejected. |
| Parameters | Full list recovered after a lost `PARAM_VALUE`. A write is confirmed. An out-of-range write → 409. A missing param → 504. |
| Camera | Photo, record start and absolute zoom reach the C12 as the exact expected packets, with CRLF where needed. Step zoom too. |
| Gimbal | Attitude −45°, centre and lock reach the C12. Lock state is reflected in telemetry. |
| Dead-man | A single rate command moves the gimbal, then the bridge stops it by itself |
| WebSocket | Telemetry is pushed on connect. Hold-to-move rates stream to the C12. Closing the socket stops the gimbal. A viewer token cannot move it. |

All of these pass with `-race` (2026-11-10). The frontend is checked with `npm run build`, which runs the TypeScript compiler in strict mode before bundling.

## Local sandbox

To work on the UI or try the system without a drone, run each of these in its own terminal, from the repository root:

1. `go run ./tools/sim-vehicle -bridge 127.0.0.1:14556`
2. `go run ./drone/cmd/gimbal-bridge -local-port 0 -gimbal-host 127.0.0.1` (sends C12 packets to localhost, where nothing listens)
3. `go run ./server/cmd/drone-gateway -config <file>`, using a config with:
   - `listen: ":8091"`,
   - `web_dir: frontend/dist`,
   - one drone with `mavlink: udp:127.0.0.1:14560`.

   Add an `auth.users` entry to test logins, or leave `auth` out for open mode.
4. Open `http://127.0.0.1:8091`.

**Frontend hot reload:** while the server from step 3 runs, run `npm run dev` in `frontend/` and open `http://localhost:5173`. Vite proxies `/api` (including the WebSocket) to port 8091.

`sim-vehicle` takes `-prearm-fail` to rehearse error handling in the UI. It is not a flight simulator: it moves in straight lines and has no physics.

## ArduPilot SITL (recommended before the first real flight)

For real flight behaviour (mode logic, failsafes, mission execution), point the gateway at ArduPilot SITL instead of sim-vehicle. The server's compose file already references a SITL image, which exposes TCP 5760; use `mavlink: tcp:<host>:5760`. SITL has no C12, so camera and gimbal calls return 409/504 there unless a gimbal-bridge is attached through a router.

## On star9

See [07-status.md](07-status.md) for what has and has not been run against the real drone.
