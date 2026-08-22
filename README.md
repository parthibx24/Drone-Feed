# Drone View

Lightweight single-drone monitor — **visible + thermal video** and **full MAVLink telemetry** over Tailscale.

Runs on any device (laptop, tablet, ground-station PC) that can reach one Pi via Tailscale. No fleet registry, no gimbal controls — view only.

```
Your device (this app :8091)
        │ Tailscale
        ▼
Raspberry Pi on drone
  go2rtc :1984   ← video (browser connects direct)
  drone-agent :8080 ← MAVLink telemetry (proxied via this app)
```

## Setup

```bash
cd "/Users/startechnologiesinc/Documents/Rifat/Star Technologies/Drone_View"

cp .env.example .env
# Edit .env — set DRONE_HOST to the Pi Tailscale IP

python3 -m venv .venv
.venv/bin/pip install -r requirements.txt

.venv/bin/python __main__.py
```

Open **http://localhost:8091** (or `http://<this-device-ip>:8091` from another screen on the LAN).

## `.env` options

| Variable | Default | Description |
|----------|---------|-------------|
| `DRONE_HOST` | *(required)* | Pi Tailscale IP, e.g. `100.91.79.23` |
| `DRONE_NAME` | `Drone` | Shown in the header |
| `AGENT_PORT` | `8080` | drone-agent on the Pi |
| `GO2RTC_PORT` | `1984` | go2rtc on the Pi |
| `STREAM` | `skydroid_camera` | Visible stream name |
| `THERMAL_STREAM` | `skydroid_thermal` | Thermal stream name |
| `VIEWER_HOST` | `0.0.0.0` | Bind address |
| `VIEWER_PORT` | `8091` | Web UI port |

## What the Pi needs

Same as [Gimbal_Control](../Gimbal_Control/docs/SETUP_NEW_DRONE.md) Part 3 — **no extra install** if the Pi already runs:

- `go2rtc` with camera streams
- `drone-agent` with MAVLink bridge
- `MAVProxy` → `127.0.0.1:14550`

This viewer only **reads** telemetry and video; it does not replace the fleet dashboard.

## Copy to another device (no git)

Copy the whole `Drone_View` folder, create `.env` with that drone’s IP, install deps, run `__main__.py`.

Exclude `.venv` when copying — recreate on each device:

```bash
rsync -avz --exclude '.venv' Drone_View/ user@device:/path/Drone_View/
```

## vs Gimbal_Control fleet dashboard

| | **Drone_View** | **Gimbal_Control** |
|--|----------------|---------------------|
| Drones | One (`.env`) | Many (`drones.yaml`) |
| Video | Both feeds at once | Toggle visible/thermal |
| Gimbal / mission / params | No | Yes |
| Port | 8091 | 8090 |
