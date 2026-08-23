/** MAVLink WebSocket + map + attitude */

export class MavlinkClient {
  constructor(onTelemetry) {
    this.onTelemetry = onTelemetry;
    this.ws = null;
    this._timer = null;
    this._host = null;
  }

  connect(host) {
    this._host = host;
    this.disconnect();
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    const ws = new WebSocket(
      `${proto}//${location.host}/ws/mavlink?host=${encodeURIComponent(host)}`,
    );
    ws.onmessage = (ev) => {
      try {
        const msg = JSON.parse(ev.data);
        if (msg.type === "telemetry" && msg.data) this.onTelemetry(msg.data);
      } catch { /* ignore */ }
    };
    ws.onclose = () => {
      this.ws = null;
      this._timer = setTimeout(() => this.connect(this._host), 3000);
    };
    this.ws = ws;
  }

  disconnect() {
    clearTimeout(this._timer);
    if (this.ws) { this.ws.close(); this.ws = null; }
  }
}

export class AttitudeIndicator {
  constructor(canvas) {
    this.ctx = canvas.getContext("2d");
    this.canvas = canvas;
    this.roll = 0;
    this.pitch = 0;
  }

  update(roll, pitch) {
    this.roll = roll ?? 0;
    this.pitch = pitch ?? 0;
    const ctx = this.ctx;
    const w = this.canvas.width;
    const h = this.canvas.height;
    const cx = w / 2;
    const cy = h / 2;
    const r = Math.min(w, h) / 2 - 4;
    ctx.clearRect(0, 0, w, h);
    ctx.save();
    ctx.beginPath();
    ctx.arc(cx, cy, r, 0, Math.PI * 2);
    ctx.clip();
    ctx.translate(cx, cy);
    ctx.rotate((-this.roll * Math.PI) / 180);
    const pitchPx = (this.pitch / 60) * r;
    ctx.fillStyle = "#1e3a5f";
    ctx.fillRect(-w, -h + pitchPx, w * 2, h);
    ctx.fillStyle = "#5c3d1e";
    ctx.fillRect(-w, pitchPx, w * 2, h);
    ctx.strokeStyle = "#fff";
    ctx.lineWidth = 2;
    ctx.beginPath();
    ctx.moveTo(-r * 0.5, pitchPx);
    ctx.lineTo(r * 0.5, pitchPx);
    ctx.stroke();
    ctx.restore();
    ctx.strokeStyle = "#64748b";
    ctx.lineWidth = 2;
    ctx.beginPath();
    ctx.arc(cx, cy, r, 0, Math.PI * 2);
    ctx.stroke();
  }
}

export class MapTracker {
  constructor(el) {
    this.map = L.map(el, { zoomControl: false, attributionControl: false }).setView([0, 0], 2);
    L.tileLayer("https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png", { maxZoom: 19 }).addTo(this.map);
    this.marker = null;
    this.trail = L.polyline([], { color: "#00d4aa", weight: 2 }).addTo(this.map);
    this._pts = [];
  }

  update(lat, lon) {
    if (lat == null || lon == null || (lat === 0 && lon === 0)) return;
    if (!this.marker) {
      const icon = L.divIcon({
        className: "",
        html: '<div class="drone-marker"></div>',
        iconSize: [12, 12],
        iconAnchor: [6, 6],
      });
      this.marker = L.marker([lat, lon], { icon }).addTo(this.map);
      this.map.setView([lat, lon], 17);
    } else {
      this.marker.setLatLng([lat, lon]);
    }
    this._pts.push([lat, lon]);
    if (this._pts.length > 250) this._pts.shift();
    this.trail.setLatLngs(this._pts);
  }
}

export function applyTelemetry(data, els) {
  els.mode.textContent = data.flight_mode ?? "—";
  els.armed.textContent = data.armed ? "ARMED" : "DISARMED";
  els.armed.classList.toggle("is-armed", !!data.armed);

  els.alt.textContent = data.alt_m != null ? `${data.alt_m.toFixed(1)} m` : "—";
  els.relAlt.textContent = data.relative_alt_m != null ? `${data.relative_alt_m.toFixed(1)} m` : "—";
  els.speed.textContent = data.groundspeed_m_s != null ? `${data.groundspeed_m_s.toFixed(1)} m/s` : "—";
  els.climb.textContent = data.climb_m_s != null ? `${data.climb_m_s.toFixed(1)} m/s` : "—";
  els.heading.textContent = data.heading_deg != null ? `${Math.round(data.heading_deg)}°` : "—";

  if (data.battery_v != null) {
    const pct = data.battery_pct != null ? ` (${data.battery_pct}%)` : "";
    els.battery.textContent = `${data.battery_v.toFixed(1)} V${pct}`;
  } else {
    els.battery.textContent = "—";
  }

  const fix = data.gps_fix_type ?? 0;
  const sats = data.satellites ?? 0;
  els.gps.textContent = fix >= 3 ? `${sats} sats` : `No fix (${sats})`;

  if (data.lat != null && data.lon != null) {
    els.pos.textContent = `${data.lat.toFixed(5)}, ${data.lon.toFixed(5)}`;
  } else {
    els.pos.textContent = "—";
  }

  els.roll.textContent = data.roll_deg != null ? `${data.roll_deg.toFixed(1)}°` : "—";
  els.pitch.textContent = data.pitch_deg != null ? `${data.pitch_deg.toFixed(1)}°` : "—";
  
  if (data.yaw_deg != null) {
    const yawTxt = `${data.yaw_deg.toFixed(1)}°`;
    if (els.yaw) els.yaw.textContent = yawTxt;
    if (els.camYaw) els.camYaw.textContent = yawTxt;
    if (els.hudYaw) els.hudYaw.textContent = yawTxt;
  } else {
    if (els.yaw) els.yaw.textContent = "—";
  }

  if (data.zoom_level != null || data.zoom != null) {
    const z = Number(data.zoom_level ?? data.zoom);
    const zTxt = `${z.toFixed(1)}×`;
    if (els.zoom) els.zoom.textContent = zTxt;
    if (els.camZoom) els.camZoom.textContent = zTxt;
    if (els.hudZoom) els.hudZoom.textContent = zTxt;
  }

  if (data.timestamp) {
    const age = Math.max(0, (Date.now() / 1000) - data.timestamp);
    els.age.textContent = age < 3 ? "live" : `${age.toFixed(0)}s ago`;
  }

  if (data.system_status) {
    els.status.textContent = data.system_status;
  }
}
