import { api } from "./api";

/** How often hold-to-move rates are re-sent. The bridge stops the gimbal if
 * updates stop for 600 ms (dead-man), so this must stay well under that. */
const RATE_INTERVAL_MS = 150;

export type ZoomDirection = "in" | "out";

/** Gimbal and camera control through drone-gateway. */
export class GimbalClient {
  private timer: ReturnType<typeof setInterval> | null = null;
  private readonly droneId: string;
  private readonly sendRate: (pitchDegS: number, yawDegS: number) => void;

  constructor(droneId: string, sendRate: (pitchDegS: number, yawDegS: number) => void) {
    this.droneId = droneId;
    this.sendRate = sendRate;
  }

  /** Starts moving: yaw/pitch are directions (-1, 0, 1), speed in deg/s. */
  move(yaw: number, pitch: number, speed: number): void {
    this.clearTimer();
    const tick = () => this.sendRate(pitch * speed, yaw * speed);
    tick();
    this.timer = setInterval(tick, RATE_INTERVAL_MS);
  }

  stop(): void {
    this.clearTimer();
    this.sendRate(0, 0);
  }

  center(): Promise<unknown> {
    return api.gimbalCenter(this.droneId);
  }

  zoom(direction: ZoomDirection): Promise<unknown> {
    return api.zoom(this.droneId, "step", direction === "in" ? 1 : -1);
  }

  zoomReset(): Promise<unknown> {
    return api.zoom(this.droneId, "range", 0);
  }

  dispose(): void {
    if (this.timer) this.stop();
  }

  private clearTimer(): void {
    if (this.timer) {
      clearInterval(this.timer);
      this.timer = null;
    }
  }
}
