/** go2rtc WebRTC / MSE / iframe player with auto-recovery */

export class VideoPlayer {
  constructor(videoEl, overlayEl, statusEl) {
    this.video = videoEl;
    this.overlay = overlayEl;
    this.status = statusEl;
    this.wrap = videoEl.parentElement;
    this.pc = null;
    this._abort = null;
    this._iframe = null;
    this._watchdog = null;
    this._lastConnect = null;
    this._reconnecting = false;
    this._lastFrameAt = 0;
    this._lastCurrentTime = -1;
    this._iframeLoadedAt = 0;
    this._frameCallbackId = null;
    this.onStateChange = null;

    // --- Transport label + FPS tracking ---
    this._transport = null;   // "RTC" | "MSE" | "IFRAME" | null
    this._fpsFrameCount = 0;
    this._fpsLastSample = 0;
    this._currentFps = 0;
    this._fpsTimer = null;
    this._badgeEl = null;     // set externally via setBadge()

    this._onVideoStall = () => this._scheduleReconnect("video stalled");
    this._onVideoError = () => this._scheduleReconnect("video error");
    this.video.addEventListener("stalled", this._onVideoStall);
    this.video.addEventListener("error", this._onVideoError);
    this.video.addEventListener("emptied", this._onVideoError);

    document.addEventListener("visibilitychange", () => {
      if (document.visibilityState === "visible" && this._lastConnect) {
        setTimeout(() => this._checkHealth(), 500);
      }
    });
  }

  /** Bind a badge element (should contain .badge-transport and .badge-fps children) */
  setBadge(badgeEl) {
    this._badgeEl = badgeEl;
  }

  /** @returns {Promise<boolean>} true when playback is verified */
  async connect(go2rtcUrl, streamName, { retries = 2, silent = false } = {}) {
    this._lastConnect = { go2rtcUrl, streamName };
    this._stopWatchdog();

    for (let attempt = 0; attempt <= retries; attempt++) {
      if (attempt > 0) {
        if (!silent) this._show(`Retrying ${streamName} (${attempt}/${retries})…`);
        await this._sleep(1500 * attempt);
      }

      try {
        const ok = await this._connectOnce(go2rtcUrl, streamName, silent);
        if (ok) {
          this._hide();
          this._lastFrameAt = Date.now();
          this._startFrameMonitor();
          this._startFpsSampler();
          this._notifyState(true);
          this._startWatchdog();
          return true;
        }
      } catch (err) {
        console.warn(`Video connect attempt ${attempt + 1} failed:`, err);
        if (attempt === retries) {
          if (!silent) this._show(`Video error: ${err.message}`);
          this._notifyState(false);
          throw err;
        }
      }
    }

    if (!silent) this._show(`No video from ${streamName}`);
    this._notifyState(false);
    return false;
  }

  disconnect({ clearTarget = true } = {}) {
    this._stopWatchdog();
    this._stopFrameMonitor();
    this._stopFpsSampler();
    if (this._abort) {
      this._abort.abort();
      this._abort = null;
    }
    if (this.pc) {
      this.pc.close();
      this.pc = null;
    }
    if (this._iframe) {
      this._iframe.remove();
      this._iframe = null;
    }
    this.video.style.display = "";
    // Keep last frame: don't clear srcObject/src during reconnects
    if (clearTarget) {
      this.video.srcObject = null;
      this.video.removeAttribute("src");
      this.video.load();
      this._lastConnect = null;
      this._setTransport(null);
    }
  }

  async _connectOnce(go2rtcUrl, streamName, silent = false) {
    this.disconnect({ clearTarget: false });
    if (!silent) this._show(`Connecting to ${streamName}…`);

    const base = go2rtcUrl.replace(/\/$/, "");
    this._abort = new AbortController();
    this._lastConnect = { go2rtcUrl, streamName };

    // Native WebRTC first — we can monitor and auto-recover it
    try {
      await this._tryWebRTC(base, streamName);
      if (await this._waitForFrames(8000)) {
        this._setTransport("RTC");
        return true;
      }
      console.warn("WebRTC connected but no frames — trying iframe");
    } catch (webrtcErr) {
      console.warn("WebRTC failed:", webrtcErr);
    }

    try {
      await this._tryIframe(base, streamName);
      await this._sleep(2000);
      if (await this._probeStream(base, streamName)) {
        this._iframeLoadedAt = Date.now();
        this._setTransport("IFRAME");
        return true;
      }
      console.warn("Iframe loaded but stream inactive — trying MSE");
    } catch (iframeErr) {
      console.warn("Iframe failed:", iframeErr);
    }

    try {
      await this._tryMSE(base, streamName);
      if (await this._waitForFrames(6000)) {
        this._setTransport("MSE");
        return true;
      }
    } catch (mseErr) {
      console.warn("MSE failed:", mseErr);
    }

    return false;
  }

  async _tryWebRTC(base, src) {
    this._showWebRTC();

    const pc = new RTCPeerConnection({
      iceServers: [{ urls: "stun:stun.l.google.com:19302" }],
    });
    this.pc = pc;

    pc.onconnectionstatechange = () => {
      const state = pc.connectionState;
      if (state === "failed" || state === "disconnected" || state === "closed") {
        console.warn("WebRTC connection", state);
        this._scheduleReconnect(`webrtc ${state}`);
      }
    };

    pc.oniceconnectionstatechange = () => {
      const state = pc.iceConnectionState;
      if (state === "failed" || state === "disconnected") {
        console.warn("WebRTC ICE", state);
        this._scheduleReconnect(`ice ${state}`);
      }
    };

    pc.addTransceiver("video", { direction: "recvonly" });
    pc.addTransceiver("audio", { direction: "recvonly" });

    pc.ontrack = (ev) => {
      if (ev.streams?.[0]) {
        this.video.srcObject = ev.streams[0];
        this.video.play().catch(() => {});
        this._lastFrameAt = Date.now();
      }
    };

    const offer = await pc.createOffer();
    await pc.setLocalDescription(offer);

    const resp = await fetch(`${base}/api/webrtc?src=${encodeURIComponent(src)}`, {
      method: "POST",
      headers: { "Content-Type": "application/sdp" },
      body: offer.sdp,
      signal: this._abort?.signal,
    });

    if (!resp.ok) {
      throw new Error(`WebRTC HTTP ${resp.status}`);
    }

    const answer = await resp.text();
    await pc.setRemoteDescription({ type: "answer", sdp: answer });
  }

  async _tryIframe(base, src) {
    this._hideWebRTC();

    this._iframe = document.createElement("iframe");
    this._iframe.className = "go2rtc-frame";
    this._iframe.allow = "autoplay; fullscreen";
    this._iframe.src = this._iframeUrl(base, src);
    this.wrap.appendChild(this._iframe);

    await new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error("iframe timeout")), 20000);
      this._iframe.onload = () => {
        clearTimeout(timer);
        resolve();
      };
      this._iframe.onerror = () => {
        clearTimeout(timer);
        reject(new Error("iframe load failed"));
      };
    });
  }

  _iframeUrl(base, src) {
    return `${base}/stream.html?src=${encodeURIComponent(src)}&mode=webrtc,mse&_=${Date.now()}`;
  }

  async _reloadIframe() {
    if (!this._iframe || !this._lastConnect) return false;
    const base = this._lastConnect.go2rtcUrl.replace(/\/$/, "");
    const src = this._lastConnect.streamName;
    this._iframeLoadedAt = Date.now();
    this._iframe.src = this._iframeUrl(base, src);
    await this._sleep(2500);
    return this._probeStream(base, src);
  }

  async _tryMSE(base, src) {
    this._showWebRTC();
    this.video.src = `${base}/api/stream.mp4?src=${encodeURIComponent(src)}&_=${Date.now()}`;
    await this.video.play();
  }

  _showWebRTC() {
    if (this._iframe) {
      this._iframe.remove();
      this._iframe = null;
    }
    this.video.style.display = "";
  }

  _hideWebRTC() {
    if (this.pc) {
      this.pc.close();
      this.pc = null;
    }
    // Don't blank the video — keep last frame visible
    this.video.style.display = "none";
  }

  _waitForFrames(ms) {
    return new Promise((resolve) => {
      const start = performance.now();
      const check = () => {
        if (this.video.readyState >= 2 && this.video.videoWidth > 0) {
          this._lastFrameAt = Date.now();
          resolve(true);
          return;
        }
        if (performance.now() - start >= ms) {
          resolve(false);
          return;
        }
        requestAnimationFrame(check);
      };
      check();
    });
  }

  async _probeStream(base, streamName) {
    try {
      const resp = await fetch(`${base}/api/streams`, { signal: AbortSignal.timeout(3000) });
      if (!resp.ok) return true; // can't verify — assume ok
      const streams = await resp.json();
      const info = streams[streamName];
      if (!info) return false;
      return (info.producers?.length ?? 0) > 0;
    } catch {
      return true; // network blip — don't false-trigger reconnect
    }
  }

  // ---- Frame monitor (liveness) ----

  _startFrameMonitor() {
    this._stopFrameMonitor();
    if (this._iframe || !("requestVideoFrameCallback" in this.video)) return;

    const tick = () => {
      this._lastFrameAt = Date.now();
      this._fpsFrameCount++;
      this._frameCallbackId = this.video.requestVideoFrameCallback(tick);
    };
    this._frameCallbackId = this.video.requestVideoFrameCallback(tick);
  }

  _stopFrameMonitor() {
    if (this._frameCallbackId != null && "cancelVideoFrameCallback" in this.video) {
      this.video.cancelVideoFrameCallback(this._frameCallbackId);
    }
    this._frameCallbackId = null;
  }

  // ---- FPS sampler ----

  _startFpsSampler() {
    this._stopFpsSampler();
    this._fpsFrameCount = 0;
    this._fpsLastSample = performance.now();
    this._fpsLastDecodedFrames = 0;
    this._fpsTimer = setInterval(() => this._sampleFps(), 1000);
    // Kick off initial stats fetch for WebRTC
    this._pollRtcFrames();
  }

  _stopFpsSampler() {
    if (this._fpsTimer) {
      clearInterval(this._fpsTimer);
      this._fpsTimer = null;
    }
    this._currentFps = 0;
    this._updateBadge();
  }

  /** Poll WebRTC stats for framesDecoded (most reliable for RTC) */
  async _pollRtcFrames() {
    if (!this.pc) return 0;
    try {
      const stats = await this.pc.getStats();
      let frames = 0;
      stats.forEach((report) => {
        if (report.type === "inbound-rtp" && report.kind === "video") {
          frames = report.framesDecoded || 0;
        }
      });
      return frames;
    } catch {
      return 0;
    }
  }

  async _sampleFps() {
    const now = performance.now();
    const elapsed = (now - this._fpsLastSample) / 1000;
    if (elapsed <= 0) return;

    let fps = 0;

    // Method 1: WebRTC getStats() — best for RTC transport
    if (this.pc && this._transport === "RTC") {
      const decoded = await this._pollRtcFrames();
      if (this._fpsLastDecodedFrames > 0 && decoded > this._fpsLastDecodedFrames) {
        fps = Math.round((decoded - this._fpsLastDecodedFrames) / elapsed);
      }
      this._fpsLastDecodedFrames = decoded;
    }

    // Method 2: requestVideoFrameCallback count (fallback)
    if (fps === 0 && this._fpsFrameCount > 0) {
      fps = Math.round(this._fpsFrameCount / elapsed);
    }

    // Method 3: getVideoPlaybackQuality (MSE only)
    if (fps === 0 && this._transport === "MSE") {
      try {
        const q = this.video.getVideoPlaybackQuality?.();
        const total = q?.totalVideoFrames ?? 0;
        if (this._fpsLastDecodedFrames > 0 && total > this._fpsLastDecodedFrames) {
          fps = Math.round((total - this._fpsLastDecodedFrames) / elapsed);
        }
        this._fpsLastDecodedFrames = total;
      } catch { /* ignore */ }
    }

    this._currentFps = fps;
    this._fpsFrameCount = 0;
    this._fpsLastSample = now;
    this._updateBadge();
  }

  // ---- Transport label + badge ----

  _setTransport(type) {
    this._transport = type;
    this._updateBadge();
  }

  _updateBadge() {
    if (!this._badgeEl) return;
    const tEl = this._badgeEl.querySelector(".badge-transport");
    const fEl = this._badgeEl.querySelector(".badge-fps");
    if (tEl) tEl.textContent = this._transport || "—";
    if (fEl) fEl.textContent = this._currentFps > 0 ? `${this._currentFps} fps` : "—";
  }

  // ---- Watchdog ----

  _startWatchdog() {
    this._stopWatchdog();
    this._watchdog = setInterval(() => this._checkHealth(), 3000);
  }

  _stopWatchdog() {
    if (this._watchdog) {
      clearInterval(this._watchdog);
      this._watchdog = null;
    }
  }

  async _checkHealth() {
    if (!this._lastConnect || this._reconnecting) return;

    const { go2rtcUrl, streamName } = this._lastConnect;
    const base = go2rtcUrl.replace(/\/$/, "");

    if (this._iframe) {
      const streamOk = await this._probeStream(base, streamName);
      const iframeAge = Date.now() - this._iframeLoadedAt;
      if (!streamOk || iframeAge > 120000) {
        console.warn("Iframe stream unhealthy — reloading");
        this._scheduleReconnect("iframe stale", { soft: true });
      }
      return;
    }

    const pcBad =
      this.pc &&
      ["failed", "disconnected", "closed"].includes(this.pc.connectionState);
    const iceBad =
      this.pc &&
      ["failed", "disconnected"].includes(this.pc.iceConnectionState);

    const hasDimensions = this.video.videoWidth > 0;
    const hasData = this.video.readyState >= 2;

    if (hasDimensions && hasData && !("requestVideoFrameCallback" in this.video)) {
      const t = this.video.currentTime;
      if (t !== this._lastCurrentTime) {
        this._lastCurrentTime = t;
        this._lastFrameAt = Date.now();
      }
    }

    const stalled = this._lastFrameAt > 0 && Date.now() - this._lastFrameAt > 6000;
    const lostFrames = this._lastFrameAt > 0 && (!hasDimensions || !hasData);

    if (pcBad || iceBad || stalled || lostFrames) {
      const reason = pcBad || iceBad ? "connection lost" : stalled ? "frames stalled" : "frames lost";
      console.warn(`Video ${reason} — reconnecting`);
      this._scheduleReconnect(reason);
    }
  }

  _scheduleReconnect(reason, { soft = false } = {}) {
    if (!this._lastConnect || this._reconnecting) return;
    this._reconnecting = true;
    console.warn("Video reconnect:", reason);

    const run = async () => {
      this._show("Reconnecting video…");
      this._notifyState(false);

      if (soft && this._iframe) {
        const ok = await this._reloadIframe();
        if (ok) {
          this._hide();
          this._notifyState(true);
          return;
        }
      }

      const { go2rtcUrl, streamName } = this._lastConnect;
      await this.connect(go2rtcUrl, streamName, { retries: 2, silent: true });
    };

    run()
      .catch(() => {
        if (this._lastConnect) {
          this._show("Video reconnect failed — retrying…");
        }
      })
      .finally(() => {
        this._reconnecting = false;
      });
  }

  _notifyState(playing) {
    this.onStateChange?.(playing);
  }

  _sleep(ms) {
    return new Promise((resolve) => setTimeout(resolve, ms));
  }

  _show(msg) {
    this.overlay.classList.remove("hidden");
    this.status.textContent = msg;
  }

  _hide() {
    this.overlay.classList.add("hidden");
  }
}
