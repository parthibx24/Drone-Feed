import { useElapsedSeconds } from "../../hooks/useElapsedSeconds";
import type { RecordingControls } from "../../hooks/useRecording";
import type { RecordingState } from "../../types/recording";
import { formatClock } from "../../utils/format";
import { ControlGroup } from "../ui/ControlGroup";
import styles from "./RecordButton.module.css";

const LABELS: Record<Exclude<RecordingState, "recording">, string> = {
  idle: "Start Record",
  starting: "Starting…",
  waiting: "Waiting…",
  saving: "Saving…",
};

const TITLES: Partial<Record<RecordingState, string>> = {
  idle: "Record all camera feeds to a single MKV (zero-encoding)",
  waiting: "Waiting for active camera feed from go2rtc… Click to cancel.",
  recording: "Stop recording",
};

export function RecordButton({ recording, disabled = false }: { recording: RecordingControls; disabled?: boolean }) {
  const { state, startTime, toggle } = recording;
  const elapsed = useElapsedSeconds(state === "recording" ? startTime : null);
  const label = state === "recording" ? `Stop ${formatClock(elapsed)}` : LABELS[state];

  return (
    <ControlGroup strong className={styles.group}>
      <button
        type="button"
        className={`${styles.button} ${styles[state]}`}
        title={TITLES[state]}
        disabled={disabled || state === "starting" || state === "saving"}
        onClick={toggle}
      >
        {label}
      </button>
    </ControlGroup>
  );
}
