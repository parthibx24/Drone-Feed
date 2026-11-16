import { useCallback, useEffect, useState } from "react";
import { api } from "../services/api";
import { ApiError } from "../services/http";
import type { SpeakerInfo } from "../types/gateway";

export interface SpeakerState {
  /** True once the drone is known to have a speaker API (GET answered, not 404). */
  available: boolean;
  /** Latest status; null while unknown or when the speaker service is unreachable. */
  info: SpeakerInfo | null;
  /** Why the status is missing (e.g. "pilot app not connected"), if it is. */
  problem: string | null;
  refresh: () => void;
}

/**
 * The drone's loudspeaker. Polls its status every 3 s while [active] (the
 * Speaker tab is open) and every 30 s otherwise, and not while the browser
 * tab is hidden. Drones without one answer 404: available stays false.
 */
export function useSpeaker(droneId: string, active: boolean): SpeakerState {
  const [available, setAvailable] = useState(false);
  const [info, setInfo] = useState<SpeakerInfo | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const [tick, setTick] = useState(0);
  const refresh = useCallback(() => setTick((t) => t + 1), []);

  useEffect(() => {
    let cancelled = false;
    const load = () => {
      if (document.hidden) return;
      api
        .speaker(droneId)
        .then((res) => {
          if (cancelled) return;
          setAvailable(true);
          setInfo(res);
          setProblem(null);
        })
        .catch((error: unknown) => {
          if (cancelled) return;
          if (error instanceof ApiError && error.status === 404) {
            setAvailable(false);
            setInfo(null);
            return;
          }
          if (error instanceof ApiError && error.status !== 401 && error.status !== 403) setAvailable(true);
          setInfo(null);
          setProblem(error instanceof Error ? error.message : String(error));
        });
    };
    load();
    const timer = window.setInterval(load, active ? 3000 : 30000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [droneId, active, tick]);

  return { available, info, problem, refresh };
}
