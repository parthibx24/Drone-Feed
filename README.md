# Drone View

Lightweight single-drone monitor — **visible + thermal video** and **full MAVLink telemetry** over Tailscale.

Runs on any device (laptop, tablet, ground-station PC) that can reach one Pi via Tailscale. No fleet registry, no gimbal controls — view only.

```
Your device (this app :8091)
        │ Tailscale
        ▼
Raspberry Pi on drone
  go2rtc (centralized) ← video (browser connects direct)
  drone-agent :8080    ← MAVLink telemetry (proxied via this app)
```

## Setup

### Local Python Environment

```bash
cp .env.example .env
# Edit .env — configure go2rtc URL and agent port

python3 -m venv .venv
.venv/bin/pip install -r requirements.txt

.venv/bin/python __main__.py
```

### Docker / Docker Compose

```bash
cp .env.example .env
# Edit .env — configure go2rtc URL and agent port

# Using Docker Compose (recommended)
docker compose up -d

# Or using Docker directly
docker build -t drone-feed .
docker run -d --name drone-feed --env-file .env -p 8091:8091 drone-feed
```

### Open in browser

The drone IP and name are specified as **URL query parameters**:

```
http://localhost:8091/?host=100.91.79.23&name=kutwali
```

The `name` parameter is used as a **prefix for go2rtc stream names**:
- `kutwali` → streams `kutwali_camera` + `kutwali_thermal`
- `drone1` → streams `drone1_camera` + `drone1_thermal`

| Parameter | Required | Description |
|-----------|----------|-------------|
| `host`    | **Yes**  | Pi Tailscale IP, e.g. `100.91.79.23` |
| `name`    | **Yes**  | Stream prefix & display name (e.g. `kutwali` → `kutwali_camera`, `kutwali_thermal`) |

If either `?host=` or `?name=` is missing, the page shows an error prompt.

## `.env` options

| Variable | Default | Description |
|----------|---------|-------------|
| `GO2RTC_URL` | `https://cams.drone.starwatch.bb7fab11.sslip.io` | Centralized go2rtc server URL |
| `AGENT_PORT` | `8080` | drone-agent on the Pi |
| `VIEWER_HOST` | `0.0.0.0` | Bind address |
| `VIEWER_PORT` | `8091` | Web UI port |

## What the Pi needs

Same as [Gimbal_Control](../Gimbal_Control/docs/SETUP_NEW_DRONE.md) Part 3 — **no extra install** if the Pi already runs:

- `go2rtc` with camera streams
- `drone-agent` with MAVLink bridge
- `MAVProxy` → `127.0.0.1:14550`

This viewer only **reads** telemetry and video; it does not replace the fleet dashboard.

## Copy to another device (no git)

Copy the whole `Drone_View` folder, install deps, run `__main__.py`. Point your browser to the drone's IP via the `?host=` and `?name=` query params.

Exclude `.venv` when copying — recreate on each device:

```bash
rsync -avz --exclude '.venv' Drone_View/ user@device:/path/Drone_View/
```

## vs Gimbal_Control fleet dashboard

| | **Drone_View** | **Gimbal_Control** |
|--|----------------|---------------------|
| Drones | One (URL `?host=&name=`) | Many (`drones.yaml`) |
| Video | Both feeds at once | Toggle visible/thermal |
| Gimbal / mission / params | No | Yes |
| Port | 8091 | 8090 |
