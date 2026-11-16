/**
 * Push-to-talk: streams the computer's microphone to the drone's speaker
 * through the gateway (GET /api/v1/drones/{id}/speaker/live, WebSocket).
 *
 * Audio leaves as binary frames of PCM, signed 16-bit little-endian, 16 kHz
 * mono, 40 ms each; {"type":"end"} ends it; the server answers one
 * {"type":"result","outcome","reason"}.
 */

export interface TalkResult {
  outcome: "done" | "refused" | "failed";
  reason?: string;
}

interface MicStreamerEvents {
  /** Microphone level, 0..1, about 25 times a second while talking. */
  onLevel?: (level: number) => void;
  /** The server's answer for the session (once). */
  onResult?: (result: TalkResult) => void;
}

const TARGET_RATE = 16000;
const CHUNK_SAMPLES = 640; // 40 ms at 16 kHz
const MAX_BUFFERED = 64 * 1024; // drop audio rather than fall seconds behind

// Runs on the audio thread: low-pass, resample to 16 kHz (linear), convert
// to Int16 and post 40 ms chunks with their level.
const WORKLET = `
class Pcm16k extends AudioWorkletProcessor {
  constructor() {
    super();
    this.ratio = sampleRate / ${TARGET_RATE};
    this.alpha = 1 - Math.exp(-2 * Math.PI * 7000 / sampleRate);
    this.lp = 0; this.last = 0; this.pos = 0;
    this.out = new Int16Array(${CHUNK_SAMPLES}); this.n = 0; this.sq = 0;
  }
  process(inputs) {
    const input = inputs[0] && inputs[0][0];
    if (!input) return true;
    const x = new Float32Array(input.length);
    for (let k = 0; k < input.length; k++) { this.lp += this.alpha * (input[k] - this.lp); x[k] = this.lp; }
    const n = x.length;
    let pos = this.pos;
    while (pos <= n - 1) {
      const i = Math.floor(pos), f = pos - i;
      const a = i < 0 ? this.last : x[i];
      const b = i + 1 <= n - 1 ? x[i + 1] : a;
      let s = a + (b - a) * f;
      s = s > 1 ? 1 : s < -1 ? -1 : s;
      this.out[this.n++] = s < 0 ? s * 0x8000 : s * 0x7fff;
      this.sq += s * s;
      if (this.n === ${CHUNK_SAMPLES}) {
        const level = Math.min(1, Math.sqrt(this.sq / ${CHUNK_SAMPLES}) * 4);
        this.port.postMessage({ pcm: this.out.buffer, level }, [this.out.buffer]);
        this.out = new Int16Array(${CHUNK_SAMPLES}); this.n = 0; this.sq = 0;
      }
      pos += this.ratio;
    }
    this.pos = pos - n;
    this.last = x[n - 1];
    return true;
  }
}
registerProcessor("pcm16k", Pcm16k);
`;

/** Why the microphone can't be used here, or null if it can. */
export function micUnavailableReason(): string | null {
  if (!window.isSecureContext || !navigator.mediaDevices?.getUserMedia) {
    return "The microphone needs the dashboard over HTTPS";
  }
  if (typeof AudioWorkletNode === "undefined") return "This browser can't stream the microphone";
  return null;
}

export class MicStreamer {
  private ws: WebSocket | null = null;
  private stream: MediaStream | null = null;
  private ctx: AudioContext | null = null;
  private stopped = false;
  private resulted = false;
  private opened = false;

  constructor(
    private readonly droneId: string,
    private readonly events: MicStreamerEvents = {},
  ) {}

  /** Opens the session and starts the microphone. Rejects with a readable message. */
  async start(): Promise<void> {
    const unavailable = micUnavailableReason();
    if (unavailable) throw new Error(unavailable);

    const protocol = location.protocol === "https:" ? "wss:" : "ws:";
    const ws = new WebSocket(`${protocol}//${location.host}/api/v1/drones/${encodeURIComponent(this.droneId)}/speaker/live`);
    ws.binaryType = "arraybuffer";
    this.ws = ws;
    ws.onmessage = (event) => {
      if (typeof event.data !== "string") return;
      try {
        const msg = JSON.parse(event.data) as { type?: string } & TalkResult;
        if (msg.type === "result") this.result({ outcome: msg.outcome, reason: msg.reason });
      } catch {
        /* ignore anything else */
      }
    };
    ws.onclose = () => {
      if (this.opened) {
        this.result({ outcome: "failed", reason: this.stopped ? "No answer from the speaker" : "Connection to the speaker closed" });
      }
      this.release();
    };
    await new Promise<void>((resolve, reject) => {
      ws.onopen = () => {
        this.opened = true;
        resolve();
      };
      ws.onerror = () => reject(new Error("Could not reach the speaker"));
    });
    if (this.stopped) return;

    try {
      this.stream = await navigator.mediaDevices.getUserMedia({
        audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true, channelCount: 1 },
      });
    } catch (error) {
      this.close();
      const name = error instanceof DOMException ? error.name : "";
      throw new Error(name === "NotAllowedError" ? "Microphone permission denied" : "No microphone available");
    }
    if (this.stopped) {
      this.release();
      return;
    }

    const ctx = new AudioContext();
    this.ctx = ctx;
    const url = URL.createObjectURL(new Blob([WORKLET], { type: "application/javascript" }));
    try {
      await ctx.audioWorklet.addModule(url);
    } finally {
      URL.revokeObjectURL(url);
    }
    if (this.stopped) {
      this.release();
      return;
    }
    const node = new AudioWorkletNode(ctx, "pcm16k", { numberOfOutputs: 1 });
    node.port.onmessage = (event: MessageEvent<{ pcm: ArrayBuffer; level: number }>) => {
      this.events.onLevel?.(event.data.level);
      if (ws.readyState === WebSocket.OPEN && ws.bufferedAmount < MAX_BUFFERED) ws.send(event.data.pcm);
    };
    // A silent path to the output keeps the graph (and the worklet) running.
    const mute = ctx.createGain();
    mute.gain.value = 0;
    ctx.createMediaStreamSource(this.stream).connect(node);
    node.connect(mute).connect(ctx.destination);
    if (ctx.state === "suspended") await ctx.resume();
  }

  /** Stops the microphone and ends the session; the result still arrives. */
  stop(): void {
    if (this.stopped) return;
    this.stopped = true;
    this.events.onLevel?.(0);
    this.release();
    const ws = this.ws;
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ type: "end" }));
      window.setTimeout(() => this.close(), 10000); // give up on the answer after 10 s
    } else {
      this.close();
    }
  }

  private result(r: TalkResult): void {
    if (this.resulted) return;
    this.resulted = true;
    this.events.onResult?.(r);
    this.close();
  }

  private release(): void {
    this.stream?.getTracks().forEach((t) => t.stop());
    this.stream = null;
    void this.ctx?.close().catch(() => undefined);
    this.ctx = null;
  }

  private close(): void {
    const ws = this.ws;
    if (ws && ws.readyState <= WebSocket.OPEN) ws.close();
  }
}
