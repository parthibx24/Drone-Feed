export interface DroneParams {
  host: string;
  name: string;
}

export type LinkState = "checking" | "ok" | "bad";

export interface Telemetry {
  flight_mode?: string | null;
  armed?: boolean;
  system_status?: string | null;
  alt_m?: number | null;
  relative_alt_m?: number | null;
  groundspeed_m_s?: number | null;
  climb_m_s?: number | null;
  heading_deg?: number | null;
  battery_v?: number | null;
  battery_pct?: number | null;
  gps_fix_type?: number | null;
  satellites?: number | null;
  lat?: number | null;
  lon?: number | null;
  roll_deg?: number | null;
  pitch_deg?: number | null;
  yaw_deg?: number | null;
  zoom_level?: number | null;
  zoom?: number | null;
  timestamp?: number | null;
}

export interface Health {
  agent_ok?: boolean;
  mavlink_connected?: boolean;
}

export interface ViewerConfig {
  go2rtc_url?: string;
}

export interface CameraFeed {
  stream: string;
  label: string;
  thermal: boolean;
}
