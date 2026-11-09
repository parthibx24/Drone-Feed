/** Self-contained go2rtc player document, loaded into each feed iframe via srcdoc. */
export function buildVideoFrameHtml(go2rtcUrl: string, streamName: string): string {
  return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <link rel="apple-touch-icon" href="${go2rtcUrl}/icons/apple-touch-icon-180x180.png" sizes="180x180">
  <link rel="icon" href="${go2rtcUrl}/icons/favicon.ico">
  <title>${streamName} - go2rtc</title>
  <style>
    body {
      background: black;
      margin: 0;
      padding: 0;
      display: flex;
      font-family: Arial, Helvetica, sans-serif;
      height: 100%;
      width: 100%;
      overflow: hidden;
    }
    html, body {
      height: 100%;
      width: 100%;
    }
    .flex {
      flex-wrap: wrap;
      align-content: flex-start;
      align-items: flex-start;
    }
    video-stream {
      position: relative;
      width: 100%;
      height: 100%;
      display: flex;
      flex: 1 0 100%;
    }
    video-stream video {
      display: block;
      width: 100%;
      height: 100%;
      object-fit: contain;
    }
    .info {
      position: absolute;
      top: 0;
      left: 0;
      right: 0;
      padding: 12px;
      color: white;
      display: flex;
      justify-content: space-between;
      align-items: center;
      pointer-events: none;
      font-family: Arial, Helvetica, sans-serif;
      font-size: 13px;
      text-shadow: 0 1px 3px rgba(0, 0, 0, 0.9);
      z-index: 10;
    }
    .info .status {
      flex: 1;
      min-width: 0;
      word-break: break-word;
      font-size: 12px;
      opacity: 0.9;
    }
    /* Not shown, but the FPS element must exist: the playing/stalled reports depend on it. */
    .info .fps-metric,
    .info .mode {
      display: none;
    }
  </style>
</head>
<body>
  <script type="module" src="${go2rtcUrl}/video-stream.js"></script>
  <script type="module">
    const go2rtcUrl = ${JSON.stringify(go2rtcUrl)};
    const streamName = ${JSON.stringify(streamName)};
    const wsUrl = new URL("api/ws?src=" + encodeURIComponent(streamName), go2rtcUrl + "/").href;
    const streamsApiUrl = new URL("api/streams?src=" + encodeURIComponent(streamName), go2rtcUrl + "/").href;

    const videoStream = document.createElement("video-stream");
    videoStream.background = true;
    videoStream.mode = "webrtc,mse";
    videoStream.style.flex = "1 0 100%";
    videoStream.src = wsUrl;
    document.body.appendChild(videoStream);
    document.title = streamName + " - go2rtc";

    // ---- Helper to extract stream source bytes from go2rtc streams API ----
    function extractSourceBytes(data, name) {
      if (!data) return null;
      const stream = (data && data[name]) ? data[name] : data;
      if (!stream || typeof stream !== "object") return null;

      let total = 0;
      let hasValue = false;

      if (Array.isArray(stream.producers) && stream.producers.length > 0) {
        for (const p of stream.producers) {
          if (typeof p.bytes_recv === "number") { total += p.bytes_recv; hasValue = true; }
          else if (typeof p.recv === "number") { total += p.recv; hasValue = true; }
          else if (typeof p.bytes_sent === "number") { total += p.bytes_sent; hasValue = true; }
          else if (typeof p.send === "number") { total += p.send; hasValue = true; }
          else if (typeof p.bytes === "number") { total += p.bytes; hasValue = true; }

          if (Array.isArray(p.receivers)) {
            for (const r of p.receivers) {
              if (typeof r.bytes === "number") { total += r.bytes; hasValue = true; }
            }
          }
          if (Array.isArray(p.senders)) {
            for (const s of p.senders) {
              if (typeof s.bytes === "number") { total += s.bytes; hasValue = true; }
            }
          }
        }
      }

      if (!hasValue && Array.isArray(stream.consumers)) {
        for (const c of stream.consumers) {
          if (typeof c.bytes_sent === "number") { total += c.bytes_sent; hasValue = true; }
          else if (typeof c.send === "number") { total += c.send; hasValue = true; }
          else if (typeof c.bytes === "number") { total += c.bytes; hasValue = true; }
          if (Array.isArray(c.senders)) {
            for (const s of c.senders) {
              if (typeof s.bytes === "number") { total += s.bytes; hasValue = true; }
            }
          }
        }
      }

      if (!hasValue) {
        if (typeof stream.bytes === "number") return stream.bytes;
        if (typeof stream.bytes_recv === "number") return stream.bytes_recv;
        if (typeof stream.bytes_sent === "number") return stream.bytes_sent;
        return null;
      }
      return total;
    }

    async function fetchStreamBytes() {
      const endpoints = [
        "/api/go2rtc/streams?src=" + encodeURIComponent(streamName),
        streamsApiUrl,
      ];
      for (const endpoint of endpoints) {
        try {
          const res = await fetch(endpoint, { cache: "no-store" });
          if (res.ok) {
            const json = await res.json();
            return extractSourceBytes(json, streamName);
          }
        } catch (err) {
          // try fallback
        }
      }
      return null;
    }

    // ---- 0 FPS Dual Probe (probe 1, wait 1s, probe 2) ----
    let isProbingZeroFps = false;
    async function executeZeroFpsProbe() {
      if (isProbingZeroFps) return;
      isProbingZeroFps = true;
      try {
        console.log("[go2rtc probe] 0 FPS detected for " + streamName + ". Probe 1 starting…");
        const probe1Bytes = await fetchStreamBytes();
        console.log("[go2rtc probe] Probe 1 (" + streamName + "):", probe1Bytes, "bytes");

        // 1-second delay between consecutive calls
        await new Promise((resolve) => setTimeout(resolve, 1000));

        console.log("[go2rtc probe] Probe 2 starting after 1s delay for " + streamName + "…");
        const probe2Bytes = await fetchStreamBytes();
        console.log("[go2rtc probe] Probe 2 (" + streamName + "):", probe2Bytes, "bytes");

        const delta = (probe1Bytes !== null && probe2Bytes !== null) ? (probe2Bytes - probe1Bytes) : null;
        console.log("[go2rtc probe] 0 FPS summary (" + streamName + "): Probe1=" + probe1Bytes + " B, Probe2=" + probe2Bytes + " B, Delta=" + delta + " B");

        window.parent?.postMessage({
          type: "go2rtc-probe-result",
          stream: streamName,
          probe1: probe1Bytes,
          probe2: probe2Bytes,
          delta: delta,
        }, "*");
      } catch (e) {
        console.warn("[go2rtc probe] Probe execution error:", e);
      } finally {
        isProbingZeroFps = false;
      }
    }

    // ---- Setup VRFC FPS Observer & Info Metric Element ----
    let targetVideo = null;
    let infoEl = null;
    let fpsEl = null;
    let frameCount = 0;
    let lastSampleTime = performance.now();
    let hasProbedZeroFps = false;
    let vrfcActive = false;

    function ensureFpsMetricElement() {
      if (!infoEl) {
        infoEl = videoStream.querySelector(".info");
      }
      if (infoEl && !fpsEl) {
        fpsEl = infoEl.querySelector(".fps-metric");
        if (!fpsEl) {
          fpsEl = document.createElement("div");
          fpsEl.className = "fps-metric zero";
          fpsEl.textContent = "0 fps";
          const modeEl = infoEl.querySelector(".mode");
          if (modeEl) {
            infoEl.insertBefore(fpsEl, modeEl);
          } else {
            infoEl.appendChild(fpsEl);
          }
        }
      }
    }

    function onVideoFrame() {
      frameCount++;
      if (targetVideo && "requestVideoFrameCallback" in targetVideo) {
        targetVideo.requestVideoFrameCallback(onVideoFrame);
      }
    }

    function bindVideoVrfc(video) {
      if (!video || targetVideo === video) return;
      targetVideo = video;
      frameCount = 0;
      lastSampleTime = performance.now();

      if ("requestVideoFrameCallback" in video) {
        vrfcActive = true;
        video.requestVideoFrameCallback(onVideoFrame);
      } else {
        console.warn("[VRFC] requestVideoFrameCallback not supported on this video element");
      }
    }

    function checkDom() {
      ensureFpsMetricElement();
      const video = videoStream.querySelector("video");
      if (video) video.controls = false;
      if (video && video !== targetVideo) {
        bindVideoVrfc(video);
      }
    }

    // Mutation observer to detect when video-stream creates video and info
    const domObserver = new MutationObserver(() => {
      checkDom();
    });
    domObserver.observe(videoStream, { childList: true, subtree: true });
    checkDom();

    // ---- 1-second FPS sample timer ----
    setInterval(() => {
      checkDom();
      const now = performance.now();
      const elapsed = (now - lastSampleTime) / 1000;
      if (elapsed <= 0) return;

      const fps = Math.round(frameCount / elapsed);
      frameCount = 0;
      lastSampleTime = now;

      if (fpsEl) {
        if (fps > 0) {
          fpsEl.textContent = fps + " fps";
          fpsEl.classList.remove("zero");
          hasProbedZeroFps = false; // Reset 0fps probe flag when stream is active
          window.parent?.postMessage({ type: "video-playing", stream: streamName, fps: fps }, "*");
        } else {
          fpsEl.textContent = "0 fps";
          fpsEl.classList.add("zero");
          // Trigger dual probe at first point of 0fps
          if (!hasProbedZeroFps) {
            hasProbedZeroFps = true;
            executeZeroFpsProbe();
          }
          window.parent?.postMessage({ type: "video-stalled", stream: streamName, fps: 0 }, "*");
        }
      }
    }, 1000);
  </script>
</body>
</html>`;
}
