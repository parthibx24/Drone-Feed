import { useDrone } from "../../context/DroneContext";
import type { Telemetry } from "../../types/drone";
import { flightModeLabel } from "../../utils/modes";
import { EMPTY_VALUE, formatTelemetryAge } from "../../utils/format";
import styles from "./FlightStatusChips.module.css";

export function FlightStatusChips({ telemetry }: { telemetry: Telemetry }) {
  const { capabilities } = useDrone();
  const { armed, system_status: status, timestamp } = telemetry;
  const mode = flightModeLabel(telemetry.flight_mode, capabilities?.autopilot);

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
