# 08 — Web UI

**Code:** [`frontend/src/`](../frontend/src/)

It's React 19 + TypeScript, built with Vite, and uses react-router for pages and Leaflet for maps. The visual design comes from the Figma "Drone Dashboard" frame: a fixed 1880 × 1200 canvas that scales with the window through `rem` ([`styles/global.css`](../frontend/src/styles/global.css)). New pages reuse the same tokens and components.

## Pages

| URL | Page | What it is for |
|---|---|---|
| `/login` | [`pages/Login`](../frontend/src/pages/Login/) | Sign in. Every other page redirects here when not logged in, then returns. |
| `/` | [`pages/Fleet`](../frontend/src/pages/Fleet/) | All drones: online, mode, armed, battery, GPS, altitude. Updates every 3 s. Old `/?host=…&name=<prefix>` links redirect to the matching drone. |
| `/drones/:id` | [`pages/Dashboard`](../frontend/src/pages/Dashboard/) | The original dashboard (video, statistics, compass, map, gimbal pad, recordings) plus a **Flight** panel and **Go to** on the map |
| `/drones/:id/mission` | [`pages/Mission`](../frontend/src/pages/Mission/) | Mission planner |
| `/drones/:id/params` | [`pages/Params`](../frontend/src/pages/Params/) | ArduPilot parameters |
| `/drones/:id/messages` | [`pages/Messages`](../frontend/src/pages/Messages/) | Autopilot messages (pre-arm errors, GPS, EKF, failsafes) |

Every drone page shares one live WebSocket ([`context/DroneContext.tsx`](../frontend/src/context/DroneContext.tsx)), so switching tabs does not reconnect.

## Roles in the UI

| | Viewer | Operator |
|---|---|---|
| See video, telemetry, map, mission, parameters, messages, recordings | ✓ | ✓ |
| Gimbal, zoom, record | disabled | ✓ |
| Flight panel, map Go to | "View only" notice | ✓ |
| Edit, write or start missions; write parameters | read-only | ✓ |

The UI only hides or disables controls; the **server enforces roles** on every request.

## Safety confirmations

These ask for confirmation first:

- Arm
- Disarm while flying
- Take off
- Go to
- Write mission
- Delete the mission on the drone
- Start mission
- Write a parameter

Land, RTL and Pause act **immediately**, because they are what you reach for in an emergency.

Every command reports the drone's real answer:
- success: "Armed", "Mode LOITER", …
- refusal: for example "Drone refused: FAILED" when pre-arm checks fail;
- "Drone offline" or "No answer from the drone".

## Dashboard additions

| What | How it works |
|---|---|
| **Flight panel** (sidebar, "Flight" tab) | Mode selector plus Set mode, Arm and Disarm, takeoff altitude plus Take off, then Land, RTL, Pause and Resume |
| **Recordings tab** | The original "Recent Recordings" list (the full library is still under the folder button on the video) |
| **Go to** on the map | Appears for operators while the drone is armed and online. Click Go to, click the map, confirm: the drone flies there at its current altitude (GUIDED). |
| **Gimbal** | Hold-to-move now streams over the WebSocket at about 7 Hz. Releasing, closing the tab or losing the link stops the gimbal (the bridge's dead-man). |

## Mission planner

- **Map:** click to add waypoints (at the default altitude). Drag markers to move them; click a marker to select it. Shown on the map:
  - home (H),
  - the path,
  - the live drone (yellow arrow),
  - the item the drone is flying to (highlighted).
- **Item list:** add any item type after the selected item, reorder with ↑ ↓, delete with ✕. The editor below the list shows the selected item's fields.
- **Checks before upload:**
  - **Errors block the upload:** missing fields, out-of-range values, limits from `/api/v1/limits`.
  - **Warnings don't:** for example "first item is not a takeoff", or "does not end with RTL or Land".
- **Toolbar:**
  - Read from drone
  - Write to drone
  - Start (switches to AUTO)
  - Delete on drone

  A badge shows **Synced** or **Not on drone**.

## Parameters

- **Load from drone** downloads all of them (about 1,000, roughly 20 s at the current serial speed). After that the server's cache is used.
- **Search** by name.
- **New value** column (operators): type a value, then Write (or press Enter). You confirm, and the drone's echoed value is shown.

## Talking to the server

| File | Role |
|---|---|
| [`services/http.ts`](../frontend/src/services/http.ts) | `fetch` wrapper. Sends the `X-Drone-Feed` header on writes (CSRF). Turns errors into readable text. Sends you to login on a 401. |
| [`services/api.ts`](../frontend/src/services/api.ts) | Every endpoint used by the UI ([04-api.md](04-api.md)) |
| [`services/DroneSocket.ts`](../frontend/src/services/DroneSocket.ts) | Per-drone WebSocket with automatic reconnect |
| [`utils/telemetry.ts`](../frontend/src/utils/telemetry.ts) | Maps the server's telemetry onto the shape the original dashboard widgets expect, so they did not need rewriting |
| [`services/GimbalClient.ts`](../frontend/src/services/GimbalClient.ts) | Hold-to-move over the WebSocket; centre and zoom over REST |

## Build

| Command | Result |
|---|---|
| `npm run build` | Type-check (strict) + bundle into `frontend/dist/`, which the server serves (`web_dir`) |
| `npm run dev` | Hot reload on :5173, proxying `/api` and WebSockets to the server on :8091 |
| `docker build .` | Builds the UI and the server together; see the root `Dockerfile` |
