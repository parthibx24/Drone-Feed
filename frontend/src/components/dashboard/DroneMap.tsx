import "leaflet/dist/leaflet.css";
import { useEffect, useRef } from "react";
import droneIcon from "../../assets/map/drone.svg";
import markerPointer from "../../assets/map/marker-pointer.svg";
import { MapTracker } from "../../services/MapTracker";
import type { Telemetry } from "../../types/drone";
import { Panel } from "../ui/Panel";
import styles from "./DroneMap.module.css";

const TRAIL_COLOR = "#00d4aa";

const MARKER_HTML = `
  <div class="${styles.marker}">
    <img class="${styles.pointer}" src="${markerPointer}" alt="" />
    <img class="${styles.drone}" src="${droneIcon}" alt="" />
  </div>`;

export function DroneMap({ telemetry }: { telemetry: Telemetry }) {
  const containerRef = useRef<HTMLDivElement>(null);
  const trackerRef = useRef<MapTracker | null>(null);
  const { lat, lon, heading_deg: heading } = telemetry;

  useEffect(() => {
    const tracker = new MapTracker(containerRef.current!, MARKER_HTML, TRAIL_COLOR);
    trackerRef.current = tracker;
    return () => {
      tracker.destroy();
      trackerRef.current = null;
    };
  }, []);

  useEffect(() => {
    trackerRef.current?.update(lat, lon, heading ?? 0);
  }, [lat, lon, heading]);

  return (
    <Panel className={styles.panel}>
      <div ref={containerRef} className={styles.map} />
    </Panel>
  );
}
