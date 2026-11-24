import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { api } from "../services/api";
import { DroneSocket } from "../services/DroneSocket";
import type { AIEvent, AIFollowState, DetectionFrame } from "../types/ai";
import type { DroneEvent, DroneSummary, Snapshot, StatusText } from "../types/gateway";

const MAX_MESSAGES = 300;
const MAX_AI_EVENTS = 200;
const NO_FOLLOW: AIFollowState = { active: false, stream: "", track_id: 0 };

/** Latest detector frame for one stream, stamped with the browser's receive time
 * (the server clock may differ from ours, so freshness uses this). */
export interface LiveDetections {
  frame: DetectionFrame;
  receivedAt: number;
}

interface DroneState {
  id: string;
  /** Static info (name, stream prefix); null until loaded, undefined if unknown. */
  info: DroneSummary | null | undefined;
  snapshot: Snapshot | null;
  /** WebSocket to the gateway is up. */
  connected: boolean;
  messages: StatusText[];
  lastReachedSeq: number | null;
  /** Latest detection frame per stream name (WebSocket "detections"). */
  detections: Record<string, LiveDetections>;
  /** Gimbal follow state (WebSocket "ai_follow"). */
  follow: AIFollowState;
  /** AI events received live since the page opened, newest first. */
  liveEvents: AIEvent[];
  setFollow: (state: AIFollowState) => void;
  /** Streams a gimbal rate update over the WebSocket (hold-to-move). */
  sendGimbalRate: (pitchDegS: number, yawDegS: number) => void;
}

const DroneContext = createContext<DroneState | null>(null);

export function DroneProvider({ id, children }: { id: string; children: ReactNode }) {
  const [info, setInfo] = useState<DroneSummary | null | undefined>(null);
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [connected, setConnected] = useState(false);
  const [messages, setMessages] = useState<StatusText[]>([]);
  const [lastReachedSeq, setLastReachedSeq] = useState<number | null>(null);
  const [detections, setDetections] = useState<Record<string, LiveDetections>>({});
  const [follow, setFollow] = useState<AIFollowState>(NO_FOLLOW);
  const [liveEvents, setLiveEvents] = useState<AIEvent[]>([]);
  const socket = useRef<DroneSocket | null>(null);

  useEffect(() => {
    let cancelled = false;
    setInfo(null);
    setSnapshot(null);
    setMessages([]);
    setDetections({});
    setFollow(NO_FOLLOW);
    setLiveEvents([]);
    api
      .drones()
      .then((list) => !cancelled && setInfo(list.find((d) => d.id === id)))
      .catch(() => !cancelled && setInfo(undefined));
    api
      .statusTexts(id)
      .then((list) => !cancelled && setMessages(list.slice(-MAX_MESSAGES)))
      .catch(() => {});

    const onEvent = (event: DroneEvent) => {
      switch (event.type) {
        case "telemetry":
          setSnapshot(event.data);
          break;
        case "statustext":
          setMessages((list) => [...list, event.data].slice(-MAX_MESSAGES));
          break;
        case "mission_item_reached":
          setLastReachedSeq(event.data.seq);
          break;
        case "link":
          setSnapshot((s) => (s ? { ...s, online: event.data.online } : s));
          break;
        case "detections": {
          const frame = event.data;
          setDetections((all) => ({ ...all, [frame.stream]: { frame, receivedAt: Date.now() } }));
          break;
        }
        case "ai_event": {
          const item = event.data;
          setLiveEvents((list) => [item, ...list.filter((e) => e.id !== item.id)].slice(0, MAX_AI_EVENTS));
          break;
        }
        case "ai_follow":
          setFollow(event.data);
          break;
      }
    };
    socket.current = new DroneSocket(id, onEvent, setConnected);
    return () => {
      cancelled = true;
      socket.current?.close();
      socket.current = null;
    };
  }, [id]);

  const sendGimbalRate = useCallback((pitchDegS: number, yawDegS: number) => {
    socket.current?.send({ type: "gimbal_rate", pitch_deg_s: pitchDegS, yaw_deg_s: yawDegS });
  }, []);

  const value = useMemo(
    () => ({
      id,
      info,
      snapshot,
      connected,
      messages,
      lastReachedSeq,
      detections,
      follow,
      liveEvents,
      setFollow,
      sendGimbalRate,
    }),
    [id, info, snapshot, connected, messages, lastReachedSeq, detections, follow, liveEvents, sendGimbalRate],
  );
  return <DroneContext.Provider value={value}>{children}</DroneContext.Provider>;
}

export function useDrone(): DroneState {
  const ctx = useContext(DroneContext);
  if (!ctx) throw new Error("useDrone outside DroneProvider");
  return ctx;
}
