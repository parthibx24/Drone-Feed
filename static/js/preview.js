/**
 * In-browser multi-track MKV preview via mediabunny (Matroska demux) + WebCodecs.
 * Decodes every video track of a recording and draws each to its own canvas,
 * driven by a shared clock so all cameras stay in sync.
 */

const MEDBUNNY_URL = "https://cdn.jsdelivr.net/npm/mediabunny@1/dist/bundles/mediabunny.min.mjs";

let mediabunnyPromise = null;

function loadMediabunny() {
  if (!mediabunnyPromise) {
    mediabunnyPromise = import(MEDBUNNY_URL).catch((err) => {
      mediabunnyPromise = null;
      throw err;
    });
  }
  return mediabunnyPromise;
}

export function canUseClientPreview() {
  return typeof window.VideoDecoder !== "undefined";
}

/** One video track: sequential decode iterator + current frame + canvas. */
class TrackRenderer {
  constructor(track, canvas, label) {
    this.track = track;
    this.canvas = canvas;
    this.label = label;
    this.ctx = canvas.getContext("2d");
    this.sink = null;
    this.iterator = null;
    this.current = null;
    this.next = null;
    this.drawn = null;
    this.fetching = false;
    this.firstTimestamp = 0;
  }

  async init() {
    const width = await this.track.getDisplayWidth();
    const height = await this.track.getDisplayHeight();
    this.canvas.width = width;
    this.canvas.height = height;
    this.firstTimestamp = await this.track.getFirstTimestamp();
  }

  /** Start (or restart after a seek) the sample iterator at `time` seconds. */
  async startAt(time, sinkFactory) {
    await this.stop();
    this.sink = sinkFactory(this.track);
    this.iterator = this.sink.samples(time)[Symbol.asyncIterator]();
    await this.advance();
  }

  /** Ensure a prefetched upcoming frame exists (re-entrant safe). */
  async advance() {
    if (!this.iterator || this.fetching || this.next) return;
    this.fetching = true;
    try {
      while (this.iterator && !this.next) {
        const { value, done } = await this.iterator.next();
        if (done) return;
        this.next = value;
      }
    } finally {
      this.fetching = false;
    }
  }

  /** Pull frames up to `clock` (seconds) and draw the latest one. */
  tick(clock) {
    // Advance while the upcoming frame's time has been reached
    while (this.next && this.next.timestamp <= clock) {
      if (this.current) this.current.close();
      this.current = this.next;
      this.next = null;
      this.advance(); // async prefetch of the following frame
      if (!this.next) break; // arrives asynchronously; draw what we have
    }
    if (this.current && this.current !== this.drawn) {
      this.current.draw(this.ctx, 0, 0, this.canvas.width, this.canvas.height);
      this.drawn = this.current;
    }
  }

  async stop() {
    if (this.iterator) {
      await this.iterator.return?.().catch(() => {});
      this.iterator = null;
    }
    for (const frame of [this.current, this.next]) {
      if (frame) frame.close();
    }
    this.current = null;
    this.next = null;
    this.drawn = null;
  }
}

export class MultiTrackPlayer {
  /**
   * @param container element to fill with per-track canvas cells
   * @param labels per-track display names (optional)
   * @param onTransport called with { playing, time, duration } to update UI
   */
  constructor(container, labels = [], onTransport = () => {}) {
    this.container = container;
    this.labels = labels;
    this.onTransport = onTransport;
    this.input = null;
    this.renderers = [];
    this.playing = false;
    this.clock = 0;
    this.duration = 0;
    this._lastTickAt = 0;
    this._rafId = null;
    this.disposed = false;
  }

  async load(url) {
    if (!canUseClientPreview()) {
      throw new Error("WebCodecs not supported in this browser");
    }
    const MB = await loadMediabunny();

    this.input = new MB.Input({
      formats: [MB.MATROSKA],
      source: new MB.UrlSource(url),
    });

    const tracks = await this.input.getVideoTracks();
    if (!tracks.length) {
      throw new Error("Recording has no video tracks");
    }
    for (const track of tracks) {
      if (!(await track.canDecode())) {
        throw new Error(`Track #${track.number} codec not decodable by this browser`);
      }
    }

    this.duration = await this.input.computeDuration();

    this.container.innerHTML = "";
    this.renderers = [];
    tracks.forEach((track, i) => {
      const cell = document.createElement("div");
      cell.className = "preview-cell";
      const head = document.createElement("div");
      head.className = "preview-label";
      head.textContent = this.labels[i] || `Track ${track.number}`;
      const canvas = document.createElement("canvas");
      cell.append(head, canvas);
      this.container.appendChild(cell);
      this.renderers.push(new TrackRenderer(track, canvas, head.textContent));
    });

    for (const renderer of this.renderers) {
      await renderer.init();
    }
    await this.seekTo(0);
  }

  play() {
    if (this.playing || this.disposed) return;
    this.playing = true;
    this._lastTickAt = performance.now();
    this._rafId = requestAnimationFrame(() => this._tick());
    this.onTransport(this.state());
  }

  pause() {
    this.playing = false;
    if (this._rafId != null) {
      cancelAnimationFrame(this._rafId);
      this._rafId = null;
    }
    this.onTransport(this.state());
  }

  toggle() {
    if (this.playing) this.pause();
    else this.play();
  }

  state() {
    return { playing: this.playing, time: this.clock, duration: this.duration };
  }

  async seekTo(time) {
    this.clock = Math.max(0, Math.min(time, this.duration));
    const MB = await loadMediabunny();
    for (const renderer of this.renderers) {
      await renderer.startAt(this.clock, (track) => new MB.VideoSampleSink(track));
      renderer.tick(this.clock);
    }
    this.onTransport(this.state());
  }

  _tick() {
    if (!this.playing || this.disposed) return;
    const now = performance.now();
    this.clock += (now - this._lastTickAt) / 1000;
    this._lastTickAt = now;

    if (this.clock >= this.duration) {
      this.clock = this.duration;
    }

    for (const renderer of this.renderers) {
      renderer.tick(this.clock);
    }
    this.onTransport(this.state());

    if (this.clock >= this.duration) {
      this.pause();
    } else if (this.playing) {
      this._rafId = requestAnimationFrame(() => this._tick());
    }
  }

  async dispose() {
    this.disposed = true;
    this.pause();
    for (const renderer of this.renderers) {
      await renderer.stop().catch(() => {});
    }
    this.renderers = [];
    if (this.input) {
      this.input.dispose();
      this.input = null;
    }
    if (this.container) this.container.innerHTML = "";
  }
}
