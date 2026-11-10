import type { DroneEvent } from "../types/gateway";

const RECONNECT_MS = 2000;

/**
 * One live WebSocket per drone page: telemetry (5 Hz), autopilot messages,
 * link changes and mission progress arrive here; gimbal hold-to-move rates go
 * out. Reconnects automatically. Auth uses the session cookie (same origin).
 */
export class DroneSocket {
  private ws: WebSocket | null = null;
  private timer: ReturnType<typeof setTimeout> | undefined;
  private closed = false;
  private readonly url: string;
  private readonly onEvent: (event: DroneEvent) => void;
  private readonly onConnection: (connected: boolean) => void;

  constructor(droneId: string, onEvent: (event: DroneEvent) => void, onConnection: (connected: boolean) => void) {
    const protocol = location.protocol === "https:" ? "wss:" : "ws:";
    this.url = `${protocol}//${location.host}/api/v1/drones/${encodeURIComponent(droneId)}/ws`;
    this.onEvent = onEvent;
    this.onConnection = onConnection;
    this.connect();
  }

  private connect(): void {
    const ws = new WebSocket(this.url);
    ws.onopen = () => this.onConnection(true);
    ws.onmessage = (message) => {
      try {
        this.onEvent(JSON.parse(message.data as string) as DroneEvent);
      } catch {
        /* ignore malformed frames */
      }
    };
    ws.onclose = () => {
      this.ws = null;
      this.onConnection(false);
      if (!this.closed) this.timer = setTimeout(() => this.connect(), RECONNECT_MS);
    };
    this.ws = ws;
  }

  /** Sends a message if connected; returns false otherwise. */
  send(message: unknown): boolean {
    if (this.ws?.readyState !== WebSocket.OPEN) return false;
    this.ws.send(JSON.stringify(message));
    return true;
  }

  close(): void {
    this.closed = true;
    clearTimeout(this.timer);
    if (this.ws) {
      this.ws.onclose = null;
      this.ws.close();
      this.ws = null;
    }
  }
}
