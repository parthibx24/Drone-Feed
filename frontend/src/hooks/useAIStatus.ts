import { useCallback, useEffect, useState } from "react";
import { api } from "../services/api";
import { ApiError, errorText } from "../services/http";
import type { AIStatus } from "../types/ai";

const POLL_MS = 4000;

export interface AIStatusState {
  status: AIStatus | null;
  /** Why the status is unavailable (e.g. the server has no AI support). */
  error: string | null;
  refresh: () => void;
}

/** Polls the drone's AI status (detector health, per-stream rates, features). */
export function useAIStatus(droneId: string): AIStatusState {
  const [status, setStatus] = useState<AIStatus | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [tick, setTick] = useState(0);

  const refresh = useCallback(() => setTick((t) => t + 1), []);

  useEffect(() => {
    let cancelled = false;
    const load = () =>
      api
        .aiStatus(droneId)
        .then((next) => {
          if (cancelled) return;
          setStatus(next);
          setError(null);
        })
        .catch((err: unknown) => {
          if (cancelled) return;
          setStatus(null);
          setError(err instanceof ApiError && err.status === 404 ? "AI is not available on this server." : errorText(err));
        });
    void load();
    const timer = setInterval(load, POLL_MS);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [droneId, tick]);

  return { status, error, refresh };
}
