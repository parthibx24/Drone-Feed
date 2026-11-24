import type { ReactNode } from "react";
import { Link } from "react-router";
import { useDrone } from "../../context/DroneContext";
import type { Feature } from "../../types/gateway";
import { Panel, PanelTitle } from "../ui/Panel";
import { DroneTopBar } from "./DroneTopBar";
import { PageFrame } from "./PageFrame";
import styles from "./RequireFeature.module.css";

/**
 * Shows a drone page only if the drone offers its feature (a DJI aircraft
 * has no parameters). Until the capabilities are known the page is shown:
 * the gateway refuses what the drone can't do anyway.
 */
export function RequireFeature({ feature, what, children }: { feature: Feature; what: string; children: ReactNode }) {
  const { id, info, capabilities, can } = useDrone();
  if (!capabilities || can(feature)) return <>{children}</>;
  return (
    <PageFrame header={<DroneTopBar />}>
      <Panel className={styles.panel}>
        <PanelTitle>Not available on this drone</PanelTitle>
        <p className={styles.text}>
          {info?.name ?? id}
          {capabilities.model ? ` (DJI ${capabilities.model})` : ""} does not offer {what}.
        </p>
        <Link className={styles.back} to={`/drones/${encodeURIComponent(id)}`}>
          Back to the dashboard
        </Link>
      </Panel>
    </PageFrame>
  );
}
