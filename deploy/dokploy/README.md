# Deploying `feature/ai-detection` on Dokploy

This branch is the Go drone-gateway plus AI detection (people and vehicles, thermal, events, alerts, Analyze frame, follow). The files here deploy the whole central server: Tailscale, go2rtc, Loki, Grafana, the file manager and the dashboard plus the AI detector.

| File | Use |
|---|---|
| [`docker-compose.yml`](docker-compose.yml) | Paste into the Dokploy Compose app (Provider: **Raw**) |
| [`gateway.example.yaml`](gateway.example.yaml) | Copy into a Dokploy **File Mount** with path `gateway.yaml`, then fill in the placeholders |

## Steps

1. **Compose:** in the Dokploy app, General → Provider → Raw, paste `docker-compose.yml`, Save.
2. **Environment tab:**

   | Variable | Required | What |
   |---|---|---|
   | `CLOUDCMD_PASSWORD` | yes | Password for the file manager (Cloud Commander), user `admin` |
   | `TS_AUTHKEY` | first deploy only | Tailscale auth key; leave empty while the `drone-tailnet` volume holds a logged-in node |
   | `SESSION_SECRET` | yes | Signs login cookies: `openssl rand -hex 32` |
   | `AI_INGEST_TOKEN` | yes | Shared by the dashboard and the detector: `openssl rand -hex 24` |
   | `ANTHROPIC_API_KEY` | no | Turns on "Analyze frame" |
   | `TELEGRAM_BOT_TOKEN` | no | Telegram alerts (also set `chat_id` in `gateway.yaml`) |

3. **File mount:** Advanced → Mounts → Add → File Mount, path `gateway.yaml`, content from `gateway.example.yaml`. Replace:
   - `REPLACE-WITH-BCRYPT-HASH`: run `drone-gateway hash-password` in the `drone-dashboard` terminal (Open Terminal, shell `sh`) if a gateway version is already running. Otherwise run it on any machine with Docker: `docker run --rm -it --entrypoint drone-gateway $(docker build -q https://github.com/parthibx24/Drone-Feed.git#feature/ai-detection) hash-password`.
   - `REPLACE-WITH-CHAT-ID`: the Telegram group's chat id, or delete the `telegram` line. Delete the whole `alerts` block if you don't want alerts.
   - Check the drone entries (Tailscale IP and `stream_prefix`).

4. **Domains tab** (unchanged between branches):

   | Service | Port | Domain |
   |---|---|---|
   | `tailscale` | 8091 | dashboard (`dashboard.drone.starwatch…`) |
   | `tailscale` | 1984 | go2rtc video (`cams.drone.starwatch…`) |
   | `grafana` | 3000 | Grafana |
   | `loki` | 3100 | Loki |
   | `filemanager` | 8000 | file manager |

   `dashboard` and `go2rtc` share the `tailscale` container's network, so their domains point at `tailscale`.

5. **Deploy**, then open the `drone-dashboard` logs.
   Expect `drone online` for each drone and no warning about open auth.
   Expect `AI enabled` there, and `loaded visible model` / `loaded thermal model` in the `drone-detector` logs. The dashboard's AI tab should show **Detecting** while you watch a drone.

## Building from the branch or from a commit

The compose builds from the **latest commit of `feature/ai-detection`** at deploy time. For production, pin a commit so a later push cannot change what runs: replace `#feature/ai-detection` with `#<commit hash>` in both `dashboard` and `detector` (keep `:ai/detector` after the detector's hash).

## Switching between branches

Each deployable branch has its own `deploy/dokploy/`. To switch, paste that branch's compose (and file mount, if it has one) and deploy. Volumes (recordings, Tailscale identity, go2rtc config, Grafana) are shared, so nothing is lost.

| Branch | Dashboard | Needs on the drone's Pi |
|---|---|---|
| `main` | Python Drone-Feed | the old drone-agent (port 8080) |
| `ui-redesign` | Python Drone-Feed, React UI | the old drone-agent (port 8080) |
| `feature/unified-server` | drone-gateway | mavlink-router + gimbal-bridge |
| `feature/ai-detection` | drone-gateway + detector | mavlink-router + gimbal-bridge |

Going back to a Python branch needs no clean-up: its container runs as root and can still write the recordings volume.
