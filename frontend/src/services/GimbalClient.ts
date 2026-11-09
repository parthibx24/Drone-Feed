const BASE = "/api/gimbal";
const JOG_INTERVAL_MS = 100;

export type ZoomDirection = "in" | "out";

/** Gimbal and camera control through the viewer's REST proxy. */
export class GimbalClient {
  private moveTimer: ReturnType<typeof setInterval> | null = null;
  private jogInFlight = false;
  private readonly host: string;

  constructor(host: string) {
    this.host = host;
  }

  move(yaw: number, pitch: number, speed: number): void {
    this.clearMoveTimer();
    const tick = () => {
      this.jog(yaw, pitch, speed).catch((error) => console.warn("jog failed", error));
    };
    tick();
    this.moveTimer = setInterval(tick, JOG_INTERVAL_MS);
  }

  async stop(): Promise<void> {
    this.clearMoveTimer();
    await this.post("/stop");
  }

  center(): Promise<void> {
    return this.post("/ptz/center");
  }

  zoom(direction: ZoomDirection): Promise<void> {
    return this.post(`/zoom/${direction}`);
  }

  zoomReset(): Promise<void> {
    return this.post("/zoom-level", { level: 1 });
  }

  private url(path: string): string {
    return `${BASE}${path}?host=${encodeURIComponent(this.host)}`;
  }

  private clearMoveTimer(): void {
    if (this.moveTimer) {
      clearInterval(this.moveTimer);
      this.moveTimer = null;
    }
  }

  /** Skips a tick while the previous jog is still in flight, to avoid request pile-up. */
  private async jog(yaw: number, pitch: number, speed: number): Promise<void> {
    if (this.jogInFlight) return;
    this.jogInFlight = true;
    try {
      await fetch(this.url("/jog"), {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ yaw, pitch, speed }),
      });
    } finally {
      this.jogInFlight = false;
    }
  }

  private async post(path: string, body?: unknown): Promise<void> {
    const init: RequestInit = { method: "POST" };
    if (body !== undefined) {
      init.headers = { "Content-Type": "application/json" };
      init.body = JSON.stringify(body);
    }
    const response = await fetch(this.url(path), init);
    if (!response.ok) throw new Error(`${path} → ${response.status}`);
  }
}
