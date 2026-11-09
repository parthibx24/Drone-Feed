import type { Telemetry } from "../../types/drone";
import { EMPTY_VALUE, formatTelemetryAge } from "../../utils/format";
import styles from "./FlightStatusChips.module.css";

export function FlightStatusChips({ telemetry }: { telemetry: Telemetry }) {
  const { flight_mode: mode, armed, system_status: status, timestamp } = telemetry;

  return (
    <div className={styles.chips}>
      <span className={styles.chip}>{mode ?? EMPTY_VALUE}</span>
      <span className={`${styles.chip} ${armed ? styles.armed : styles.disarmed}`}>
        {armed ? "ARMED" : "DISARMED"}
      </span>
      {status && <span className={styles.chip}>{status}</span>}
      <span className={`${styles.chip} ${styles.muted}`}>Updated: {formatTelemetryAge(timestamp)}</span>
    </div>
  );
}
