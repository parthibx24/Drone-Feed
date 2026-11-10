# 05 — Deployment

**Code and configuration:**
- Pi: [`drone/deploy/`](../drone/deploy/)
- Server: [`deploy/server/`](../deploy/server/)
- Tailscale ACL: [`deploy/tailscale-acl.example.hujson`](../deploy/tailscale-acl.example.hujson)
- Build: [`scripts/build.sh`](../scripts/build.sh)

## Overview

There are two halves:

- **The drone Pi:** mavlink-router and gimbal-bridge replace MAVProxy and the old Python drone-agent.
- **The central server:** the `dashboard` container becomes the new single server (drone-gateway serving the new UI).

The order matters, so nothing breaks in between:

| Step | Pi | Central server | Who uses what |
|---|---|---|---|
| Today | MAVProxy + old agent | old Drone-Feed (Python) | old UI → old agent |
| 1. Pi **transition** | mavlink-router + gimbal-bridge, old agent still running | old Drone-Feed | old UI still works; the new server can be tested from a laptop |
| 2. Server switch | unchanged | new image (drone-gateway + UI) | new UI → gateway → mavlink-router |
| 3. Pi **final** | old agent stopped, bridge takes UDP 5000 | new | new system only |

You can roll back the Pi from any stage with `/opt/drone-stack/rollback.sh`, and the server by pointing the compose build back at the old commit.

## 1. Build

Run `./scripts/build.sh` on your Mac. It:

- runs the tests,
- cross-compiles the binaries,
- downloads mavlink-router v4 for aarch64 (cached in `dist/cache/`),
- assembles `dist/pi-bundle/`.

## 2. Before touching a drone

- **The Pi must be stable.** star9 froze several times on 2026-11-10. The main suspect is MAVProxy's log filling RAM, which this stack removes. Even so, cut over on the bench with someone near the drone.
- **Props off.** Bench tests in this guide move the gimbal and can arm the vehicle.
- **Know the sudo password.** The installer asks for it.

## 3. Pi: transition stage

Run `./drone/deploy/deploy.sh dronepi@<tailscale-ip> transition` from the repository root on your Mac. It builds, copies the bundle to `/tmp/drone-stack-bundle` on the Pi, and runs `install.sh transition`, which:

1. installs `mavlink-routerd`, `gimbal-bridge` and `rollback.sh` into `/opt/drone-stack/`;
2. installs `/etc/mavlink-router/main.conf`, backing up any different existing file;
3. creates `/etc/drone-stack/gimbal-bridge.env` (first install only), with the Pi's Tailscale IP filled into the video URLs and `LOCAL_PORT=5001`;
4. installs both systemd units;
5. **disables MAVProxy and starts mavlink-router.** The units also declare a `Conflicts=` so both can never own the serial port at once;
6. starts gimbal-bridge;
7. prints the state of every service.

**Check on the Pi afterwards:**

| What | Healthy result |
|---|---|
| `journalctl -u mavlink-router -n 30` | Endpoints listed. No serial errors. |
| `journalctl -u gimbal-bridge -n 30` | `vehicle identified sysid=1` |
| `curl -s 127.0.0.1:8080/api/health` (old agent) | `mavlink_connected: true`, fed through the router's legacy endpoint |
| `systemctl status mavlink-router gimbal-bridge` | Both active. Memory well under the 48 MB caps. |

**Check from your Mac, with a local server:**

1. Build the UI once: `cd frontend && npm ci && npm run build`.
2. Make a `gateway.yaml` from [`gateway.example.yaml`](../deploy/server/gateway.example.yaml) with:
   - `web_dir: frontend/dist`,
   - no `auth` block (open mode for the test),
   - one drone at `mavlink: udp:<tailscale-ip>:14560`.
3. Run `go run ./server/cmd/drone-gateway -config gateway.yaml`, then open `http://127.0.0.1:8091`.
4. The drone should show **Online**, with firmware 4.7.0 and its position. On the dashboard, the **Drone** pill should be green.
5. On the bench, test the camera and gimbal: Center, the arrow pad, zoom, and a photo from the API. The C12 should respond.

## 4. Server: switch the `dashboard` service

The new image replaces the old Python Drone-Feed **in the same compose service**. It keeps:

- the same container name;
- the same tailscale network namespace;
- the same port, 8091;
- the same `recordings` volume, so existing recordings stay in the library.

1. Commit and push this branch, and note the commit hash.
2. On the server, next to `docker-compose.yml`, create `gateway.yaml` from [`gateway.example.yaml`](../deploy/server/gateway.example.yaml):
   - add the drones (`mavlink: udp:<pi-tailscale-ip>:14560`, `stream_prefix`);
   - add the users. Create each password hash with `docker compose run --rm dashboard hash-password`, after step 4's build.
3. Add `SESSION_SECRET=<openssl rand -hex 32>` to the server's `.env`.
4. Replace the `dashboard` service with:

   | Key | Value |
   |---|---|
   | `build.context` | `https://github.com/parthibx24/Drone-Feed.git#<commit>` |
   | `network_mode` | `"service:tailscale"` (unchanged) |
   | `environment` | `SESSION_SECRET=${SESSION_SECRET}` (the old `GO2RTC_URL`, `AGENT_PORT`, `VIEWER_*`, `RECORDING_PATH` and `GO2RTC_INGEST_MODE` variables are no longer used; their settings now live in `gateway.yaml`) |
   | `volumes` | `./gateway.yaml:/config/gateway.yaml:ro` and `recordings:/recordings` |
   | `restart` | `unless-stopped` |

5. **One-time only:** the old container wrote recordings as root, and the new one runs as an unprivileged user (uid 10001). Hand the volume over with `docker compose run --rm --user root --entrypoint chown dashboard -R 10001 /recordings`.
6. Run `docker compose up -d --build dashboard`, then `docker logs drone-dashboard`. Expect `drone online` for each drone, and no warning about open auth.
7. Open the usual Drone-Feed URL. You should get the login page.

## 5. What happened to the old Drone-Feed backend

It was replaced, not modified: the Python app (`app.py`, `recorder.py`, …) is gone from this branch. Its recording feature now lives in [`server/recorder/`](../server/recorder/), with the same behaviour. Old bookmarks of the form `/?host=…&name=<prefix>` redirect to the matching drone.

## 6. Pi: final stage

Once the new server is live (step 4), run `./drone/deploy/deploy.sh dronepi@<tailscale-ip> final`. This stops and disables the old drone-agent and moves gimbal-bridge to local UDP port 5000.

## Rollback

Run `sudo /opt/drone-stack/rollback.sh` on the Pi. It stops the new services and re-enables MAVProxy and the old drone-agent with their original unit files, which install.sh never modifies.

## ArduPilot parameters

Review these in Mission Planner or QGC, or through the gateway's params API. The names and current values below were **read from star9's Pixhawk (ArduCopter 4.7.0) on 2026-11-10**. 4.7 renamed several parameters, so older guides use different names.

| Parameter (4.7 name) | star9 now | Recommendation | Why |
|---|---|---|---|
| `MAV_SYSID` (was `SYSID_THISMAV`) | 1 | Unique per drone | The gateway keeps drones apart by link, but QGC and logs need unique IDs |
| `MAV_GCS_SYSID` (was `SYSID_MYGCS`) | 255 | Keep 255, matching the gateway's `gcs_system_id` | Only this GCS's heartbeat counts for the GCS failsafe |
| `FS_GCS_ENABLE` | **0 (disabled)** | Decide deliberately, for example 1 = RTL | Today nothing happens if the 4G link drops mid-flight |
| `FS_GCS_TIMEOUT` | 5 | Keep | |
| `FS_OPTIONS` | 16 | Keep | Bit 4: continue an AUTO mission on GCS failsafe |
| `CAM1_TYPE` | 0 | MAVLink Camera v2 (6; check the description on your firmware) | Needed only for **mission** photo and video items. Direct API camera commands work without it. |
| `MNT1_TYPE` | 0 | Keep 0 | The bridge drives the C12 directly |
| `SERIAL5_PROTOCOL` / `SERIAL5_BAUD` | 2 / 115 | Consider 921 (921600), and change `Baud` in `main.conf` to match | SERIAL5 is the only MAVLink 2 port at 115200, so it is almost certainly the Pi's link. At 115200 a full parameter download (1,017 params) took 20 s and delayed heartbeats by more than 3 s. |
| `RTL_ALT_M`, `WP_SPD` (were `RTL_ALT` in cm, `WPNAV_SPEED` in cm/s) | — | Units are now metres and m/s | Clients writing these must use the new names |

Two other things seen on star9 during the check:

- **The stored mission has no takeoff item.** It is 2 waypoints then RTL. ArduCopter will not take off from the ground in AUTO without one.
- **The EKF reported no horizontal position** (GPS HDOP 5.4, 9 satellites). This was probably indoors. Check it outdoors before any flight.

## Security checklist

- [ ] Serve the UI over HTTPS (for example through the same reverse proxy that serves the go2rtc sslip URL), so session cookies are marked Secure.
- [ ] Apply a Tailscale ACL like [`tailscale-acl.example.hujson`](../deploy/tailscale-acl.example.hujson). Tag the drones `tag:drone` and the server `tag:ground`.
- [ ] Configure `auth.users` (and `auth.session_secret`). Without users or tokens the UI and API are open; the server logs a warning at startup.
- [ ] Leave `cors_origins` empty. The UI is served from the same origin, so it is not needed.
- [ ] Keep `allow_raw_commands: false` unless needed.
- [ ] Change the shared `Alpha1` password. It is used for the Pis' SSH and for Cloud Commander in the server's compose file.
