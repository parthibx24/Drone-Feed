import "leaflet/dist/leaflet.css";
import L from "leaflet";
import { useEffect, useRef } from "react";
import type { MissionItem } from "../../types/gateway";
import { hasPosition } from "../../utils/mission";
import styles from "./MissionMap.module.css";

const TILES = "https://server.arcgisonline.com/ArcGIS/rest/services/World_Imagery/MapServer/tile/{z}/{y}/{x}";

interface MissionMapProps {
  items: MissionItem[];
  selected: number | null;
  /** Index of the item the drone is currently flying to (from telemetry). */
  activeIndex: number | null;
  home: [number, number] | null;
  drone: { lat: number; lon: number; heading: number } | null;
  addOnClick: boolean;
  editable: boolean;
  onAdd: (lat: number, lon: number) => void;
  onMove: (index: number, lat: number, lon: number) => void;
  onSelect: (index: number) => void;
}

/** Satellite map with numbered, draggable mission markers and the flight path. */
export function MissionMap(props: MissionMapProps) {
  const { items, selected, activeIndex, home, drone, addOnClick, editable } = props;
  const el = useRef<HTMLDivElement>(null);
  const map = useRef<L.Map | null>(null);
  const layer = useRef<L.LayerGroup | null>(null);
  const droneMarker = useRef<L.Marker | null>(null);
  const centered = useRef(false);
  const handlers = useRef(props);
  handlers.current = props;

  useEffect(() => {
    const m = L.map(el.current!, { zoomControl: true }).setView([23.8, 90.4], 5);
    m.attributionControl.setPrefix(false);
    L.tileLayer(TILES, { maxZoom: 20, maxNativeZoom: 19, attribution: "Imagery © Esri" }).addTo(m);
    layer.current = L.layerGroup().addTo(m);
    m.on("click", (e: L.LeafletMouseEvent) => {
      if (handlers.current.addOnClick && handlers.current.editable) handlers.current.onAdd(e.latlng.lat, e.latlng.lng);
    });
    map.current = m;
    return () => {
      m.remove();
      map.current = null;
    };
  }, []);

  useEffect(() => {
    if (el.current) el.current.style.cursor = addOnClick && editable ? "crosshair" : "";
  }, [addOnClick, editable]);

  // Rebuild markers + path whenever the mission changes.
  useEffect(() => {
    const group = layer.current;
    const m = map.current;
    if (!group || !m) return;
    group.clearLayers();
    const path: L.LatLngTuple[] = home ? [home] : [];

    if (home) {
      L.marker(home, {
        icon: L.divIcon({ className: "", html: `<div class="${styles.home}">H</div>`, iconSize: [0, 0] }),
        interactive: false,
      }).addTo(group);
    }

    items.forEach((item, index) => {
      if (!hasPosition(item)) return;
      const pos: L.LatLngTuple = [item.lat!, item.lon!];
      if (item.type !== "roi") path.push(pos);
      const cls = [
        styles.pin,
        item.type === "roi" ? styles.roi : "",
        index === selected ? styles.selected : "",
        index === activeIndex ? styles.active : "",
      ].join(" ");
      const marker = L.marker(pos, {
        draggable: editable,
        icon: L.divIcon({ className: "", html: `<div class="${cls}">${index + 1}</div>`, iconSize: [0, 0] }),
      }).addTo(group);
      marker.on("click", () => handlers.current.onSelect(index));
      marker.on("dragend", () => {
        const ll = marker.getLatLng();
        handlers.current.onMove(index, ll.lat, ll.lng);
      });
    });
    if (path.length > 1) L.polyline(path, { color: "#00d4aa", weight: 3, opacity: 0.9 }).addTo(group);

    if (!centered.current && path.length > 0) {
      if (path.length > 1) m.fitBounds(L.latLngBounds(path), { padding: [60, 60], maxZoom: 18 });
      else m.setView(path[0], 17);
      centered.current = true;
    }
    // Live drone position deliberately not a dependency: rebuilding markers
    // 5x a second would interrupt dragging.
  }, [items, selected, activeIndex, home, editable]);

  // Live drone position (and initial centering when there is no mission yet).
  useEffect(() => {
    const m = map.current;
    if (!m) return;
    if (drone && !centered.current) {
      m.setView([drone.lat, drone.lon], 17);
      centered.current = true;
    }
    if (!drone) {
      droneMarker.current?.remove();
      droneMarker.current = null;
      return;
    }
    const html = `<div class="${styles.drone}" style="--heading:${drone.heading}deg"></div>`;
    if (!droneMarker.current) {
      droneMarker.current = L.marker([drone.lat, drone.lon], {
        icon: L.divIcon({ className: "", html, iconSize: [0, 0] }),
        interactive: false,
        zIndexOffset: 1000,
      }).addTo(m);
    } else {
      droneMarker.current.setLatLng([drone.lat, drone.lon]);
      droneMarker.current.setIcon(L.divIcon({ className: "", html, iconSize: [0, 0] }));
    }
  }, [drone]);

  return <div ref={el} className={styles.map} />;
}
