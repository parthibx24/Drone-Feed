import { VideoPlayer } from "./video.js";
import {
  MavlinkClient,
  AttitudeIndicator,
  MapTracker,
  applyTelemetry,
} from "./mavlink.js";
import { GimbalClient } from "./gimbal.js";

const DEFAULT_GIMBAL_SPEED = 4;

class DroneView {
  constructor() {
    this.config = null;
    this.host = null;
    this.name = null;
    this.gimbalSpeed = DEFAULT_GIMBAL_SPEED;

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

    this.currentZoom = 1.0;

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
      zoom: document.getElementById("tel-zoom"),
      camYaw: document.getElementById("cam-yaw-val"),
      camZoom: document.getElementById("cam-zoom-val"),
      hudYaw: document.getElementById("hud-yaw-val"),
      hudZoom: document.getElementById("hud-zoom-val"),
      age: document.getElementById("tel-age"),
    };

    this.toast = document.getElementById("toast");

    // ---- Recording state & elements ----
    this.isRecording = false;
    this.recStartTime = null;
    this.recTimerInterval = null;

    this.btnRecord = document.getElementById("btn-record");
    this.recLabel = document.getElementById("rec-label");
    this.recTimer = document.getElementById("rec-timer");
    this.btnRecordings = document.getElementById("btn-recordings");
    this.modalRecordings = document.getElementById("modal-recordings");
    this.modalBackdrop = document.getElementById("modal-backdrop");
    this.btnCloseRecordings = document.getElementById("btn-close-recordings");
    this.btnRefreshRecordings = document.getElementById("btn-refresh-recordings");
    this.sortSelect = document.getElementById("sort-select");
    this.recordingsList = document.getElementById("recordings-list");
    this.recordingsCount = document.getElementById("recordings-count");

    this._bindSpeedControl();
    this._bindRecordingControls();
  }

  updateZoom(level) {
    if (level != null) {
      this.currentZoom = Math.max(1, typeof level === "number" ? level : parseFloat(level) || 1);
    }
    const formatted = `${this.currentZoom.toFixed(1)}×`;
    if (this.els.zoom) this.els.zoom.textContent = formatted;
    if (this.els.camZoom) this.els.camZoom.textContent = formatted;
    if (this.els.hudZoom) this.els.hudZoom.textContent = formatted;
  }

  updateYaw(deg) {
    if (deg == null) return;
    const formatted = `${Number(deg).toFixed(1)}°`;
    if (this.els.yaw) this.els.yaw.textContent = formatted;
    if (this.els.camYaw) this.els.camYaw.textContent = formatted;
    if (this.els.hudYaw) this.els.hudYaw.textContent = formatted;
  }

  _bindSpeedControl() {
    const slider = document.getElementById("speed-slider");
    const valDisplay = document.getElementById("speed-value");
    if (!slider) return;

    slider.addEventListener("input", (e) => {
      this.gimbalSpeed = parseFloat(e.target.value) || DEFAULT_GIMBAL_SPEED;
      if (valDisplay) valDisplay.textContent = this.gimbalSpeed;
    });
  }

  _bindRecordingControls() {
    if (this.btnRecord) {
      this.btnRecord.addEventListener("click", () => this.toggleRecording());
    }
    if (this.btnRecordings) {
      this.btnRecordings.addEventListener("click", () => this.openRecordingsModal());
    }
    if (this.btnCloseRecordings) {
      this.btnCloseRecordings.addEventListener("click", () => this.closeRecordingsModal());
    }
    if (this.modalBackdrop) {
      this.modalBackdrop.addEventListener("click", () => this.closeRecordingsModal());
    }
    if (this.btnRefreshRecordings) {
      this.btnRefreshRecordings.addEventListener("click", () => this.loadRecordings());
    }
    if (this.sortSelect) {
      this.sortSelect.addEventListener("change", () => this.loadRecordings());
    }

    window.addEventListener("keydown", (e) => {
      if (e.key === "Escape" && this.modalRecordings && !this.modalRecordings.classList.contains("hidden")) {
        this.closeRecordingsModal();
      }
    });
  }

  async syncRecordingStatus() {
    try {
      const resp = await fetch("/api/recording/status");
      if (resp.ok) {
        const data = await resp.json();
        const droneStatus = (data.by_drone && this.name) ? data.by_drone[this.name] : (data.drone_name === this.name ? data : null);
        if (droneStatus && droneStatus.is_recording) {
          this.setRecordingState(droneStatus.state || "recording", droneStatus.start_time, droneStatus.init_time, droneStatus.message);
        } else if (this.isRecording) {
          this.setRecordingState("idle");
        }
      }
    } catch { /* offline or server restarting */ }
  }


  async toggleRecording() {
    if (this.btnRecord?.classList.contains("stopping")) return;
    if (this.isRecording) {
      await this.stopRecording();
    } else {
      await this.startRecording();
    }
  }

  async startRecording() {
    if (!this.name) return;
    this.recLabel.textContent = "STARTING…";
    try {
      const resp = await fetch(`/api/recording/start?name=${encodeURIComponent(this.name)}`, {
        method: "POST",
      });
      const data = await resp.json();
      if (resp.ok) {
        const state = data.state || "waiting";
        this.setRecordingState(state, data.start_time, data.init_time, data.message);
        if (state === "waiting") {
          this.toastMsg("Waiting for active camera feed from go2rtc…");
        } else {
          this.toastMsg("Recording started (Visible + Thermal MKV)");
        }
      } else {
        this.setRecordingState("idle");
        this.toastMsg(`Record failed: ${data.detail || "Server error"}`);
      }
    } catch (err) {
      this.setRecordingState("idle");
      this.toastMsg(`Record error: ${err.message}`);
    }
  }

  async stopRecording() {
    if (!this.name) return;
    if (this.btnRecord) this.btnRecord.classList.add("stopping");
    this.recLabel.textContent = "SAVING…";
    try {
      const resp = await fetch(`/api/recording/stop?name=${encodeURIComponent(this.name)}`, {
        method: "POST",
      });
      const data = await resp.json();
      this.setRecordingState("idle");
      if (resp.ok) {
        if (data.status === "recording_cancelled") {
          this.toastMsg(data.message || "Recording cancelled");
        } else {
          const dur = data.duration_formatted || `${data.duration_sec}s`;
          const sz = data.file_size_human || "";
          this.toastMsg(`Saved ${data.filename} (${dur}, ${sz})`);
        }
      } else {
        this.toastMsg(`Stop record failed: ${data.detail || "Server error"}`);
      }
    } catch (err) {
      this.setRecordingState("idle");
      this.toastMsg(`Stop error: ${err.message}`);
    } finally {
      if (this.btnRecord) this.btnRecord.classList.remove("stopping");
    }
  }

  setRecordingState(state, startTime = null, initTime = null, message = null) {
    this.recState = state;
    this.isRecording = state === "recording" || state === "waiting";

    if (!this.btnRecord) return;

    if (state === "waiting") {
      this._stopRecTimer();
      this.btnRecord.classList.remove("recording");
      this.btnRecord.classList.add("waiting");
      this.btnRecord.title = "Waiting for active camera feed from go2rtc… Click to cancel.";
      if (this.recLabel) this.recLabel.textContent = "WAITING…";
      if (this.recTimer) {
        this.recTimer.classList.add("hidden");
        this.recTimer.textContent = "00:00";
      }
    } else if (state === "recording") {
      this.recStartTime = startTime || (Date.now() / 1000);
      this.btnRecord.classList.remove("waiting");
      this.btnRecord.classList.add("recording");
      this.btnRecord.title = "Stop dual-stream recording";
      if (this.recLabel) this.recLabel.textContent = "STOP REC";
      if (this.recTimer) this.recTimer.classList.remove("hidden");
      this._startRecTimer();
    } else {
      this._stopRecTimer();
      this.btnRecord.classList.remove("recording", "waiting");
      this.btnRecord.title = "Record dual visible + thermal feeds to MKV (zero-encoding)";
      if (this.recLabel) this.recLabel.textContent = "REC";
      if (this.recTimer) {
        this.recTimer.classList.add("hidden");
        this.recTimer.textContent = "00:00";
      }
    }
  }

  setRecordingActive(active, startTime = null) {
    this.setRecordingState(active ? "recording" : "idle", startTime);
  }


  _startRecTimer() {
    this._stopRecTimer();
    const update = () => {
      const elapsed = Math.max(0, Math.floor((Date.now() / 1000) - this.recStartTime));
      const mins = Math.floor(elapsed / 60);
      const secs = elapsed % 60;
      const hrs = Math.floor(mins / 60);
      let formatted = `${String(mins % 60).padStart(2, "0")}:${String(secs).padStart(2, "0")}`;
      if (hrs > 0) {
        formatted = `${String(hrs).padStart(2, "0")}:${formatted}`;
      }
      if (this.recTimer) this.recTimer.textContent = formatted;
    };
    update();
    this.recTimerInterval = setInterval(update, 1000);
  }

  _stopRecTimer() {
    if (this.recTimerInterval) {
      clearInterval(this.recTimerInterval);
      this.recTimerInterval = null;
    }
  }

  openRecordingsModal() {
    if (this.modalRecordings) {
      this.modalRecordings.classList.remove("hidden");
      this.loadRecordings();
    }
  }

  closeRecordingsModal() {
    if (this.modalRecordings) {
      this.modalRecordings.classList.add("hidden");
    }
  }

  async loadRecordings() {
    if (!this.recordingsList) return;
    this.recordingsList.innerHTML = '<div class="empty-state">Loading recordings…</div>';

    let sortBy = "time";
    let order = "desc";
    if (this.sortSelect && this.sortSelect.value) {
      const parts = this.sortSelect.value.split("-");
      sortBy = parts[0] || "time";
      order = parts[1] || "desc";
    }

    try {
      const url = `/api/recordings?sort_by=${encodeURIComponent(sortBy)}&order=${encodeURIComponent(order)}`;
      const resp = await fetch(url);
      if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
      const items = await resp.json();

      if (this.recordingsCount) {
        this.recordingsCount.textContent = `${items.length} ${items.length === 1 ? "clip" : "clips"}`;
      }

      if (!items || items.length === 0) {
        this.recordingsList.innerHTML = '<div class="empty-state">No recordings found. Click REC to start capturing visible + thermal streams.</div>';
        return;
      }

      this.recordingsList.innerHTML = "";
      for (const item of items) {
        const el = this._createRecordingCard(item);
        this.recordingsList.appendChild(el);
      }
    } catch (err) {
      this.recordingsList.innerHTML = `<div class="empty-state">Failed to load recordings: ${err.message}</div>`;
    }
  }

  _createRecordingCard(item) {
    const card = document.createElement("div");
    card.className = `rec-item ${item.is_active ? "active-recording" : ""}`;

    const dateStr = item.created_at ? new Date(item.created_at).toLocaleString() : "—";
    const durStr = item.duration_formatted || (item.duration_sec != null ? `${item.duration_sec}s` : "—");
    const sizeStr = item.file_size_human || "—";

    let streamPillsHtml = "";
    if (Array.isArray(item.streams) && item.streams.length > 0) {
      streamPillsHtml = item.streams.map((s) => {
        const isThermal = s.role === "thermal" || s.index === 1;
        const res = s.resolution || (s.width && s.height ? `${s.width}x${s.height}` : null);
        const fpsStr = s.fps ? `${s.fps}fps` : "";
        const codecStr = s.codec_name ? s.codec_name.toUpperCase() : "";
        const label = isThermal ? "Thermal" : "Visible";
        const details = [codecStr, res, fpsStr].filter(Boolean).join(" · ");
        return `<span class="stream-pill ${isThermal ? "thermal" : ""}" title="Video Track ${s.index}">${label}: ${details}</span>`;
      }).join("");
    } else {
      streamPillsHtml = `<span class="stream-pill" title="Dual-stream copy">Dual Track (MKV)</span>`;
    }

    card.innerHTML = `
      <div class="rec-info">
        <div class="rec-title-row">
          <span class="rec-filename">${this._escapeHtml(item.filename)}</span>
          ${item.is_active ? '<span class="rec-badge live">● RECORDING NOW</span>' : ''}
          <span class="rec-badge mono">${item.drone_name || "drone"}</span>
        </div>
        <div class="rec-meta-row mono">
          <div class="rec-meta-item"><span>🕒</span> <span class="meta-val">${dateStr}</span></div>
          <div class="rec-meta-item"><span>⏱</span> <span class="meta-val">${durStr}</span></div>
          <div class="rec-meta-item"><span>💾</span> <span class="meta-val">${sizeStr}</span></div>
        </div>
        <div class="rec-streams">${streamPillsHtml}</div>
      </div>
      <div class="rec-item-actions">
        <a href="${item.download_url}" download="${item.filename}" class="btn-action btn-download" title="Download MKV video">
          ⬇ Download
        </a>
        <button class="btn-action btn-delete" title="Delete recording" ${item.is_active ? "disabled" : ""}>
          🗑
        </button>
      </div>
    `;

    const deleteBtn = card.querySelector(".btn-delete");
    if (deleteBtn && !item.is_active) {
      deleteBtn.addEventListener("click", async () => {
        const targetPath = item.relative_path || item.filename;
        if (confirm(`Delete recording '${item.filename}'?`)) {
          try {
            const resp = await fetch(`/api/recordings/${encodeURIComponent(targetPath)}`, {
              method: "DELETE",
            });
            if (resp.ok) {
              this.toastMsg(`Deleted ${item.filename}`);
              this.loadRecordings();
            } else {
              const err = await resp.json();
              this.toastMsg(`Delete failed: ${err.detail || "Server error"}`);
            }
          } catch (e) {
            this.toastMsg(`Delete error: ${e.message}`);
          }
        }
      });
    }

    return card;
  }

  _escapeHtml(str) {
    if (!str) return "";
    return String(str)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  _bindControls() {
    for (const btn of document.querySelectorAll(".cam-btn")) {
      const action = btn.dataset.action;
      if (action === "zoom-in" || action === "zoom-out") {
        const dir = action === "zoom-in" ? "in" : "out";
        this._bindHoldRepeat(btn, () => {
          this.gimbal.zoom(dir).catch(() => {});
          const next = dir === "in" ? this.currentZoom + 0.1 : Math.max(1, this.currentZoom - 0.1);
          this.updateZoom(next);
        });
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
          this.gimbalSpeed,
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
      if (action === "zoom-in") {
        await this.gimbal.zoom("in");
        this.updateZoom(this.currentZoom + 0.5);
      } else if (action === "zoom-out") {
        await this.gimbal.zoom("out");
        this.updateZoom(Math.max(1, this.currentZoom - 0.5));
      } else if (action === "zoom-reset") {
        await this.gimbal.zoomReset();
        this.updateZoom(1.0);
      } else {
        await this.gimbal.ptz(action); // center
      }
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
      if (e.key === "1" || e.key === "z" || e.key === "Z") {
        this.handleAction("zoom-reset").catch(() => {});
        return;
      }
      if (e.key === "r" || e.key === "R") {
        // Toggle recording with hotkey 'R'
        this.toggleRecording();
        return;
      }
      const dir = keys[e.key];
      if (dir && activeKey !== e.key) {
        e.preventDefault();
        activeKey = e.key;
        this.gimbal.move(dir[0], dir[1], this.gimbalSpeed);
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
    // ---- Read drone host & name from URL query params ----
    const params = new URLSearchParams(window.location.search);
    const rawHost = params.get("host");
    const rawName = params.get("name");
    this.host = rawHost ? rawHost.trim().replace(/^["']|["']$/g, "") : null;
    this.name = rawName ? rawName.trim().replace(/^["']|["']$/g, "") : null;

    if (!this.host || !this.name) {
      const missing = !this.host && !this.name ? "host and name"
        : !this.host ? "host" : "name";
      const msg = `Missing ?${missing}= query parameter.\n\nOpen this page as:\n` +
        window.location.origin + "/?host=<DRONE_IP>&name=<DRONE_NAME>";
      document.getElementById("status-visible").textContent =
        `Missing ?${missing}= in URL`;
      document.getElementById("overlay-visible").classList.remove("hidden");
      document.getElementById("status-thermal").textContent =
        "Add ?host=<IP>&name=<NAME> to the URL";
      document.getElementById("overlay-thermal").classList.remove("hidden");
      document.getElementById("drone-name").textContent = "No Drone";
      document.getElementById("drone-host").textContent =
        "Add ?host=<IP>&name=<NAME> to the URL";
      throw new Error(msg);
    }

    // ---- Fetch server-side config (stream names, ports) ----
    const resp = await fetch("/api/config");
    this.config = await resp.json();

    // ---- Set header from query params ----
    document.getElementById("drone-name").textContent = this.name;
    document.getElementById("drone-host").textContent = this.host;

    // ---- go2rtc URL from server config; stream names from name prefix ----
    const go2rtcUrl = (this.config.go2rtc_url || "").trim().replace(/^["']|["']$/g, "").replace(/\/+$/, "");
    const visibleStream = `${this.name}_camera`;
    const thermalStream = `${this.name}_thermal`;

    // ---- Initialise gimbal client with host ----
    this.gimbal = new GimbalClient(this.host);

    this._bindControls();
    this._bindKeyboard();

    this.setPill("conn-agent", null);
    this.setPill("conn-mavlink", null);
    this.setPill("conn-video-vis", null);
    this.setPill("conn-video-th", null);

    this.mavlink.connect(this.host);

    // Start telemetry/health polling immediately — must NOT depend on video,
    // otherwise a failing/slow go2rtc connect would prevent MAVLink from showing.
    this.pollHealth();
    this.pollTelemetry();
    this.syncRecordingStatus();
    setInterval(() => this.pollHealth(), 5000);
    setInterval(() => this.pollTelemetry(), 1000);
    setInterval(() => this.syncRecordingStatus(), 3000);

    // Video runs independently; swallow errors so it can't break the dashboard.
    this.videoVis
      .connect(go2rtcUrl, visibleStream)
      .catch((err) => console.warn("Visible video failed:", err));
    this.videoTh
      .connect(go2rtcUrl, thermalStream)
      .catch((err) => console.warn("Thermal video failed:", err));
  }

  async pollHealth() {
    try {
      const resp = await fetch(`/api/health?host=${encodeURIComponent(this.host)}`);
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
      const resp = await fetch(`/api/telemetry?host=${encodeURIComponent(this.host)}`);
      if (resp.ok) this.onTelemetry(await resp.json());
    } catch { /* offline */ }
  }

  onTelemetry(data) {
    applyTelemetry(data, this.els);
    if (data.zoom_level != null || data.zoom != null) {
      this.currentZoom = Number(data.zoom_level ?? data.zoom);
    }
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
});

