import { useCallback, useEffect, useRef, useState } from "react";
import type { SpeakerState } from "../../hooks/useSpeaker";
import { api } from "../../services/api";
import { ApiError, errorText } from "../../services/http";
import { MicStreamer, micUnavailableReason, type TalkResult } from "../../services/MicStreamer";
import type { SpeakerLang } from "../../types/gateway";
import { Button } from "../ui/Button";
import { SegmentedControl } from "../ui/SegmentedControl";
import styles from "./SpeakerPanel.module.css";

interface SpeakerPanelProps {
  droneId: string;
  speaker: SpeakerState;
  canOperate: boolean;
  notify: (message: string) => void;
}

const LANGS: { value: SpeakerLang; label: string }[] = [
  { value: "en", label: "English" },
  { value: "bn", label: "বাংলা" },
];

const STATUS_TEXT: Record<string, string> = {
  idle: "Speaker ready",
  playing: "Speaker playing",
  transmitting: "Sending audio to the speaker",
  error: "Speaker reports an error",
  unknown: "Speaker status unknown",
};

const MAX_TEXT = 500;

/** The server's own words for a refusal ("The pilot hasn't allowed StarWatch control"). */
function reasonOf(error: unknown): string {
  if (error instanceof ApiError && error.status !== 401 && error.status !== 403) return error.message;
  return errorText(error);
}

/**
 * The drone's loudspeaker: say a typed message (English or Bangla) once or
 * on loop, stop it, set the volume, and talk live (hold the button, or T).
 */
export function SpeakerPanel({ droneId, speaker, canOperate, notify }: SpeakerPanelProps) {
  const { info, problem, refresh } = speaker;
  const [text, setText] = useState("");
  const [lang, setLang] = useState<SpeakerLang>("en");
  const [busy, setBusy] = useState<"say" | "loop" | "stop" | null>(null);
  const [volume, setVolume] = useState<number | null>(null);
  const [lastProblem, setLastProblem] = useState<string | null>(null);
  const [talking, setTalking] = useState(false);
  const [level, setLevel] = useState(0);
  const mic = useRef<MicStreamer | null>(null);

  // Follow the drone's volume unless the operator is moving the slider.
  const dragging = useRef(false);
  useEffect(() => {
    if (!dragging.current && info?.volume != null) setVolume(info.volume);
  }, [info?.volume]);

  const ready = canOperate && Boolean(info?.present && info.can_play);
  const notReady = !canOperate
    ? "Operator role required"
    : !info
      ? (problem ?? "Speaker status unknown")
      : !info.present
        ? "No speaker fitted on the drone"
        : !info.can_play
          ? "Not ready: the pilot app is offline, or the pilot hasn't granted StarWatch control"
          : null;

  const run = async (kind: "say" | "loop" | "stop", call: () => Promise<unknown>, done: string) => {
    setBusy(kind);
    try {
      await call();
      setLastProblem(null);
      notify(done);
    } catch (error) {
      const reason = reasonOf(error);
      setLastProblem(reason);
      notify(reason);
    } finally {
      setBusy(null);
      refresh();
    }
  };

  const say = (loop: boolean) => {
    const message = text.trim();
    if (!message) return notify("Type a message first");
    void run(loop ? "loop" : "say", () => api.speakerSay(droneId, message, lang, loop), loop ? "Playing on loop" : "Speaking");
  };

  const sendVolume = () => {
    dragging.current = false;
    if (volume == null || volume === info?.volume) return;
    api.speakerVolume(droneId, volume).then(refresh, (error) => notify(reasonOf(error)));
  };

  // --- push-to-talk
  const startTalk = useCallback(async () => {
    if (mic.current || !ready) return;
    const streamer = new MicStreamer(droneId, {
      onLevel: setLevel,
      onResult: (r: TalkResult) => {
        if (r.outcome !== "done") {
          const reason = r.reason || "The speaker didn't take the audio";
          setLastProblem(reason);
          notify(reason);
        }
        refresh();
      },
    });
    mic.current = streamer;
    setTalking(true);
    try {
      await streamer.start();
    } catch (error) {
      mic.current = null;
      setTalking(false);
      setLevel(0);
      const reason = error instanceof Error ? error.message : String(error);
      setLastProblem(reason);
      notify(reason);
    }
  }, [droneId, notify, ready, refresh]);

  const stopTalk = useCallback(() => {
    mic.current?.stop();
    mic.current = null;
    setTalking(false);
    setLevel(0);
  }, []);

  // Hold T to talk (not while typing; T is free among the dashboard's shortcuts).
  useEffect(() => {
    const typing = () => document.activeElement?.matches("input, textarea, select") ?? false;
    const down = (event: KeyboardEvent) => {
      if (event.repeat || event.key.toLowerCase() !== "t" || typing()) return;
      event.preventDefault();
      void startTalk();
    };
    const up = (event: KeyboardEvent) => {
      if (event.key.toLowerCase() === "t") stopTalk();
    };
    window.addEventListener("keydown", down);
    window.addEventListener("keyup", up);
    window.addEventListener("blur", stopTalk);
    return () => {
      window.removeEventListener("keydown", down);
      window.removeEventListener("keyup", up);
      window.removeEventListener("blur", stopTalk);
    };
  }, [startTalk, stopTalk]);

  // Leaving the tab or the page ends a live session.
  useEffect(() => () => mic.current?.stop(), []);

  const micProblem = micUnavailableReason();
  const status = notReady ?? STATUS_TEXT[info?.status ?? "unknown"] ?? "Speaker ready";

  return (
    <div className={styles.panel}>
      <div className={`${styles.status} ${notReady ? styles.warn : ""}`} role="status">
        <span className={`${styles.dot} ${info?.status === "playing" || talking ? styles.live : ""}`} aria-hidden />
        {talking ? "Talking live…" : status}
      </div>

      <div className={styles.row}>
        <SegmentedControl ariaLabel="Language" options={LANGS} value={lang} onChange={(v) => setLang(v as SpeakerLang)} disabled={!ready} />
      </div>
      <textarea
        className={styles.text}
        lang={lang}
        rows={4}
        maxLength={MAX_TEXT}
        placeholder={lang === "bn" ? "স্পিকারে যা বলবে লিখুন" : "Type what the speaker should say"}
        value={text}
        onChange={(e) => setText(e.target.value)}
        disabled={!ready}
      />
      <div className={styles.row}>
        <Button variant="primary" className={styles.grow} onClick={() => say(false)} busy={busy === "say"} disabled={!ready || !text.trim()}>
          Say
        </Button>
        <Button className={styles.grow} onClick={() => say(true)} busy={busy === "loop"} disabled={!ready || !text.trim()}>
          Loop
        </Button>
        <Button
          variant="danger"
          className={styles.grow}
          onClick={() => void run("stop", () => api.speakerStop(droneId), "Speaker stopped")}
          busy={busy === "stop"}
          disabled={!canOperate || !info?.present}
        >
          Stop
        </Button>
      </div>

      <label className={styles.volume}>
        <span>Volume</span>
        <input
          type="range"
          min={0}
          max={100}
          step={5}
          value={volume ?? 0}
          disabled={!canOperate || !info?.present || volume == null}
          onPointerDown={() => (dragging.current = true)}
          onChange={(e) => setVolume(Number(e.target.value))}
          onPointerUp={sendVolume}
          onKeyUp={sendVolume}
        />
        <span className={styles.value}>{volume == null ? "–" : `${volume}%`}</span>
      </label>

      <button
        type="button"
        className={`${styles.talk} ${talking ? styles.talking : ""}`}
        disabled={!ready || Boolean(micProblem)}
        onPointerDown={(e) => {
          e.currentTarget.setPointerCapture(e.pointerId);
          void startTalk();
        }}
        onPointerUp={stopTalk}
        onPointerCancel={stopTalk}
        onContextMenu={(e) => e.preventDefault()}
        aria-pressed={talking}
      >
        <span className={styles.talkLabel}>{talking ? "Release to stop" : "Hold to talk"}</span>
        <span className={styles.talkHint}>{micProblem ?? "or hold T"}</span>
        <span className={styles.meter} aria-hidden>
          <span style={{ width: `${Math.round(level * 100)}%` }} />
        </span>
      </button>

      {lastProblem && <div className={styles.problem}>{lastProblem}</div>}
    </div>
  );
}
