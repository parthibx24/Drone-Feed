import type { Limits, MissionItemType } from "../types/gateway";
import type { FieldKey } from "./mission";

/**
 * The values offered as one-tap presets. Heights and speeds above the
 * gateway's limits are left out, so a preset is never refused.
 */
const HEIGHTS_M = [10, 20, 30, 50, 80, 120];
const SPEEDS_MS = [2, 5, 8, 12];

export function heightPresets(limits: Limits | null): number[] {
  return HEIGHTS_M.filter((h) => !limits || h <= limits.max_alt_m);
}

export function speedPresets(limits: Limits | null): number[] {
  return SPEEDS_MS.filter((s) => !limits || s <= limits.max_speed_m_s);
}

export interface FieldPresets {
  values: number[];
  unit?: string;
  label?: (value: number) => string;
}

/** Presets for one mission item field; null where typing is the only sensible input (lat/lon). */
export function fieldPresets(type: MissionItemType, key: FieldKey, limits: Limits | null): FieldPresets | null {
  switch (key) {
    case "alt_m":
      return { values: heightPresets(limits), unit: "m" };
    case "speed_m_s":
      return { values: speedPresets(limits), unit: "m/s" };
    case "hold_s":
      return { values: [0, 3, 5, 10], unit: "s", label: (v) => (v === 0 ? "None" : `${v} s`) };
    case "accept_radius_m":
      return { values: [1, 2, 5], unit: "m" };
    case "seconds":
      return type === "photo_interval" ? { values: [1, 2, 5, 10], unit: "s" } : { values: [5, 10, 30, 60], unit: "s" };
    case "count":
      return { values: [0, 5, 10, 20], label: (v) => (v === 0 ? "Until stopped" : `${v}×`) };
    default:
      return null;
  }
}

/** Item types whose height "Apply to all" sets: the points the aircraft flies to. */
export const FLOWN_TYPES: MissionItemType[] = ["waypoint", "loiter_time"];
