import type { AICounts, AIEvent, AIGroup, Detection } from "../types/ai";

/** Boxes older than this are hidden: the detector has stopped or the stream stalled. */
export const DETECTIONS_FRESH_MS = 1500;

/** Map markers show events from this window. */
export const MAP_EVENT_WINDOW_MS = 30 * 60 * 1000;

export const GROUP_COLORS: Record<AIGroup, string> = {
  person: "#fbbf24",
  vehicle: "#00d4aa",
  other: "#a9a9a9",
};

export const FOLLOW_COLOR = "#ff3b3b";

const LABEL_NAMES: Record<string, string> = {
  person: "Person",
  car: "Car",
  van: "Van",
  truck: "Truck",
  bus: "Bus",
  bike: "Bike",
  boat: "Boat",
  aircraft: "Aircraft",
  vehicle: "Vehicle",
};

export function labelName(label: string): string {
  return LABEL_NAMES[label] ?? label.charAt(0).toUpperCase() + label.slice(1);
}

export function groupColor(group: string): string {
  return GROUP_COLORS[group as AIGroup] ?? GROUP_COLORS.other;
}

export function percent(confidence: number): string {
  return `${Math.round(confidence * 100)}%`;
}

export function countGroups(detections: Detection[]): AICounts {
  const counts: AICounts = { person: 0, vehicle: 0, other: 0 };
  for (const d of detections) {
    const group: AIGroup = d.group in counts ? d.group : "other";
    counts[group] += 1;
  }
  return counts;
}

/** "Visible" / "Thermal" from a go2rtc stream name such as "Kotowali-Thana-SMP_thermal". */
export function streamLabel(stream: string): string {
  return /(^|_)(thermal|ir)($|_)/i.test(stream) ? "Thermal" : "Visible";
}

export function formatDistance(meters: number): string {
  return meters >= 1000 ? `≈${(meters / 1000).toFixed(1)} km` : `≈${Math.round(meters)} m`;
}

export function formatEventTime(iso: string): string {
  return new Date(iso).toLocaleTimeString([], { hour12: false });
}

export function formatEventDateTime(iso: string): string {
  const date = new Date(iso);
  return `${date.toLocaleDateString([], { day: "2-digit", month: "short", year: "numeric" })} ${date.toLocaleTimeString([], {
    hour12: false,
  })}`;
}

export function mapsUrl(lat: number, lon: number): string {
  return `https://www.google.com/maps?q=${lat.toFixed(6)},${lon.toFixed(6)}`;
}

/** Newest first, without duplicates. */
export function mergeEvents(...lists: AIEvent[][]): AIEvent[] {
  const byId = new Map<string, AIEvent>();
  for (const list of lists) for (const event of list) if (!byId.has(event.id)) byId.set(event.id, event);
  return [...byId.values()].sort((a, b) => Date.parse(b.at) - Date.parse(a.at));
}
