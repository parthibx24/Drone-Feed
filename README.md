# StarWatch Drone-Feed

Monitoring and control for StarWatch drones: live video, telemetry, flight control, missions, parameters and recordings, in one web app behind a login.

| Part | Runs on | Job |
|---|---|---|
| **drone-gateway** + web UI | central server (one container) | Logins and roles, the React UI, one MAVLink link per drone, commands with acknowledgements, missions, parameters, server-side recording |
| **mavlink-router** | each drone's Pi | Owns the Pixhawk serial port and routes MAVLink (replaces MAVProxy) |
| **gimbal-bridge** | each drone's Pi | Presents the Skydroid C12 as a standard MAVLink camera and gimbal (replaces the old Python drone-agent) |
| go2rtc | drone and server | Video (unchanged) |

## Documentation

Explanation is in [`docs/`](docs/); everything else is code or configuration.

| Doc | About |
|---|---|
| [01-architecture](docs/01-architecture.md) | How it fits together, repository layout, ports |
| [02-gimbal-bridge](docs/02-gimbal-bridge.md) | MAVLink camera/gimbal ↔ Skydroid translation |
| [03-drone-gateway](docs/03-drone-gateway.md) | Server internals and the configuration reference |
| [04-api](docs/04-api.md) | REST and WebSocket API, login |
| [05-deployment](docs/05-deployment.md) | Pi cutover, server switch, ArduPilot parameters, rollback, security |
| [06-testing](docs/06-testing.md) | Tests and the local sandbox (no drone needed) |
| [07-status](docs/07-status.md) | What is verified, what is not, next steps |
| [08-frontend](docs/08-frontend.md) | Pages, roles in the UI, how the UI talks to the server |

## Quick start (no drone needed)

| Step | Command |
|---|---|
| Build the UI | `cd frontend && npm ci && npm run build` |
| Fake drone | `go run ./tools/sim-vehicle -bridge 127.0.0.1:14556` |
| Fake camera bridge | `go run ./drone/cmd/gimbal-bridge -local-port 0 -gimbal-host 127.0.0.1` |
| Server | `go run ./server/cmd/drone-gateway -config gateway.yaml` (see [06-testing](docs/06-testing.md#local-sandbox)) |
| Open | `http://127.0.0.1:8091` |
| Tests | `go test ./...` and `cd frontend && npm run build` |
| Password hash for a user | `go run ./server/cmd/drone-gateway hash-password` |




