import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../services/api";
import type { Recording, RecordingState, RecordingStopResponse } from "../types/recording";
import { errorText } from "../services/http";
import type { Notify } from "./useToast";

const STATUS_POLL_MS = 3000;
const nowInSeconds = () => Date.now() / 1000;

function stopMessage(result: RecordingStopResponse): string {
  if (result.status === "recording_cancelled") return result.message ?? "Recording cancelled";
  const duration = result.duration_formatted ?? `${result.duration_sec}s`;
  return `Saved ${result.filename} (${duration}, ${result.file_size_human ?? ""})`;
}

/** Server-side recording state for one drone, plus the actions that change it.
 * Recordings are keyed by the drone's stream prefix on the server. */
export function useRecording(droneId: string, name: string, notify: Notify) {
  const [state, setState] = useState<RecordingState>("idle");
  const [startTime, setStartTime] = useState<number | null>(null);
  const [listVersion, setListVersion] = useState(0);
  const busy = useRef(false);
  const previousState = useRef(state);

  const refreshList = useCallback(() => setListVersion((version) => version + 1), []);

  const applyServerState = useCallback((next: RecordingState, serverStart?: number) => {
    setState(next);
    if (next === "recording") {
      setStartTime((current) => serverStart ?? current ?? nowInSeconds());
    } else {
      setStartTime(null);
    }
  }, []);

  useEffect(() => {
    const previous = previousState.current;
    previousState.current = state;
    if (previous !== state && (state === "idle" || state === "recording")) refreshList();
  }, [state, refreshList]);

  useEffect(() => {
    const sync = () => {
      if (busy.current) return;
      api
        .recordingStatus()
        .then((response) => {
          if (busy.current) return;
          const status = response.by_drone?.[name];
          if (status?.is_recording) applyServerState(status.state as RecordingState, status.start_time);
          else applyServerState("idle");
        })
        .catch(() => {
          /* offline or server restarting */
        });
    };
    sync();
    const timer = setInterval(sync, STATUS_POLL_MS);
    return () => clearInterval(timer);
  }, [name, applyServerState]);

  const start = useCallback(async () => {
    busy.current = true;
    setState("starting");
    try {
      const result = await api.startRecording(droneId);
      const next = result.state ?? "waiting";
      applyServerState(next, result.start_time);
      notify(next === "waiting" ? "Waiting for active camera feed from go2rtc…" : "Recording started");
    } catch (error) {
      applyServerState("idle");
      notify(`Record failed: ${errorText(error)}`);
    } finally {
      busy.current = false;
    }
  }, [droneId, notify, applyServerState]);

  const stop = useCallback(async () => {
    busy.current = true;
    setState("saving");
    try {
      notify(stopMessage(await api.stopRecording(droneId)));
    } catch (error) {
      notify(`Stop record failed: ${errorText(error)}`);
    } finally {
      applyServerState("idle");
      busy.current = false;
    }
  }, [droneId, notify, applyServerState]);

  const toggle = useCallback(() => {
    if (busy.current) return;
    void (state === "recording" || state === "waiting" ? stop() : start());
  }, [state, start, stop]);

  const deleteRecording = useCallback(
    async (item: Recording) => {
      if (!window.confirm(`Delete recording '${item.filename}'?`)) return;
      try {
        await api.deleteRecording(item.relative_path || item.filename);
        notify(`Deleted ${item.filename}`);
        refreshList();
      } catch (error) {
        notify(`Delete failed: ${errorText(error)}`);
      }
    },
    [notify, refreshList],
  );

  return { state, startTime, listVersion, toggle, deleteRecording };
}

export type RecordingControls = ReturnType<typeof useRecording>;
