/** Gimbal + camera control via the viewer REST proxy (single drone). */

const BASE = "/api/gimbal";

export class GimbalClient {
  constructor() {
    this._moveTimer = null;
  }

  _jog(yaw, pitch, speed) {
    return fetch(`${BASE}/jog`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ yaw, pitch, speed }),
    });
  }

  startMove(yaw, pitch, speed) {
    this.stopMove(false);
    const tick = () => {
      this._jog(yaw, pitch, speed).catch((e) => console.warn("jog failed", e));
    };
    tick();
    this._moveTimer = setInterval(tick, 100);
  }

  async stopMove(callAgent = true) {
    if (this._moveTimer) {
      clearInterval(this._moveTimer);
      this._moveTimer = null;
    }
    if (callAgent) await this._post("/stop");
  }

  move(yaw, pitch, speed) {
    this.startMove(yaw, pitch, speed);
  }

  async stop() {
    await this.stopMove(true);
  }

  async ptz(action) {
    if (action === "stop") return this.stop();
    await this._post(`/ptz/${action}`);
  }

  photo() {
    return this._post("/photo");
  }

  record(start) {
    return this._post(`/record/${start ? "start" : "stop"}`);
  }

  zoom(direction) {
    return this._post(`/zoom/${direction}`);
  }

  zoomLevel(level) {
    return this._post("/zoom-level", { level });
  }

  goto(yaw, pitch) {
    const body = {};
    if (yaw != null && yaw !== "") body.yaw = Number(yaw);
    if (pitch != null && pitch !== "") body.pitch = Number(pitch);
    return this._post("/goto", body);
  }

  async _post(path, body) {
    const opts = { method: "POST" };
    if (body !== undefined) {
      opts.headers = { "Content-Type": "application/json" };
      opts.body = JSON.stringify(body);
    }
    const resp = await fetch(`${BASE}${path}`, opts);
    if (!resp.ok) throw new Error(`${path} → ${resp.status}`);
    return resp.json();
  }
}
