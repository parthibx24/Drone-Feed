import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../services/api";
import { ApiError, errorText } from "../services/http";
import type { AircraftCommand, AircraftFeature, AircraftFeatures } from "../types/gateway";

export interface AircraftControls {
  /** True once the drone is known to have an aircraft API (GET answered, not 404). */
  available: boolean;
  /** Latest state; null until the first answer. Kept, stale, while the service is unreachable. */
  state: AircraftFeatures | null;
  /** Why the state is stale or missing, if it is. */
  problem: string | null;
  /** True while the state is fresh and the aircraft online: controls work. */
  live: boolean;
  /** Why controls are off (aircraft offline, service unreachable), or null when live. */
  offlineReason: string | null;
  has: (feature: AircraftFeature) => boolean;
  /** The kind of the command on its way, if any. */
  pending: AircraftCommand["kind"] | null;
  /**
   * Sends a command and refreshes the state. Resolves to null when the
   * aircraft took it, or to the reason it didn't ("The pilot hasn't
   * allowed StarWatch control").
   */
  send: (command: AircraftCommand) => Promise<string | null>;
}

const POLL_MS = 2000;
/** A drone without an aircraft API keeps none; look again now and then only. */
const ABSENT_POLL_MS = 60000;

/** The server's own words for a refusal; 401/403 get the usual sign-in text. */
function reasonOf(error: unknown): string {
  if (error instanceof ApiError && error.status !== 401 && error.status !== 403) return error.message;
  return errorText(error);
}

/**
 * The aircraft's functions beyond MAVLink (thermal camera, obstacle
 * avoidance, RTK). Polls every 2 s, not while the browser tab is hidden.
 * Drones without an aircraft API answer 404: available stays false.
 */
export function useAircraftFeatures(droneId: string): AircraftControls {
  const [available, setAvailable] = useState(false);
  const [state, setState] = useState<AircraftFeatures | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const [pending, setPending] = useState<AircraftCommand["kind"] | null>(null);
  const [tick, setTick] = useState(0);
  const loadRef = useRef<() => void>(() => {});

  useEffect(() => {
    setAvailable(false);
    setState(null);
    setProblem(null);
  }, [droneId]);

  useEffect(() => {
    let cancelled = false;
    let timer = 0;
    const schedule = (ms: number) => {
      window.clearTimeout(timer);
      timer = window.setTimeout(load, ms);
    };
    const load = () => {
      if (document.hidden) return schedule(POLL_MS);
      api
        .features(droneId)
        .then((res) => {
          if (cancelled) return;
          setAvailable(true);
          setState(res);
          setProblem(null);
          schedule(POLL_MS);
        })
        .catch((error: unknown) => {
          if (cancelled) return;
          if (error instanceof ApiError && error.status === 404) {
            setAvailable(false);
            setState(null);
            return schedule(ABSENT_POLL_MS);
          }
          // Keep the last state (so the tabs stay) but say it is stale: live is false.
          if (error instanceof ApiError && error.status !== 401 && error.status !== 403) setAvailable(true);
          setProblem(reasonOf(error));
          schedule(POLL_MS);
        });
    };
    loadRef.current = () => schedule(0);
    load();
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [droneId, tick]);

  // Coming back to the browser tab: don't wait for the next poll.
  useEffect(() => {
    const onVisible = () => !document.hidden && loadRef.current();
    document.addEventListener("visibilitychange", onVisible);
    return () => document.removeEventListener("visibilitychange", onVisible);
  }, []);

  const send = useCallback(
    async (command: AircraftCommand) => {
      setPending(command.kind);
      try {
        await api.aircraftCommand(droneId, command);
        return null;
      } catch (error) {
        return reasonOf(error);
      } finally {
        setPending(null);
        setTick((t) => t + 1);
      }
    },
    [droneId],
  );

  const has = useCallback((feature: AircraftFeature) => Boolean(state?.features.includes(feature)), [state]);

  const offlineReason = problem
    ? problem
    : !state
      ? "Waiting for the aircraft's state"
      : !state.online
        ? "Aircraft offline: controls return when StarWatch Pilot reconnects"
        : null;

  return { available, state, problem, live: offlineReason === null, offlineReason, has, pending, send };
}
