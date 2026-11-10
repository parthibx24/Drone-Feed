import type { Limits, MissionItem, MissionItemType } from "../types/gateway";

export type FieldKey = "lat" | "lon" | "alt_m" | "hold_s" | "accept_radius_m" | "seconds" | "speed_m_s" | "count";

interface ItemSpec {
  label: string;
  /** Fields the editor shows; required ones are validated. */
  fields: FieldKey[];
  required: FieldKey[];
  /** Has a map position (always, or optionally). */
  position: "always" | "optional" | "none";
}

export const ITEM_SPECS: Record<MissionItemType, ItemSpec> = {
  takeoff: { label: "Take off", fields: ["alt_m"], required: ["alt_m"], position: "none" },
  waypoint: { label: "Waypoint", fields: ["lat", "lon", "alt_m", "hold_s", "accept_radius_m"], required: ["lat", "lon", "alt_m"], position: "always" },
  loiter_time: { label: "Loiter (time)", fields: ["seconds", "lat", "lon", "alt_m"], required: ["seconds"], position: "optional" },
  speed: { label: "Change speed", fields: ["speed_m_s"], required: ["speed_m_s"], position: "none" },
  delay: { label: "Wait", fields: ["seconds"], required: ["seconds"], position: "none" },
  photo: { label: "Take photo", fields: [], required: [], position: "none" },
  photo_interval: { label: "Photos every N s", fields: ["seconds", "count"], required: ["seconds"], position: "none" },
  photo_stop: { label: "Stop photos", fields: [], required: [], position: "none" },
  video_start: { label: "Start video", fields: [], required: [], position: "none" },
  video_stop: { label: "Stop video", fields: [], required: [], position: "none" },
  roi: { label: "Point at location", fields: ["lat", "lon", "alt_m"], required: ["lat", "lon", "alt_m"], position: "always" },
  roi_none: { label: "Stop pointing", fields: [], required: [], position: "none" },
  land: { label: "Land", fields: ["lat", "lon"], required: [], position: "optional" },
  rtl: { label: "Return home", fields: [], required: [], position: "none" },
  raw: { label: "Other (raw)", fields: [], required: [], position: "none" },
};

/** Types offered in the "add item" menu, in a useful order. */
export const ADDABLE_TYPES: MissionItemType[] = [
  "takeoff", "waypoint", "loiter_time", "photo", "photo_interval", "photo_stop",
  "video_start", "video_stop", "speed", "delay", "roi", "roi_none", "land", "rtl",
];

export const FIELD_LABELS: Record<FieldKey, string> = {
  lat: "Latitude",
  lon: "Longitude",
  alt_m: "Altitude (m above home)",
  hold_s: "Hold (s)",
  accept_radius_m: "Accept radius (m)",
  seconds: "Seconds",
  speed_m_s: "Speed (m/s)",
  count: "Count (0 = until stopped)",
};

/** Label for an item's altitude, which may be above home, ground or sea level. */
export function altLabel(item: MissionItem): string {
  if (item.alt_frame === "terrain") return "Altitude (m above ground)";
  if (item.alt_frame === "absolute") return "Altitude (m above sea level)";
  return FIELD_LABELS.alt_m;
}

function altSuffix(item: MissionItem): string {
  if (item.alt_frame === "terrain") return " above ground";
  if (item.alt_frame === "absolute") return " MSL";
  return "";
}

export function hasPosition(item: MissionItem): boolean {
  return item.lat != null && item.lon != null && (item.lat !== 0 || item.lon !== 0);
}

export function describe(item: MissionItem): string {
  const spec = ITEM_SPECS[item.type];
  switch (item.type) {
    case "takeoff":
      return `${spec.label} to ${item.alt_m ?? "?"} m${altSuffix(item)}`;
    case "waypoint":
      return `${spec.label} · ${item.alt_m ?? "?"} m${altSuffix(item)}${item.hold_s ? ` · hold ${item.hold_s}s` : ""}`;
    case "loiter_time":
      return `Loiter ${item.seconds ?? "?"} s`;
    case "speed":
      return `Speed ${item.speed_m_s ?? "?"} m/s`;
    case "delay":
      return `Wait ${item.seconds ?? "?"} s`;
    case "photo_interval":
      return `Photo every ${item.seconds ?? "?"} s${item.count ? ` × ${item.count}` : ""}`;
    case "raw":
      return `Command ${item.command ?? "?"}`;
    default:
      return spec.label;
  }
}

/** Client-side checks matching the server's, so errors show before upload. */
export function validate(items: MissionItem[], limits: Limits | null): string[] {
  const errors: string[] = [];
  if (items.length === 0) errors.push("The mission is empty.");
  if (limits && items.length > limits.max_mission_items) errors.push(`At most ${limits.max_mission_items} items.`);
  items.forEach((item, i) => {
    const n = `Item ${i + 1} (${ITEM_SPECS[item.type].label})`;
    for (const key of ITEM_SPECS[item.type].required) {
      if (item[key] == null || Number.isNaN(item[key])) errors.push(`${n}: ${FIELD_LABELS[key]} is required.`);
    }
    if (item.lat != null && (item.lat < -90 || item.lat > 90)) errors.push(`${n}: latitude out of range.`);
    if (item.lon != null && (item.lon < -180 || item.lon > 180)) errors.push(`${n}: longitude out of range.`);
    if (item.lat === 0 && item.lon === 0) errors.push(`${n}: position 0,0 is not valid.`);
    // Sea-level altitudes are checked by the server against the home altitude.
    if (item.alt_m != null && limits && item.alt_frame !== "absolute" && (item.alt_m <= 0 || item.alt_m > limits.max_alt_m)) {
      errors.push(`${n}: altitude must be between 0 and ${limits.max_alt_m} m.`);
    }
    if (item.speed_m_s != null && limits && (item.speed_m_s <= 0 || item.speed_m_s > limits.max_speed_m_s)) {
      errors.push(`${n}: speed must be between 0 and ${limits.max_speed_m_s} m/s.`);
    }
  });
  return errors;
}

/** Warnings that do not block upload but usually mean a mistake. */
export function warnings(items: MissionItem[]): string[] {
  const out: string[] = [];
  if (items.length > 0 && items[0].type !== "takeoff") {
    out.push("The first item is not a takeoff: ArduCopter will not take off on its own in AUTO.");
  }
  const last = items[items.length - 1];
  if (last && last.type !== "rtl" && last.type !== "land") {
    out.push("The mission does not end with Return home or Land: the drone will hover at the last point.");
  }
  return out;
}

/** Strips download-only fields before upload. */
export function forUpload(items: MissionItem[]): MissionItem[] {
  return items.map(({ seq: _seq, ...rest }) => rest);
}
