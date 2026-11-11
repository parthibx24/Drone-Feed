import { useDrone } from "../../context/DroneContext";
import type { LinkState } from "../../types/drone";
import { TopBar, type LinkStatus } from "../dashboard/TopBar";

/** Top bar for every per-drone page: page tabs plus link health. */
export function DroneTopBar({ extraLinks = [] }: { extraLinks?: LinkStatus[] }) {
  const { id, info, snapshot, connected } = useDrone();
  const base = `/drones/${encodeURIComponent(id)}`;
  const droneState: LinkState = !snapshot ? "checking" : snapshot.online ? "ok" : "bad";

  return (
    <TopBar
      title={info?.name ?? id}
      nav={[
        { to: base, label: "Dashboard", end: true },
        { to: `${base}/mission`, label: "Mission" },
        { to: `${base}/params`, label: "Parameters" },
        { to: `${base}/messages`, label: "Messages" },
        { to: `${base}/detections`, label: "Detections" },
      ]}
      links={[
        { label: "Server", state: connected ? "ok" : "checking" },
        { label: "Drone", state: droneState },
        ...extraLinks,
      ]}
    />
  );
}
