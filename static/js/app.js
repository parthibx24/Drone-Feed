import { VideoPlayer } from "./video.js";
import {
  MavlinkClient,
  AttitudeIndicator,
  MapTracker,
  applyTelemetry,
} from "./mavlink.js";
import { GimbalClient } from "./gimbal.js";

const GIMBAL_SPEED = 4;

class DroneView {
  constructor() {
    this.config = null;
    this.gimbal = new GimbalClient();

    this.videoVis = new VideoPlayer(
      document.getElementById("video-visible"),
      document.getElementById("overlay-visible"),
      document.getElementById("status-visible"),
    );
    this.videoTh = new VideoPlayer(
      document.getElementById("video-thermal"),
      document.getElementById("overlay-thermal"),
      document.getElementById("status-thermal"),
    );
    this.videoVis.onStateChange = (ok) => this.setPill("conn-video-vis", ok);
    this.videoTh.onStateChange = (ok) => this.setPill("conn-video-th", ok);

    this.mavlink = new MavlinkClient((d) => this.onTelemetry(d));
    this.attitude = new AttitudeIndicator(document.getElementById("attitude"));
    this.map = new MapTracker(document.getElementById("map"));

    this.els = {
      mode: document.getElementById("hud-mode"),
      armed: document.getElementById("hud-armed"),
      status: document.getElementById("hud-status"),
      alt: document.getElementById("tel-alt"),
      relAlt: document.getElementById("tel-rel-alt"),
      speed: document.getElementById("tel-speed"),
      climb: document.getElementById("tel-climb"),
      heading: document.getElementById("tel-heading"),
      battery: document.getElementById("tel-battery"),
      gps: document.getElementById("tel-gps"),
      pos: document.getElementById("tel-pos"),
      roll: document.getElementById("tel-roll"),
      pitch: document.getElementById("tel-pitch"),
      yaw: document.getElementById("tel-yaw"),
      age: document.getElementById("tel-age"),
    };

    this.toast = document.getElementById("toast");

    this._bindControls();
    this._bindKeyboard();
  }

  _bindControls() {
    for (const btn of document.querySelectorAll(".cam-btn")) {
      const action = btn.dataset.action;
      if (action === "zoom-in" || action === "zoom-out") {
        const dir = action === "zoom-in" ? "in" : "out";
        this._bindHoldRepeat(btn, () => this.gimbal.zoom(dir).catch(() => {}));
        continue;
      }
      if (action) {
        btn.addEventListener("click", () => this.handleAction(action));
        continue;
      }

      const start = () => {
        btn.classList.add("active");
        this.gimbal.move(
          parseFloat(btn.dataset.yaw || 0),
          parseFloat(btn.dataset.pitch || 0),
          GIMBAL_SPEED,
        );
      };
      const stop = () => {
        btn.classList.remove("active");
        this.gimbal.stop().catch(() => {});
      };
      btn.addEventListener("pointerdown", (e) => {
        e.preventDefault();
        btn.setPointerCapture(e.pointerId);
        start();
      });
      btn.addEventListener("pointerup", stop);
      btn.addEventListener("pointercancel", stop);
    }
  }

  /** Fire once on press, then repeat every ~66ms while held (pointer or keyboard). */
  _bindHoldRepeat(btn, fn) {
    let timer = null;
    const start = (e) => {
      if (timer) return;
      e.preventDefault();
      try { btn.setPointerCapture(e.pointerId); } catch { /* ignore */ }
      btn.classList.add("active");
      fn();
      timer = setInterval(fn, 66);
    };
    const stop = () => {
      btn.classList.remove("active");
      if (timer) { clearInterval(timer); timer = null; }
    };
    btn.addEventListener("pointerdown", start);
    btn.addEventListener("pointerup", stop);
    btn.addEventListener("pointercancel", stop);
    btn.addEventListener("pointerleave", stop);
  }

  async handleAction(action) {
    try {
      if (action === "zoom-in") await this.gimbal.zoom("in");
      else if (action === "zoom-out") await this.gimbal.zoom("out");
      else await this.gimbal.ptz(action); // center
      this.toastMsg(action);
    } catch (e) {
      this.toastMsg(e.message);
    }
  }

  _bindKeyboard() {
    const keys = {
      ArrowUp: [0, 1],
      ArrowDown: [0, -1],
      ArrowLeft: [-1, 0],
      ArrowRight: [1, 0],
    };
    let activeKey = null;

    window.addEventListener("keydown", (e) => {
      if (e.repeat) return;
      if (document.activeElement?.matches("input, textarea, select")) return;
      if (e.code === "Space") {
        e.preventDefault();
        this.gimbal.stop();
        return;
      }
      if (e.key === "c" || e.key === "C") {
        this.gimbal.ptz("center").catch(() => {});
        return;
      }
      const dir = keys[e.key];
      if (dir && activeKey !== e.key) {
        e.preventDefault();
        activeKey = e.key;
        this.gimbal.move(dir[0], dir[1], GIMBAL_SPEED);
      }
    });

    window.addEventListener("keyup", (e) => {
      if (keys[e.key] && activeKey === e.key) {
        activeKey = null;
        this.gimbal.stop().catch(() => {});
      }
    });
  }

  toastMsg(msg) {
    if (!this.toast) return;
    this.toast.textContent = msg;
    clearTimeout(this._toastTimer);
    this._toastTimer = setTimeout(() => {
      this.toast.textContent = "";
    }, 2800);
  }

  async init() {
    const resp = await fetch("/api/config");
    this.config = await resp.json();

    document.getElementById("drone-name").textContent = this.config.name;
    document.getElementById("drone-host").textContent = this.config.host;

    this.setPill("conn-agent", null);
    this.setPill("conn-mavlink", null);
    this.setPill("conn-video-vis", null);
    this.setPill("conn-video-th", null);

    this.mavlink.connect();

    // Start telemetry/health polling immediately — must NOT depend on video,
    // otherwise a failing/slow go2rtc connect would prevent MAVLink from showing.
    this.pollHealth();
    this.pollTelemetry();
    setInterval(() => this.pollHealth(), 5000);
    setInterval(() => this.pollTelemetry(), 1000);

    // Video runs independently; swallow errors so it can't break the dashboard.
    this.videoVis
      .connect(this.config.go2rtc_url, this.config.stream)
      .catch((err) => console.warn("Visible video failed:", err));
    this.videoTh
      .connect(this.config.go2rtc_url, this.config.thermal_stream)
      .catch((err) => console.warn("Thermal video failed:", err));
  }

  async pollHealth() {
    try {
      const resp = await fetch("/api/health");
      const h = await resp.json();
      this.setPill("conn-agent", h.agent_ok !== false);
      this.setPill("conn-mavlink", !!h.mavlink_connected);
    } catch {
      this.setPill("conn-agent", false);
      this.setPill("conn-mavlink", false);
    }
  }

  async pollTelemetry() {
    try {
      const resp = await fetch("/api/telemetry");
      if (resp.ok) this.onTelemetry(await resp.json());
    } catch { /* offline */ }
  }

  onTelemetry(data) {
    applyTelemetry(data, this.els);
    this.attitude.update(data.roll_deg, data.pitch_deg);
    this.map.update(data.lat, data.lon);
    if (data.flight_mode && data.flight_mode !== "UNKNOWN") {
      this.setPill("conn-mavlink", true);
    }
  }

  setPill(id, ok) {
    const target = document.getElementById(id);
    if (!target) return;
    target.classList.remove("ok", "bad", "check");
    if (ok === null) target.classList.add("check");
    else if (ok) target.classList.add("ok");
    else target.classList.add("bad");
  }
}

new DroneView().init().catch((err) => {
  console.error(err);
  document.getElementById("status-visible").textContent = err.message;
  document.getElementById("overlay-visible").classList.remove("hidden");
});
