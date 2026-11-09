import type { Telemetry } from "../types/drone";

export const EMPTY_VALUE = "—";

type Reading = number | null | undefined;

const CARDINALS = ["N", "NE", "E", "SE", "S", "SW", "W", "NW"];

const signed = (value: number, digits: number) =>
  `${value >= 0 ? "+" : ""}${value.toFixed(digits)}`;

const reading = (value: Reading, render: (v: number) => string) =>
  value != null ? render(value) : EMPTY_VALUE;

export const normalizeDegrees = (degrees: number) => ((degrees % 360) + 360) % 360;

export function formatBearing(degrees: Reading): string {
  return reading(degrees, (v) => `${String(Math.round(normalizeDegrees(v)) % 360).padStart(3, "0")}°`);
}

export function cardinalOf(degrees: number): string {
  return CARDINALS[Math.round(normalizeDegrees(degrees) / 45) % CARDINALS.length];
}

export function formatClock(totalSeconds: number): string {
  const seconds = Math.max(0, Math.floor(totalSeconds));
  const pad = (value: number) => String(value).padStart(2, "0");
  const hours = Math.floor(seconds / 3600);
  const clock = `${pad(Math.floor(seconds / 60) % 60)}:${pad(seconds % 60)}`;
  return hours > 0 ? `${pad(hours)}:${clock}` : clock;
}

export function formatRecordingDate(iso: string): string {
  const parts = new Intl.DateTimeFormat("en-GB", {
    day: "numeric",
    month: "short",
    year: "numeric",
  }).formatToParts(new Date(iso));
  const part = (type: string) => parts.find((p) => p.type === type)?.value ?? "";
  return `${part("day")} ${part("month")}, ${part("year")}`;
}

export function formatRecordingTime(iso: string): string {
  return new Date(iso).toLocaleTimeString("en-US", {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: true,
  });
}

export function formatTelemetryAge(timestamp: Reading): string {
  if (!timestamp) return EMPTY_VALUE;
  const age = Math.max(0, Date.now() / 1000 - timestamp);
  return age < 3 ? "live" : `${age.toFixed(0)}s ago`;
}

function formatGps(telemetry: Telemetry): string {
  const satellites = telemetry.satellites ?? 0;
  return (telemetry.gps_fix_type ?? 0) >= 3 ? `${satellites} Satellites` : `No fix (${satellites})`;
}

function formatPosition({ lat, lon }: Telemetry): string {
  return lat != null && lon != null ? `${lat.toFixed(4)}, ${lon.toFixed(4)}` : EMPTY_VALUE;
}

export interface Stat {
  label: string;
  value: string;
}

/** The stat cards, in the reading order of the design's two-column grid. */
export function buildStats(telemetry: Telemetry, zoom: number): Stat[] {
  return [
    { label: "Altitude", value: reading(telemetry.alt_m, (v) => `${v.toFixed(1)}m`) },
    { label: "AGL", value: reading(telemetry.relative_alt_m, (v) => `${v.toFixed(1)}m`) },
    { label: "Speed", value: reading(telemetry.groundspeed_m_s, (v) => `${v.toFixed(1)}m/s`) },
    { label: "Climb", value: reading(telemetry.climb_m_s, (v) => `${signed(v, 1)}m/s`) },
    { label: "Heading", value: formatBearing(telemetry.heading_deg) },
    { label: "Battery", value: reading(telemetry.battery_v, (v) => `${v.toFixed(1)}V`) },
    { label: "GPS", value: formatGps(telemetry) },
    { label: "Position", value: formatPosition(telemetry) },
    { label: "Roll", value: reading(telemetry.roll_deg, (v) => `${signed(v, 1)}°`) },
    { label: "Pitch", value: reading(telemetry.pitch_deg, (v) => `${signed(v, 1)}°`) },
    { label: "Yaw", value: formatBearing(telemetry.yaw_deg) },
    { label: "Zoom", value: `${zoom.toFixed(1)}×` },
  ];
}
