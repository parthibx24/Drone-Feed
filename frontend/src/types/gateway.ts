/** Types for the drone-gateway API (see docs/04-api.md). */

import type { AIEvent, AIFollowState, DetectionFrame } from "./ai";

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
  /** source: the lens the video shows, on cameras that can switch it (Mavic 3T); null otherwise. */
  camera: { present: boolean; last_seen: string; source?: CameraSource | null } | null;
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
  capabilities: Capabilities;
}

/** How the gateway talks to the drone (drones[].autopilot). */
export type Autopilot = "ardupilot" | "dji" | "mavlink";

/**
 * What a drone offers. The dashboard shows a control only when its feature
 * is listed; the gateway refuses routes whose feature the drone's profile
 * rules out (server/gateway/capabilities.go).
 */
export type Feature =
  | "arm"
  | "set_mode"
  | "takeoff"
  | "land"
  | "rtl"
  | "pause"
  | "resume"
  | "goto"
  | "speed"
  | "roi"
  | "reboot"
  | "mission"
  | "mission_start"
  | "mission_jump"
  | "params"
  | "gimbal"
  | "zoom"
  | "photo"
  | "camera_video"
  | "camera_source"
  | "recording"
  | "speaker"
  | "aircraft"
  | "ai"
  | "raw_command";

/** GET /drones/{id}/capabilities, also pushed over the WebSocket when it changes. */
export interface Capabilities {
  autopilot: Autopilot;
  /** Aircraft model when its link reports one (DJI: "M3T"). */
  model?: string;
  features: Feature[];
  /** Flight modes an operator may select (only with set_mode). */
  modes: string[];
  mission_items: MissionItemType[];
  mission_alt_frames: Array<"relative" | "terrain" | "absolute">;
  /** Why fewer features than usual are offered, to show the operator. */
  note?: string;
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
  | { type: "mission_item_reached"; data: { seq: number } }
  | { type: "capabilities"; data: Capabilities }
  | { type: "detections"; data: DetectionFrame }
  | { type: "ai_event"; data: AIEvent }
  | { type: "ai_follow"; data: AIFollowState };

/** Lenses a switchable camera can show. */
export type CameraSource = "wide" | "zoom" | "thermal";

/** Languages the speaker can say typed text in. */
export type SpeakerLang = "en" | "bn";

/** The drone's loudspeaker (GET /drones/{id}/speaker). */
export interface SpeakerInfo {
  present: boolean;
  status: "idle" | "playing" | "transmitting" | "error" | "unknown";
  volume: number | null;
  languages: SpeakerLang[];
  /** False while nothing can be played (pilot app offline, StarWatch control not granted, no speaker). */
  can_play: boolean;
}


// --- aircraft features (drones with an aircraft API: GET /drones/{id}/features; 404 otherwise)

/** Feature names the aircraft offers; the dashboard shows a control only when its feature is listed. */
export type AircraftFeature =
  | "thermal_measure"
  | "thermal_palette"
  | "thermal_gain"
  | "isotherm"
  | "obstacle_avoidance"
  | "rtk"
  | "health";

export type ThermalMeasure = "off" | "spot" | "area";

/** Positions and sizes are fractions (0..1) of the thermal image, from its top-left corner. */
export interface ThermalSpot {
  x: number;
  y: number;
  temp_c: number | null;
}

export interface ThermalArea {
  x: number;
  y: number;
  width: number;
  height: number;
  avg_c: number | null;
  min_c: number | null;
  max_c: number | null;
}

export interface Isotherm {
  enabled: boolean;
  low_c: number;
  high_c: number;
}

export interface ThermalState {
  palette: string;
  palettes: string[];
  /** "high" (finer detail, narrower range), "low" (wider range) or "auto". */
  gain: string;
  isotherm: Isotherm | null;
  measure: ThermalMeasure;
  spot: ThermalSpot | null;
  area: ThermalArea | null;
}

export type AvoidanceMode = "brake" | "bypass" | "off";

export interface AvoidanceState {
  mode: AvoidanceMode;
  /** Whether each direction is sensing. */
  horizontal: boolean;
  up: boolean;
  down: boolean;
}

export type RtkFix = "none" | "single" | "float" | "fixed";

export interface RtkState {
  present: boolean;
  enabled: boolean;
  fix: RtkFix;
  sats: number;
}

export interface AircraftFeatures {
  features: AircraftFeature[];
  thermal: ThermalState | null;
  avoidance: AvoidanceState | null;
  rtk: RtkState | null;
  /** False while the aircraft (or the pilot app carrying it) is not connected. */
  online: boolean;
}

/** The commands POST /drones/{id}/aircraft-command accepts. */
export type AircraftCommand =
  | { kind: "set_metering"; mode: ThermalMeasure }
  | { kind: "meter_spot"; x: number; y: number }
  | { kind: "meter_area"; x: number; y: number; width: number; height: number }
  | { kind: "thermal_palette"; palette: string }
  | { kind: "thermal_gain"; gain: "high" | "low" }
  | { kind: "thermal_isotherm"; enabled: boolean; low_c: number; high_c: number }
  | { kind: "obstacle_avoidance"; mode: AvoidanceMode }
  | { kind: "rtk_enable"; enabled: boolean };
