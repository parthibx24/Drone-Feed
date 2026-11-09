import type { Health, Telemetry, ViewerConfig } from "../types/drone";
import type {
  Recording,
  RecordingSort,
  RecordingStartResponse,
  RecordingStatusResponse,
  RecordingStopResponse,
} from "../types/recording";

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const response = await fetch(url, init);
  if (!response.ok) {
    const body = (await response.json().catch(() => null)) as { detail?: string } | null;
    throw new Error(body?.detail ?? `HTTP ${response.status}`);
  }
  return (await response.json()) as T;
}

const param = encodeURIComponent;

export const api = {
  config: () => request<ViewerConfig>("/api/config"),
  cameras: (name: string) =>
    request<{ cameras?: string[] }>(`/api/cameras?name=${param(name)}`),
  health: (host: string) => request<Health>(`/api/health?host=${param(host)}`),
  telemetry: (host: string) => request<Telemetry>(`/api/telemetry?host=${param(host)}`),

  recordingStatus: () => request<RecordingStatusResponse>("/api/recording/status"),
  startRecording: (name: string) =>
    request<RecordingStartResponse>(`/api/recording/start?name=${param(name)}`, { method: "POST" }),
  stopRecording: (name: string) =>
    request<RecordingStopResponse>(`/api/recording/stop?name=${param(name)}`, { method: "POST" }),
  recordings: ({ sortBy, order }: RecordingSort) =>
    request<Recording[]>(`/api/recordings?sort_by=${param(sortBy)}&order=${param(order)}`),
  deleteRecording: (path: string) =>
    request<unknown>(`/api/recordings/${param(path)}`, { method: "DELETE" }),
};
