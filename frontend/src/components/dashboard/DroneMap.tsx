import "leaflet/dist/leaflet.css";
import { useEffect, useMemo, useRef } from "react";
import droneIcon from "../../assets/map/drone.svg";
import markerPointer from "../../assets/map/marker-pointer.svg";
import { MapTracker, type EventMarker } from "../../services/MapTracker";
import type { AIEvent } from "../../types/ai";
import { formatEventTime, groupColor, labelName, percent } from "../../utils/ai";
import type { Telemetry } from "../../types/drone";
import { Panel } from "../ui/Panel";
import styles from "./DroneMap.module.css";

const TRAIL_COLOR = "#00d4aa";

const MARKER_HTML = `
  <div class="${styles.marker}">
    <img class="${styles.pointer}" src="${markerPointer}" alt="" />
    <img class="${styles.drone}" src="${droneIcon}" alt="" />
  </div>`;

interface DroneMapProps {
  telemetry: Telemetry;
  /** When set, a "Go to" toggle appears; clicking the map then calls onGoto. */
  onGoto?: (lat: number, lon: number) => void;
  gotoActive?: boolean;
  onToggleGoto?: () => void;
  target?: [number, number] | null;
  /** AI events with an estimated ground position, drawn as small dots. */
  events?: AIEvent[];
}

export function DroneMap({ telemetry, onGoto, gotoActive = false, onToggleGoto, target = null, events }: DroneMapProps) {
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

  useEffect(() => {
    trackerRef.current?.onClick(gotoActive && onGoto ? onGoto : null);
  }, [gotoActive, onGoto]);

  useEffect(() => {
    trackerRef.current?.setTarget(target);
  }, [target]);

  // Rebuild markers only when the set of events changes, not on every telemetry render.
  const eventsKey = (events ?? []).map((e) => e.id).join(",");
  const markers = useMemo<EventMarker[]>(
    () =>
      (events ?? []).flatMap((e) =>
        e.geo
          ? [
              {
                lat: e.geo.lat,
                lon: e.geo.lon,
                color: groupColor(e.group),
                tooltip: `${labelName(e.label)} ${percent(e.confidence)} · ${formatEventTime(e.at)} (approx.)`,
              },
            ]
          : [],
      ),
    [eventsKey],
  );
  useEffect(() => {
    trackerRef.current?.setEvents(markers);
  }, [markers]);

  return (
    <Panel className={styles.panel}>
      <div ref={containerRef} className={styles.map} />
      {onToggleGoto && (
        <button
          type="button"
          className={`${styles.gotoButton} ${gotoActive ? styles.gotoActive : ""}`}
          onClick={onToggleGoto}
          title="Click the map to fly the drone there (GUIDED)"
        >
          {gotoActive ? "Click map… (cancel)" : "Go to"}
        </button>
      )}
    </Panel>
  );
}
