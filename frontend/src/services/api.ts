import type {
  CamerasResponse,
  DroneSummary,
  Limits,
  Me,
  MissionItem,
  Param,
  Snapshot,
  StatusText,
} from "../types/gateway";
import type {
  Recording,
  RecordingSort,
  RecordingStartResponse,
  RecordingStatusResponse,
  RecordingStopResponse,
} from "../types/recording";
import type { AIEvent, AIEventQuery, AIStatus, AnalyzeResponse } from "../types/ai";
import { request } from "./http";

function query(params: object): string {
  const entries = Object.entries(params).filter(([, v]) => v !== undefined && v !== null && v !== "");
  return entries.length ? `?${entries.map(([k, v]) => `${enc(k)}=${enc(String(v))}`).join("&")}` : "";
}

const enc = encodeURIComponent;
const drone = (id: string, path = "") => `/api/v1/drones/${enc(id)}${path}`;
const post = <T = { ok: boolean }>(url: string, json?: unknown) => request<T>(url, { method: "POST", json });

export const api = {
  // --- auth
  me: () => request<Me>("/api/auth/me"),
  login: (username: string, password: string) =>
    request<Me>("/api/auth/login", { method: "POST", json: { username, password } }),
  logout: () => post("/api/auth/logout"),

  // --- fleet + telemetry
  drones: () => request<DroneSummary[]>("/api/v1/drones"),
  snapshot: (id: string) => request<Snapshot>(drone(id)),
  statusTexts: (id: string) => request<StatusText[]>(drone(id, "/statustext")),
  modes: (id: string) => request<string[]>(drone(id, "/modes")),
  limits: () => request<Limits>("/api/v1/limits"),

  // --- flight
  arm: (id: string) => post(drone(id, "/arm")),
  disarm: (id: string) => post(drone(id, "/disarm")),
  setMode: (id: string, mode: string) => post(drone(id, "/mode"), { mode }),
  takeoff: (id: string, altM: number) => post(drone(id, "/takeoff"), { alt_m: altM }),
  land: (id: string) => post(drone(id, "/land")),
  rtl: (id: string) => post(drone(id, "/rtl")),
  pause: (id: string) => post(drone(id, "/pause")),
  resume: (id: string) => post(drone(id, "/resume")),
  goto: (id: string, lat: number, lon: number, altM: number) => post(drone(id, "/goto"), { lat, lon, alt_m: altM }),

  // --- mission
  mission: (id: string) => request<{ items: MissionItem[] }>(drone(id, "/mission")),
  uploadMission: (id: string, items: MissionItem[]) =>
    request<{ ok: boolean; count: number }>(drone(id, "/mission"), { method: "PUT", json: { items } }),
  clearMission: (id: string) => request<{ ok: boolean }>(drone(id, "/mission"), { method: "DELETE" }),
  startMission: (id: string) => post(drone(id, "/mission/start")),
  setMissionCurrent: (id: string, seq: number) => post(drone(id, "/mission/current"), { seq }),

  // --- parameters
  params: (id: string, refresh: boolean) =>
    request<{ params: Param[]; cached: boolean }>(drone(id, `/params${refresh ? "?refresh=1" : ""}`)),
  setParam: (id: string, name: string, value: number) =>
    request<{ ok: boolean; param: Param }>(drone(id, `/params/${enc(name)}`), { method: "PUT", json: { value } }),

  // --- gimbal + camera
  gimbalCenter: (id: string) => post(drone(id, "/gimbal/center")),
  gimbalMode: (id: string, mode: "follow" | "lock") => post(drone(id, "/gimbal/mode"), { mode }),
  zoom: (id: string, type: "step" | "range" | "continuous", value: number) =>
    post(drone(id, "/camera/zoom"), { type, value }),
  photo: (id: string) => post(drone(id, "/camera/photo")),
  cameras: (id: string) => request<CamerasResponse>(drone(id, "/cameras")),

  // --- recordings
  recordingStatus: () => request<RecordingStatusResponse>("/api/v1/recordings/status"),
  startRecording: (id: string) => post<RecordingStartResponse>(drone(id, "/recording/start")),
  stopRecording: (id: string) => post<RecordingStopResponse>(drone(id, "/recording/stop")),
  recordings: ({ sortBy, order }: RecordingSort) =>
    request<Recording[]>(`/api/v1/recordings?sort_by=${enc(sortBy)}&order=${enc(order)}`),
  deleteRecording: (relativePath: string) =>
    request<unknown>(`/api/v1/recordings/${relativePath.split("/").map(enc).join("/")}`, { method: "DELETE" }),

  // --- AI (docs/09-ai.md)
  aiStatus: (id: string) => request<AIStatus>(drone(id, "/ai")),
  aiSetEnabled: (id: string, enabled: boolean) => post(drone(id, "/ai"), { enabled }),
  aiAnalyze: (id: string, stream?: string, prompt?: string) =>
    post<AnalyzeResponse>(drone(id, "/ai/analyze"), { stream: stream || undefined, prompt: prompt || undefined }),
  aiFollow: (id: string, stream: string, trackId: number) => post(drone(id, "/ai/follow"), { stream, track_id: trackId }),
  aiStopFollow: (id: string) => request<{ ok: boolean }>(drone(id, "/ai/follow"), { method: "DELETE" }),
  aiEvents: (q: AIEventQuery) => request<{ events: AIEvent[] }>(`/api/v1/ai/events${query(q)}`),
};
