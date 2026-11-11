import { useEffect, useMemo, useState } from "react";
import { useDrone } from "../context/DroneContext";
import { api } from "../services/api";
import type { AIEvent } from "../types/ai";
import { mergeEvents } from "../utils/ai";

const INITIAL_LIMIT = 100;

/** This drone's recent AI events: the stored history plus those arriving live. */
export function useAIEvents(): AIEvent[] {
  const { id, liveEvents } = useDrone();
  const [history, setHistory] = useState<AIEvent[]>([]);

  useEffect(() => {
    let cancelled = false;
    setHistory([]);
    api
      .aiEvents({ drone: id, limit: INITIAL_LIMIT })
      .then((res) => !cancelled && setHistory(res.events ?? []))
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [id]);

  return useMemo(() => mergeEvents(liveEvents, history), [liveEvents, history]);
}
