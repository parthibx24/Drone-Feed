import { useState } from "react";
import { useDrone } from "../../context/DroneContext";
import type { AIStatusState } from "../../hooks/useAIStatus";
import type { Notify } from "../../hooks/useToast";
import { api } from "../../services/api";
import { errorText } from "../../services/http";
import type { AICounts, AIEvent, AnalyzeResponse } from "../../types/ai";
import {
  countGroups,
  DETECTIONS_FRESH_MS,
  formatDistance,
  formatEventTime,
  labelName,
  percent,
  streamLabel,
} from "../../utils/ai";
import { Button } from "../ui/Button";
import styles from "./AIPanel.module.css";

const VISIBLE_EVENTS = 20;

interface AIPanelProps {
  ai: AIStatusState;
  events: AIEvent[];
  /** Camera currently shown large; "Analyze frame" uses it. */
  mainStream: string | undefined;
  canOperate: boolean;
  notify: Notify;
  onOpenEvent: (event: AIEvent) => void;
}

/** Sidebar tab: detector state, live counts, follow, frame analysis and recent events. */
export function AIPanel({ ai, events, mainStream, canOperate, notify, onOpenEvent }: AIPanelProps) {
  const { id, detections, follow, setFollow } = useDrone();
  const { status, error, refresh } = ai;
  const [toggling, setToggling] = useState(false);
  const [question, setQuestion] = useState("");
  const [analyzing, setAnalyzing] = useState(false);
  const [answer, setAnswer] = useState<AnalyzeResponse | null>(null);
  const [stopping, setStopping] = useState(false);

  if (!status) {
    return (
      <div className={styles.panel}>
        <p className={styles.notice}>{error ?? "Loading AI status…"}</p>
      </div>
    );
  }

  const toggle = async () => {
    setToggling(true);
    try {
      await api.aiSetEnabled(id, !status.enabled);
      notify(status.enabled ? "AI detection turned off" : "AI detection turned on");
      refresh();
    } catch (err) {
      notify(errorText(err));
    } finally {
      setToggling(false);
    }
  };

  const analyze = async () => {
    setAnalyzing(true);
    try {
      setAnswer(await api.aiAnalyze(id, mainStream, question.trim()));
    } catch (err) {
      notify(errorText(err));
    } finally {
      setAnalyzing(false);
    }
  };

  const stopFollow = async () => {
    setStopping(true);
    try {
      await api.aiStopFollow(id);
      setFollow({ ...follow, active: false, reason: "stopped" });
    } catch (err) {
      notify(errorText(err));
    } finally {
      setStopping(false);
    }
  };

  const now = Date.now();
  const streamNames = [...new Set([...Object.keys(status.streams), ...Object.keys(detections)])].sort(
    (a, b) => Number(streamLabel(a) === "Thermal") - Number(streamLabel(b) === "Thermal"),
  );
  const state = !status.configured
    ? { text: "Not configured", tone: styles.off }
    : !status.enabled
      ? { text: "Off", tone: styles.off }
      : !status.detector_online
        ? { text: "Detector offline", tone: styles.bad }
        : status.active
          ? { text: "Detecting", tone: styles.ok }
          : { text: "Idle", tone: styles.idle };

  return (
    <div className={styles.panel}>
      <section className={styles.section} aria-label="Detection">
        <div className={styles.headerRow}>
          <h3 className={styles.heading}>Detection</h3>
          <span className={`${styles.pill} ${state.tone}`}>{state.text}</span>
          {canOperate && status.configured && (
            <button
              type="button"
              role="switch"
              aria-checked={status.enabled}
              aria-label="AI detection for this drone"
              className={`${styles.switch} ${status.enabled ? styles.switchOn : ""}`}
              disabled={toggling}
              onClick={() => void toggle()}
            >
              <span className={styles.knob} />
            </button>
          )}
        </div>
        {status.enabled && status.detector_online && !status.active && (
          <p className={styles.hint}>Runs while the drone is armed or someone has this dashboard open.</p>
        )}
        {!status.configured && <p className={styles.hint}>Add an <code>ai</code> block to gateway.yaml to enable it.</p>}

        {streamNames.length > 0 && (
          <div className={styles.streams}>
            {streamNames.map((name) => {
              const info = status.streams[name];
              const live = detections[name];
              const fresh = live && now - live.receivedAt < DETECTIONS_FRESH_MS;
              const counts: AICounts = fresh ? countGroups(live.frame.detections) : (info?.counts ?? { person: 0, vehicle: 0, other: 0 });
              const inferMs = fresh ? live.frame.infer_ms : info?.infer_ms;
              return (
                <div key={name} className={styles.streamCard}>
                  <div className={styles.streamName}>{streamLabel(name)}</div>
                  <div className={styles.counts}>
                    <Count label="People" value={counts.person} tone={styles.person} />
                    <Count label="Vehicles" value={counts.vehicle} tone={styles.vehicle} />
                    {counts.other > 0 && <Count label="Other" value={counts.other} tone={styles.other} />}
                  </div>
                  <div className={styles.rates}>
                    {info ? `${info.fps.toFixed(1)} fps` : "—"}
                    {inferMs != null && ` · ${Math.round(inferMs)} ms`}
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </section>

      {status.features.follow && (
        <section className={styles.section} aria-label="Follow">
          <h3 className={styles.heading}>Follow</h3>
          {follow.active ? (
            <div className={styles.followRow}>
              <span>
                Following #{follow.track_id} on the {streamLabel(follow.stream).toLowerCase()} camera
              </span>
              {canOperate && (
                <Button size="sm" variant="danger" busy={stopping} onClick={() => void stopFollow()}>
                  Stop
                </Button>
              )}
            </div>
          ) : (
            <p className={styles.hint}>
              {canOperate
                ? "Click a box on the video to keep that object centred with the gimbal."
                : "Operators can click a box on the video to follow it with the gimbal."}
              {follow.reason && ` Last follow ended: ${follow.reason}.`}
            </p>
          )}
        </section>
      )}

      <section className={styles.section} aria-label="Analyze frame">
        <h3 className={styles.heading}>Analyze frame</h3>
        {status.features.analyze ? (
          <>
            <textarea
              className={styles.question}
              rows={2}
              placeholder="Optional question, e.g. How many vehicles are on the road?"
              value={question}
              onChange={(e) => setQuestion(e.target.value)}
              disabled={!canOperate || analyzing}
              aria-label="Question about the current frame"
            />
            <div className={styles.analyzeRow}>
              <Button
                size="sm"
                variant="primary"
                busy={analyzing}
                disabled={!canOperate || !mainStream}
                onClick={() => void analyze()}
              >
                Analyze {mainStream ? streamLabel(mainStream).toLowerCase() : ""} frame
              </Button>
              {!canOperate && <span className={styles.hint}>Operator role required</span>}
            </div>
            {answer && (
              <div className={styles.answer} aria-live="polite">
                <div className={styles.answerMeta}>
                  {formatEventTime(answer.at)} · {streamLabel(answer.stream)} · {answer.model}
                </div>
                <p className={styles.answerText}>{answer.text}</p>
              </div>
            )}
          </>
        ) : (
          <p className={styles.hint}>
            Not configured. Set <code>ai.vision.api_key</code> in gateway.yaml to describe frames with Claude.
          </p>
        )}
      </section>

      <section className={styles.section} aria-label="Recent detections">
        <h3 className={styles.heading}>Recent detections</h3>
        {events.length === 0 ? (
          <p className={styles.hint}>No detections yet.</p>
        ) : (
          <ul className={styles.events}>
            {events.slice(0, VISIBLE_EVENTS).map((event) => (
              <li key={event.id}>
                <button type="button" className={styles.event} onClick={() => onOpenEvent(event)}>
                  <img className={styles.thumb} src={event.snapshot_url} alt="" loading="lazy" />
                  <span className={styles.eventText}>
                    <span className={styles.eventLabel}>
                      {labelName(event.label)} <span className={styles.muted}>{percent(event.confidence)}</span>
                    </span>
                    <span className={styles.eventMeta}>
                      {formatEventTime(event.at)} · {streamLabel(event.stream)}
                      {event.geo && ` · ${formatDistance(event.geo.distance_m)}`}
                      {event.alerted && " · alerted"}
                      {event.recording && " · recorded"}
                    </span>
                  </span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}

function Count({ label, value, tone }: { label: string; value: number; tone: string }) {
  return (
    <span className={styles.count}>
      <span className={`${styles.countDot} ${tone}`} aria-hidden />
      <span className={styles.countValue}>{value}</span>
      <span className={styles.countLabel}>{label}</span>
    </span>
  );
}
