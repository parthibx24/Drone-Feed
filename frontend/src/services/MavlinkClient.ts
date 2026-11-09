import type { Telemetry } from "../types/drone";

const RECONNECT_MS = 3000;

/** Receives live telemetry over the viewer's MAVLink WebSocket proxy, reconnecting if it drops. */
export class MavlinkClient {
  private ws: WebSocket | null = null;
  private timer: ReturnType<typeof setTimeout> | undefined;
  private readonly onTelemetry: (data: Telemetry) => void;

  constructor(onTelemetry: (data: Telemetry) => void) {
    this.onTelemetry = onTelemetry;
  }

  connect(host: string): void {
    this.disconnect();
    const protocol = location.protocol === "https:" ? "wss:" : "ws:";
    const ws = new WebSocket(`${protocol}//${location.host}/ws/mavlink?host=${encodeURIComponent(host)}`);
    ws.onmessage = (event) => {
      try {
        const message = JSON.parse(event.data as string) as { type?: string; data?: Telemetry };
        if (message.type === "telemetry" && message.data) this.onTelemetry(message.data);
      } catch {
        /* ignore malformed frames */
      }
    };
    ws.onclose = () => {
      this.ws = null;
      this.timer = setTimeout(() => this.connect(host), RECONNECT_MS);
    };
    this.ws = ws;
  }

  disconnect(): void {
    clearTimeout(this.timer);
    if (this.ws) {
      this.ws.onclose = null;
      this.ws.close();
      this.ws = null;
    }
  }
}
