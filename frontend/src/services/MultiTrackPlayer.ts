import type { Input, InputVideoTrack, VideoSample, VideoSampleSink } from "mediabunny";

export interface TransportState {
  playing: boolean;
  time: number;
  duration: number;
}

type SinkFactory = (track: InputVideoTrack) => VideoSampleSink;

export function canUseClientPreview(): boolean {
  return typeof window.VideoDecoder !== "undefined";
}

/** One video track: sequential decode iterator, current frame and canvas. */
class TrackRenderer {
  private readonly ctx: CanvasRenderingContext2D;
  private iterator: AsyncGenerator<VideoSample, void, unknown> | null = null;
  private current: VideoSample | null = null;
  private next: VideoSample | null = null;
  private drawn: VideoSample | null = null;
  private fetching = false;
  private readonly track: InputVideoTrack;
  private readonly canvas: HTMLCanvasElement;

  constructor(track: InputVideoTrack, canvas: HTMLCanvasElement) {
    this.track = track;
    this.canvas = canvas;
    this.ctx = canvas.getContext("2d")!;
  }

  async init(): Promise<void> {
    this.canvas.width = await this.track.getDisplayWidth();
    this.canvas.height = await this.track.getDisplayHeight();
  }

  /** Starts (or restarts after a seek) the sample iterator at `time` seconds. */
  async startAt(time: number, sinkFactory: SinkFactory): Promise<void> {
    await this.stop();
    this.iterator = sinkFactory(this.track).samples(time);
    await this.advance();
  }

  /** Ensures a prefetched upcoming frame exists (safe to call re-entrantly). */
  private async advance(): Promise<void> {
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

  /** Pulls frames up to `clock` seconds and draws the latest one. */
  tick(clock: number): void {
    while (this.next && this.next.timestamp <= clock) {
      this.current?.close();
      this.current = this.next;
      this.next = null;
      void this.advance();
    }
    if (this.current && this.current !== this.drawn) {
      this.current.draw(this.ctx, 0, 0, this.canvas.width, this.canvas.height);
      this.drawn = this.current;
    }
  }

  async stop(): Promise<void> {
    if (this.iterator) {
      await this.iterator.return().catch(() => {});
      this.iterator = null;
    }
    this.current?.close();
    this.next?.close();
    this.current = null;
    this.next = null;
    this.drawn = null;
  }
}

/** Decodes every video track of a recording in the browser, driven by one shared clock. */
export class MultiTrackPlayer {
  private input: Input | null = null;
  private renderers: TrackRenderer[] = [];
  private playing = false;
  private clock = 0;
  private duration = 0;
  private lastTickAt = 0;
  private frameId: number | null = null;
  private disposed = false;
  private readonly container: HTMLElement;
  private readonly labels: string[];
  private readonly cellClasses: { cell: string; label: string };
  private readonly onTransport: (state: TransportState) => void;

  constructor(
    container: HTMLElement,
    labels: string[],
    cellClasses: { cell: string; label: string },
    onTransport: (state: TransportState) => void,
  ) {
    this.container = container;
    this.labels = labels;
    this.cellClasses = cellClasses;
    this.onTransport = onTransport;
  }

  async load(url: string): Promise<void> {
    if (!canUseClientPreview()) throw new Error("WebCodecs not supported in this browser");
    const mediabunny = await import("mediabunny");
    if (this.disposed) return;

    this.input = new mediabunny.Input({
      formats: [mediabunny.MATROSKA],
      source: new mediabunny.UrlSource(url),
    });

    const tracks = await this.input.getVideoTracks();
    if (!tracks.length) throw new Error("Recording has no video tracks");
    for (const track of tracks) {
      if (!(await track.canDecode())) {
        throw new Error(`Track #${track.number} codec not decodable by this browser`);
      }
    }
    this.duration = await this.input.computeDuration();
    if (this.disposed) return;

    this.renderers = tracks.map((track, index) => {
      const canvas = this.addCell(this.labels[index] ?? `Track ${track.number}`);
      return new TrackRenderer(track, canvas);
    });
    for (const renderer of this.renderers) await renderer.init();
    await this.seekTo(0);
  }

  toggle(): void {
    if (this.playing) this.pause();
    else this.play();
  }

  async seekTo(time: number): Promise<void> {
    this.clock = Math.max(0, Math.min(time, this.duration));
    const mediabunny = await import("mediabunny");
    for (const renderer of this.renderers) {
      await renderer.startAt(this.clock, (track) => new mediabunny.VideoSampleSink(track));
      renderer.tick(this.clock);
    }
    this.emit();
  }

  async dispose(): Promise<void> {
    this.disposed = true;
    this.pause();
    await Promise.all(this.renderers.map((renderer) => renderer.stop().catch(() => {})));
    this.renderers = [];
    this.input?.dispose();
    this.input = null;
    this.container.replaceChildren();
  }

  private addCell(label: string): HTMLCanvasElement {
    const cell = document.createElement("div");
    cell.className = this.cellClasses.cell;
    const heading = document.createElement("div");
    heading.className = this.cellClasses.label;
    heading.textContent = label;
    const canvas = document.createElement("canvas");
    cell.append(heading, canvas);
    this.container.appendChild(cell);
    return canvas;
  }

  private play(): void {
    if (this.playing || this.disposed) return;
    this.playing = true;
    this.lastTickAt = performance.now();
    this.frameId = requestAnimationFrame(() => this.tick());
    this.emit();
  }

  private pause(): void {
    this.playing = false;
    if (this.frameId != null) {
      cancelAnimationFrame(this.frameId);
      this.frameId = null;
    }
    this.emit();
  }

  private emit(): void {
    this.onTransport({ playing: this.playing, time: this.clock, duration: this.duration });
  }

  private tick(): void {
    if (!this.playing || this.disposed) return;
    const now = performance.now();
    this.clock = Math.min(this.clock + (now - this.lastTickAt) / 1000, this.duration);
    this.lastTickAt = now;

    for (const renderer of this.renderers) renderer.tick(this.clock);
    this.emit();

    if (this.clock >= this.duration) this.pause();
    else if (this.playing) this.frameId = requestAnimationFrame(() => this.tick());
  }
}
