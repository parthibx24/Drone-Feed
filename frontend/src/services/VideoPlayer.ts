import { buildVideoFrameHtml } from "./videoFrameTemplate";

const FRAME_LOAD_TIMEOUT_MS = 3000;
const WATCHDOG_INTERVAL_MS = 4000;
const STALL_THRESHOLD_MS = 20000;
const CONNECT_RETRIES = 2;
const RETRY_BACKOFF_MS = 1500;

export interface VideoPlayerCallbacks {
  /** A message to show over the feed, or null when it is playing. */
  onStatus: (message: string | null) => void;
  onPlayingChange: (playing: boolean) => void;
}

interface Connection {
  go2rtcUrl: string;
  streamName: string;
}

function sanitizeUrl(url: string): string {
  return url.trim().replace(/^["']|["']$/g, "").replace(/\/+$/, "");
}

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

/** Plays one go2rtc stream inside an iframe and reconnects when frames stop arriving. */
export class VideoPlayer {
  private iframe: HTMLIFrameElement | null = null;
  private watchdog: ReturnType<typeof setInterval> | null = null;
  private connection: Connection | null = null;
  private reconnecting = false;
  private lastFrameAt = 0;
  private messageListener: ((event: MessageEvent) => void) | null = null;
  private readonly container: HTMLElement;
  private readonly callbacks: VideoPlayerCallbacks;

  private readonly onVisibilityChange = () => {
    if (document.visibilityState === "visible" && this.connection) {
      setTimeout(() => this.checkHealth(), 500);
    }
  };

  constructor(container: HTMLElement, callbacks: VideoPlayerCallbacks) {
    this.container = container;
    this.callbacks = callbacks;
    document.addEventListener("visibilitychange", this.onVisibilityChange);
  }

  async connect(go2rtcUrl: string, stream: string, silent = false): Promise<boolean> {
    const base = sanitizeUrl(go2rtcUrl);
    const streamName = stream.trim().replace(/^["']|["']$/g, "");
    this.connection = { go2rtcUrl: base, streamName };
    this.stopWatchdog();

    for (let attempt = 0; attempt <= CONNECT_RETRIES; attempt++) {
      if (attempt > 0) {
        if (!silent) this.callbacks.onStatus(`Retrying ${streamName} (${attempt}/${CONNECT_RETRIES})…`);
        await sleep(RETRY_BACKOFF_MS * attempt);
      }
      if (!this.connection) return false;

      try {
        await this.loadFrame(base, streamName, silent);
        if (!this.connection) return false;
        this.lastFrameAt = Date.now();
        this.startWatchdog();
        return true;
      } catch (error) {
        console.warn(`Video connect attempt ${attempt + 1} failed:`, error);
        if (attempt === CONNECT_RETRIES) {
          if (!silent) this.callbacks.onStatus(`Video error: ${(error as Error).message}`);
          this.callbacks.onPlayingChange(false);
          throw error;
        }
      }
    }
    return false;
  }

  dispose(): void {
    document.removeEventListener("visibilitychange", this.onVisibilityChange);
    this.stopWatchdog();
    this.removeFrame();
    this.connection = null;
  }

  private removeFrame(): void {
    if (this.messageListener) {
      window.removeEventListener("message", this.messageListener);
      this.messageListener = null;
    }
    this.iframe?.remove();
    this.iframe = null;
  }

  private loadFrame(go2rtcUrl: string, streamName: string, silent: boolean): Promise<void> {
    this.removeFrame();
    if (!silent) this.callbacks.onStatus(`Connecting to ${streamName}…`);

    this.messageListener = (event: MessageEvent) => {
      const data = event.data as { type?: string; stream?: string } | null;
      if (!data || typeof data !== "object" || data.stream !== streamName) return;
      if (data.type === "video-playing") {
        this.callbacks.onStatus(null);
        this.lastFrameAt = Date.now();
        this.callbacks.onPlayingChange(true);
      } else if (data.type === "video-stalled" || data.type === "video-error") {
        this.callbacks.onPlayingChange(false);
      }
    };
    window.addEventListener("message", this.messageListener);

    const iframe = document.createElement("iframe");
    iframe.allow = "autoplay; fullscreen";
    iframe.srcdoc = buildVideoFrameHtml(go2rtcUrl, streamName);
    this.iframe = iframe;
    this.container.appendChild(iframe);

    // A slow frame is not a failure: the player inside the iframe handles its own recovery.
    return new Promise((resolve) => {
      const timeout = setTimeout(resolve, FRAME_LOAD_TIMEOUT_MS);
      iframe.onload = () => {
        clearTimeout(timeout);
        resolve();
      };
    });
  }

  private startWatchdog(): void {
    this.stopWatchdog();
    this.watchdog = setInterval(() => this.checkHealth(), WATCHDOG_INTERVAL_MS);
  }

  private stopWatchdog(): void {
    if (this.watchdog) {
      clearInterval(this.watchdog);
      this.watchdog = null;
    }
  }

  private checkHealth(): void {
    if (!this.connection || this.reconnecting) return;
    if (this.lastFrameAt > 0 && Date.now() - this.lastFrameAt > STALL_THRESHOLD_MS) {
      console.warn("Video frames stalled for >20s — reconnecting iframe");
      this.reconnect();
    }
  }

  private reconnect(): void {
    if (!this.connection) return;
    this.reconnecting = true;
    this.callbacks.onStatus("Reconnecting video…");
    this.callbacks.onPlayingChange(false);

    const { go2rtcUrl, streamName } = this.connection;
    this.connect(go2rtcUrl, streamName, true)
      .catch(() => {
        if (this.connection) this.callbacks.onStatus("Video reconnect failed — retrying…");
      })
      .finally(() => {
        this.reconnecting = false;
      });
  }
}
