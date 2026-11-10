import { useMemo, useState } from "react";
import { DroneTopBar } from "../../components/layout/DroneTopBar";
import { PageFrame } from "../../components/layout/PageFrame";
import { Panel, PanelTitle } from "../../components/ui/Panel";
import { Select } from "../../components/ui/Field";
import { useDrone } from "../../context/DroneContext";
import styles from "./MessagesPage.module.css";

/** MAVLink severities, most severe first. */
const SEVERITIES = ["EMERGENCY", "ALERT", "CRITICAL", "ERROR", "WARNING", "NOTICE", "INFO", "DEBUG"];
const rank = (s: string) => {
  const i = SEVERITIES.indexOf(s);
  return i < 0 ? SEVERITIES.length : i;
};

const FILTERS = [
  { value: "7", label: "All messages" },
  { value: "4", label: "Warnings and worse" },
  { value: "3", label: "Errors only" },
];

function tone(severity: string): string {
  const r = rank(severity);
  if (r <= 3) return styles.error;
  if (r === 4) return styles.warning;
  return styles.info;
}

export function MessagesPage() {
  const { messages, snapshot } = useDrone();
  const [maxRank, setMaxRank] = useState("7");

  const visible = useMemo(
    () => messages.filter((m) => rank(m.severity) <= Number(maxRank)).slice().reverse(),
    [messages, maxRank],
  );

  return (
    <PageFrame header={<DroneTopBar />}>
      <Panel className={styles.panel}>
        <div className={styles.header}>
          <PanelTitle>Autopilot messages</PanelTitle>
          <span className={styles.count}>{visible.length} shown, newest first</span>
          <Select className={styles.filter} value={maxRank} onChange={(e) => setMaxRank(e.target.value)} aria-label="Filter">
            {FILTERS.map((f) => (
              <option key={f.value} value={f.value}>
                {f.label}
              </option>
            ))}
          </Select>
        </div>
        <p className={styles.hint}>
          Pre-arm failures, GPS and EKF status and failsafes appear here as the drone reports them.
          {snapshot && !snapshot.online && " The drone is offline, so no new messages are arriving."}
        </p>
        <ol className={styles.list}>
          {visible.length === 0 && <li className={styles.empty}>No messages yet.</li>}
          {visible.map((m, i) => (
            <li key={`${m.time}-${i}`} className={styles.row}>
              <time className={styles.time} dateTime={m.time}>
                {new Date(m.time).toLocaleTimeString([], { hour12: false })}
              </time>
              <span className={`${styles.severity} ${tone(m.severity)}`}>{m.severity}</span>
              <span className={styles.text}>{m.text}</span>
            </li>
          ))}
        </ol>
      </Panel>
    </PageFrame>
  );
}
