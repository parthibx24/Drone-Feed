/** Types for the drone-gateway API (see docs/04-api.md). */

export type Role = "viewer" | "operator";

export interface Me {
  username: string;
  role: Role;
  auth_enabled: boolean;
}

export interface Position {
  lat: number;
  lon: number;
  alt_msl_m: number;
  alt_rel_m: number;
  heading_deg: number | null;
  v_north_m_s: number;
  v_east_m_s: number;
  v_down_m_s: number;
}

/** Live telemetry. Sections are null until the drone has reported them. */
export interface Snapshot {
  drone_id: string;
  name: string;
  online: boolean;
  last_heartbeat_age_s: number | null;
  system_id?: number;
  firmware?: string;
  vehicle_type?: string;
  armed: boolean;
  mode: string;
  system_status?: string;
  landed_state?: string;
  position: Position | null;
  attitude: { roll_deg: number; pitch_deg: number; yaw_deg: number } | null;
  speed: { ground_m_s: number; air_m_s: number; climb_m_s: number; throttle_pct: number } | null;
  battery: {
    voltage_v: number | null;
    current_a: number | null;
    remaining_pct: number | null;
    consumed_mah: number | null;
  } | null;
  gps: { fix_type: string; satellites: number; hdop: number | null } | null;
  home: { lat: number; lon: number; alt_msl_m: number } | null;
  mission: { current_seq: number; total: number; state?: string } | null;
  ekf: { flags: number; healthy: boolean } | null;
  camera: { present: boolean; last_seen: string } | null;
  gimbal: { present: boolean; yaw_lock: boolean; last_seen: string } | null;
  updated_at: string;
}

export interface DroneSummary {
  id: string;
  name: string;
  online: boolean;
  armed: boolean;
  mode: string;
  stream_prefix: string;
  telemetry: Snapshot;
}

export interface StatusText {
  time: string;
  severity: string;
  text: string;
}

export type MissionItemType =
  | "takeoff"
  | "waypoint"
  | "loiter_time"
  | "land"
  | "rtl"
  | "speed"
  | "delay"
  | "photo"
  | "photo_interval"
  | "photo_stop"
  | "video_start"
  | "video_stop"
  | "roi"
  | "roi_none"
  | "raw";

export interface MissionItem {
  seq?: number;
  type: MissionItemType;
  lat?: number;
  lon?: number;
  alt_m?: number;
  /** What alt_m is measured from; absent = above home. */
  alt_frame?: "relative" | "terrain" | "absolute";
  hold_s?: number;
  accept_radius_m?: number;
  seconds?: number;
  speed_m_s?: number;
  count?: number;
  command?: number;
  frame?: number;
  params?: [number, number, number, number];
  x?: number;
  y?: number;
  z?: number;
}

export interface Param {
  name: string;
  value: number;
  type: number;
  index: number;
}

export interface Limits {
  max_alt_m: number;
  max_speed_m_s: number;
  max_mission_items: number;
}

export interface CamerasResponse {
  go2rtc_url: string;
  stream_prefix: string;
  streams: string[];
  discovered: boolean;
}

/** WebSocket messages from the gateway. */
export type DroneEvent =
  | { type: "telemetry"; data: Snapshot }
  | { type: "statustext"; data: StatusText }
  | { type: "link"; data: { online: boolean } }
  | { type: "mission_item_reached"; data: { seq: number } };
