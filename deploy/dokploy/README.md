# Deploying `ui-redesign` on Dokploy

This branch is the Python backend with the React dashboard rebuilt from the Figma design. The files here deploy the whole central server: Tailscale, go2rtc, Loki, Grafana, the file manager and the dashboard.

| File | Use |
|---|---|
| [`docker-compose.yml`](docker-compose.yml) | Paste into the Dokploy Compose app (Provider: **Raw**) |

## Steps

1. **Compose:** in the Dokploy app, General → Provider → Raw, paste `docker-compose.yml`, Save.
2. **Environment tab:**

   | Variable | Required | What |
   |---|---|---|
   | `CLOUDCMD_PASSWORD` | yes | Password for the file manager (Cloud Commander), user `admin` |
   | `TS_AUTHKEY` | first deploy only | Tailscale auth key; leave empty while the `drone-tailnet` volume holds a logged-in node |

3. **Domains tab** (unchanged between branches):

   | Service | Port | Domain |
   |---|---|---|
   | `tailscale` | 8091 | dashboard (`dashboard.drone.starwatch…`) |
   | `tailscale` | 1984 | go2rtc video (`cams.drone.starwatch…`) |
   | `grafana` | 3000 | Grafana |
   | `loki` | 3100 | Loki |
   | `filemanager` | 8000 | file manager |

   `dashboard` and `go2rtc` share the `tailscale` container's network, so their domains point at `tailscale`.

4. **Deploy**, then open the `drone-dashboard` logs.

## Building from the branch or from a commit

The compose builds from the **latest commit of `ui-redesign`** at deploy time. For production, pin a commit so a later push cannot change what runs: replace `#ui-redesign` with `#<commit hash>`.

## Switching between branches

Each deployable branch has its own `deploy/dokploy/`. To switch, paste that branch's compose (and file mount, if it has one) and deploy. Volumes (recordings, Tailscale identity, go2rtc config, Grafana) are shared, so nothing is lost.

| Branch | Dashboard | Needs on the drone's Pi |
|---|---|---|
| `main` | Python Drone-Feed | the old drone-agent (port 8080) |
| `ui-redesign` | Python Drone-Feed, React UI | the old drone-agent (port 8080) |
| `feature/unified-server` | drone-gateway | mavlink-router + gimbal-bridge |
| `feature/ai-detection` | drone-gateway + detector | mavlink-router + gimbal-bridge |

The Python versions talk to the old drone-agent on the Pi. After the Pi's **final** stage install (which stops that agent), they can show video but cannot control the drone.
