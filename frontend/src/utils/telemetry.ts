import type { Snapshot } from "../types/gateway";
import type { Telemetry } from "../types/drone";

const FIX_TYPES: Record<string, number> = {
  NO_GPS: 0,
  NO_FIX: 1,
  "2D_FIX": 2,
  "3D_FIX": 3,
  DGPS: 4,
  RTK_FLOAT: 5,
  RTK_FIXED: 6,
  STATIC: 7,
  PPP: 8,
};

/**
 * True when a position is a real fix. Without GPS, ArduPilot reports 0,0
 * ("Null Island", open ocean), which must never be shown or used as a place.
 */
export function isRealPosition(p: { lat: number; lon: number } | null | undefined): p is { lat: number; lon: number } {
  return !!p && Number.isFinite(p.lat) && Number.isFinite(p.lon) && (Math.abs(p.lat) > 0.1 || Math.abs(p.lon) > 0.1);
}

/** Maps the gateway snapshot onto the flat shape the dashboard widgets use. */
export function toTelemetry(s: Snapshot | null): Telemetry {
  if (!s) return {};
  return {
    flight_mode: s.mode,
    armed: s.armed,
    system_status: s.system_status ?? null,
    alt_m: s.position?.alt_msl_m ?? null,
    relative_alt_m: s.position?.alt_rel_m ?? null,
    groundspeed_m_s: s.speed?.ground_m_s ?? null,
    climb_m_s: s.speed?.climb_m_s ?? null,
    heading_deg: s.position?.heading_deg ?? s.attitude?.yaw_deg ?? null,
    battery_v: s.battery?.voltage_v ?? null,
    battery_pct: s.battery?.remaining_pct ?? null,
    gps_fix_type: s.gps ? (FIX_TYPES[s.gps.fix_type] ?? 0) : null,
    satellites: s.gps?.satellites ?? null,
    lat: isRealPosition(s.position) ? s.position.lat : null,
    lon: isRealPosition(s.position) ? s.position.lon : null,
    roll_deg: s.attitude?.roll_deg ?? null,
    pitch_deg: s.attitude?.pitch_deg ?? null,
    yaw_deg: s.attitude?.yaw_deg ?? null,
    timestamp: s.updated_at ? Date.parse(s.updated_at) / 1000 : null,
  };
}
