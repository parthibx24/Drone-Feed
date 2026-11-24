# 10 — Different kinds of drone in one dashboard

One server and one dashboard serve every drone, whatever flies it: StarWatch's ArduPilot drones (star9 and the Thana drones), DJI aircraft such as the Mavic 3T through dji-bridge, and other MAVLink autopilots added later. Each drone shows only the controls it can use.

## How it works

The gateway works out, per drone, a set of **capabilities**: its profile, the features it offers, the flight modes an operator may pick, and the mission items it accepts. The code is in [`server/gateway/capabilities.go`](../server/gateway/capabilities.go).

- The **dashboard** reads them from the drone list and then from the WebSocket, so they follow changes without a reload. It shows a page, tab, panel or button only when its feature is listed.
- The **gateway** refuses a route whose feature the drone's profile rules out, with `409 "<drone> does not offer this (<feature>)"`, before anything is sent. A script or an old bookmark therefore cannot send ArduCopter mode numbers to a DJI aircraft, and cannot start a parameter download that would only wait for a timeout.

Routes and their features: [04-api.md](04-api.md#capabilities).

## Profiles

`drones[].autopilot` picks the profile. If it is unset, a drone with `bridge` is `dji`; otherwise the autopilot heartbeat decides, and the drone counts as `ardupilot` until a heartbeat arrives. That default keeps existing configs working unchanged.

| Profile | For | Offers |
|---|---|---|
| `ardupilot` | ArduCopter (star9, Thana drones) | Everything: modes, arm, take-off, land, RTL, pause/resume, go to, speed, point at, reboot, missions (all item types and altitude frames, jump to item), parameters. Gimbal, zoom and photo appear once the gimbal or camera component has been seen. |
| `dji` | A DJI aircraft through dji-bridge (Mavic 3T) | What the aircraft's link can do right now, read from dji-bridge every 5 s (below). No modes, parameters, reboot, speed change or mission jump. Missions take heights above take-off only, and no raw items. |
| `mavlink` | Another MAVLink autopilot (PX4, …) | Standard MAVLink only: arm, pause/resume, missions, parameters, and gimbal/camera when seen. No modes, take-off, land, RTL or go to: the gateway sends ArduCopter mode numbers for those, which mean something else to other autopilots. |

`drones[].features` adjusts a single drone on top of its profile:

```yaml
features:
  enable: [land, rtl]   # e.g. a PX4 drone tested to land and RTL with these routes
  disable: [params]     # e.g. hide parameters from this drone's users
```

Feature names: see `knownFeatures` in `capabilities.go`, or the `Feature` type in [`frontend/src/types/gateway.ts`](../frontend/src/types/gateway.ts). Server-side recording is always offered. The speaker comes with `speaker` or `bridge`, the Thermal and Aircraft tabs with `bridge`, and AI with the server's `ai` section.

## DJI aircraft: the link decides

dji-bridge answers `GET {bridge.api}` with the aircraft and the command kinds its current link can carry out. The gateway maps those kinds to features:

| The pilot runs | The dashboard offers |
|---|---|
| **StarWatch Pilot** on the remote controller | Take control (arm), take-off, land, return home, pause, resume, go to, point at, missions and start, gimbal, zoom, photo, recording on the card, speaker, Thermal and Aircraft tabs |
| **DJI Pilot 2** (Cloud API) | Camera only: gimbal, zoom, photo. The Flight tab says: "Open StarWatch Pilot on the remote controller to fly from here." |

When the pilot switches apps, the dashboard follows within about 5 seconds.

With **no remote controller connected** (the aircraft has not come online since dji-bridge started, or its RC left), the bridge lists no commands. That is not Pilot 2's camera-only link, so the dashboard keeps every DJI feature (the aircraft is offline anyway) and says: "No remote controller is connected for this aircraft. Open StarWatch Pilot on it to fly from here, or DJI Pilot 2 for the camera only."

If dji-bridge stops answering, every DJI feature stays offered and the aircraft refuses what its link can't do. Hiding Land or Return home because a status request failed would be worse.

For DJI, Arm reads **Take control**: dji-bridge asks the pilot on the remote controller to hand over control. Disarm reads **Release**.

## Adding a drone

| Kind | In `gateway.yaml` | Elsewhere |
|---|---|---|
| ArduPilot | `id`, `name`, `mavlink`, `stream_prefix`, as for star9 | mavlink-router (+ gimbal-bridge) on the Pi ([05-deployment.md](05-deployment.md)); a unique `MAV_SYSID` |
| DJI through dji-bridge | as above, `mavlink: tcp:<dji-bridge>:<port>` and `bridge: {api, token}` | dji-bridge in the same stack ([below](#where-dji-aircraft-run)) |
| Other MAVLink | as above, plus `autopilot: mavlink` | test each command before enabling it under `features.enable` |

## Where DJI aircraft run

The code lets one dashboard hold every kind of drone. In practice each owner gets their own instance, because Drone-Feed has no per-user drone access (Limits, below). Decided 2026-11-24:

| Instance | Drones | Files |
|---|---|---|
| star9's central server | StarWatch's ArduPilot drones only | [`deploy/dokploy`](../deploy/dokploy/) here |
| Mavic 3T instance | that owner's Mavic 3T | Mavic-3t-System: `docker-compose.yml`, `deploy/dokploy/` |
| SMP instance | SMP's DJI aircraft (Mavic 3T; other DJI models once tested), with AI detection | Mavic-3t-System: `docker-compose.smp.yml`, `deploy/smp/` |

A DJI instance runs dji-bridge, mediamtx, go2rtc, this dashboard and, for SMP, the detector in one stack. The gateway reaches dji-bridge by name (`tcp:dji-bridge:5760`), so MAVLink is never published. The compose file builds the dashboard from a pinned commit of this repository.

## Limits

- **Every login sees every drone.** Drone-Feed has no per-user drone access: anyone who can log in to an instance can watch, and operators can fly, every drone in it. This is why each owner has an instance of their own (above).
- **AI on a lens-switching camera.** The 3T sends one stream whose lens changes. The gateway tells the detector the lens the camera reports, so the thermal model runs while the thermal lens is on. On a lens change the detector restarts that stream's worker, which takes a few seconds, and tracks start again.
- **The Zoom statistic** stays on drones without a zoom; it reads 1.0×.
- `mavlink` is not flight-tested on any autopilot. Its features are the conservative set, and anything more is enabled per drone.
