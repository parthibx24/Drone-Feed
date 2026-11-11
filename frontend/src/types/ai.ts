/** Types for the AI endpoints and WebSocket events (see docs/09-ai.md). */

export type AIGroup = "person" | "vehicle" | "other";
export type AIKind = "visible" | "thermal";

/** Normalised labels the detector reports. */
export const AI_LABELS = ["person", "car", "van", "truck", "bus", "bike", "boat", "aircraft", "vehicle"] as const;

/** [x, y, w, h], 0–1, from the top-left corner of the frame. */
export type NormBox = [number, number, number, number];

export interface Detection {
  track_id: number;
  label: string;
  group: AIGroup;
  confidence: number;
  box: NormBox;
}

/** One processed frame from the detector. */
export interface DetectionFrame {
  drone_id: string;
  stream: string;
  kind: AIKind;
  ts: string;
  width: number;
  height: number;
  model: string;
  infer_ms: number;
  detections: Detection[];
}

export interface AICounts {
  person: number;
  vehicle: number;
  other: number;
}

export interface AIStreamStatus {
  kind: AIKind;
  fps: number;
  infer_ms: number;
  counts: AICounts;
  frame: DetectionFrame | null;
}

export interface AIFollowState {
  active: boolean;
  stream: string;
  track_id: number;
  /** Set when following stops (e.g. "track lost"). */
  reason?: string;
}

export interface AIStatus {
  enabled: boolean;
  configured: boolean;
  detector_online: boolean;
  /** The detector is currently asked to process this drone (armed or watched). */
  active: boolean;
  last_frame_at: string | null;
  streams: Record<string, AIStreamStatus>;
  follow: AIFollowState;
  features: { analyze: boolean; alerts: boolean; auto_record: boolean; follow: boolean };
}

export interface AIEvent {
  id: string;
  drone_id: string;
  drone_name: string;
  stream: string;
  kind: AIKind;
  at: string;
  track_id: number;
  label: string;
  group: AIGroup;
  confidence: number;
  box: NormBox;
  drone: { lat: number; lon: number; alt_rel_m: number; heading_deg: number | null } | null;
  geo: { lat: number; lon: number; distance_m: number; approx: boolean } | null;
  snapshot_url: string;
  alerted: boolean;
  recording: boolean;
}

export interface AIEventQuery {
  drone?: string;
  group?: string;
  label?: string;
  since?: string;
  until?: string;
  limit?: number;
}

export interface AnalyzeResponse {
  ok: boolean;
  text: string;
  stream: string;
  model: string;
  at: string;
}
