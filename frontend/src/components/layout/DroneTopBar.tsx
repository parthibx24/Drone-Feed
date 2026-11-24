import { useDrone } from "../../context/DroneContext";
import type { LinkState } from "../../types/drone";
import { TopBar, type LinkStatus } from "../dashboard/TopBar";

/** Top bar for every per-drone page: page tabs plus link health. */
export function DroneTopBar({ extraLinks = [] }: { extraLinks?: LinkStatus[] }) {
  const { id, info, snapshot, connected, can } = useDrone();
  const base = `/drones/${encodeURIComponent(id)}`;
  const droneState: LinkState = !snapshot ? "checking" : snapshot.online ? "ok" : "bad";

  return (
    <TopBar
      title={info?.name ?? id}
      // Pages the drone has no use for are left out (a DJI aircraft has no
      // parameters; Detections needs the server's AI).
      nav={[
        { to: base, label: "Dashboard", end: true },
        ...(can("mission") ? [{ to: `${base}/mission`, label: "Mission" }] : []),
        ...(can("params") ? [{ to: `${base}/params`, label: "Parameters" }] : []),
        { to: `${base}/messages`, label: "Messages" },
        ...(can("ai") ? [{ to: `${base}/detections`, label: "Detections" }] : []),
      ]}
      links={[
        { label: "Server", state: connected ? "ok" : "checking" },
        { label: "Drone", state: droneState },
        ...extraLinks,
      ]}
    />
  );
}
