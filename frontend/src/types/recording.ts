export type RecordingState = "idle" | "starting" | "waiting" | "recording" | "saving";

export interface RecordingStream {
  index: number;
  role?: string;
  codec_name?: string | null;
  resolution?: string | null;
  width?: number | null;
  height?: number | null;
  fps?: number | null;
}

export interface Recording {
  filename: string;
  relative_path: string;
  drone_name: string;
  created_at: string;
  file_size_human: string;
  duration_sec: number | null;
  duration_formatted: string;
  is_active: boolean;
  streams: RecordingStream[];
  stream_count: number;
  download_url: string;
}

export interface DroneRecordingStatus {
  is_recording: boolean;
  state: "idle" | "waiting" | "recording";
  start_time?: number;
}

export interface RecordingStatusResponse {
  by_drone?: Record<string, DroneRecordingStatus>;
}

export interface RecordingStartResponse {
  state?: "waiting" | "recording";
  start_time?: number;
  message?: string;
}

export interface RecordingStopResponse {
  status: string;
  filename?: string;
  message?: string;
  duration_sec?: number;
  duration_formatted?: string;
  file_size_human?: string;
}

export type RecordingSortField = "time" | "duration" | "size";
export type SortOrder = "asc" | "desc";

export interface RecordingSort {
  sortBy: RecordingSortField;
  order: SortOrder;
}
