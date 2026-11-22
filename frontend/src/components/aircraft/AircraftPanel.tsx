import { Link } from "react-router";
import type { AircraftControls } from "../../hooks/useAircraftFeatures";
import type { AircraftCommand, AvoidanceMode, RtkFix } from "../../types/gateway";
import type { ConfirmOptions } from "../ui/ConfirmDialog";
import { SegmentedControl } from "../ui/SegmentedControl";
import { Switch } from "../ui/Switch";
import styles from "./AircraftPanels.module.css";

interface AircraftPanelProps {
  droneId: string;
  aircraft: AircraftControls;
  canOperate: boolean;
  confirm: (options: ConfirmOptions) => Promise<boolean>;
  notify: (message: string) => void;
}

const MODES: { value: AvoidanceMode; label: string }[] = [
  { value: "brake", label: "Brake" },
  { value: "bypass", label: "Bypass" },
  { value: "off", label: "Off" },
];

const MODE_LABEL: Record<AvoidanceMode, string> = { brake: "Brake", bypass: "Bypass", off: "Off" };

const MODE_HINT: Record<AvoidanceMode, string> = {
  brake: "The aircraft stops before an obstacle it senses.",
  bypass: "The aircraft flies around obstacles it senses, where it can, and stops where it can't.",
  off: "The aircraft does not stop for obstacles. Keep it in sight.",
};

const FIX_LABEL: Record<RtkFix, string> = {
  none: "No fix",
  single: "Single point",
  float: "Float",
  fixed: "Fixed",
};

/**
 * The aircraft's flight-safety settings that MAVLink has no message for:
 * obstacle avoidance and the RTK module. Health warnings go to Messages.
 */
export function AircraftPanel({ droneId, aircraft, canOperate, confirm, notify }: AircraftPanelProps) {
  const { state, live, offlineReason, has, pending, send } = aircraft;
  const avoidance = state?.avoidance ?? null;
  const rtk = state?.rtk ?? null;
  const can = canOperate && live;

  const run = async (command: AircraftCommand, done: string) => {
    const reason = await send(command);
    notify(reason ?? done);
  };

  const setAvoidance = async (mode: AvoidanceMode) => {
    if (mode === avoidance?.mode) return;
    if (mode === "off") {
      const ok = await confirm({
        title: "Turn obstacle avoidance off?",
        message: (
          <>
            The aircraft will <strong>not stop for obstacles</strong> in any direction until avoidance is turned back on. Fly
            with the aircraft in sight.
          </>
        ),
        confirmLabel: "Turn off",
        danger: true,
      });
      if (!ok) return;
    }
    await run({ kind: "obstacle_avoidance", mode }, `Obstacle avoidance set to ${MODE_LABEL[mode]}`);
  };

  const status = !live ? offlineReason : !canOperate ? "View only: changing settings needs the operator role" : null;

  return (
    <div className={styles.panel}>
      <div className={`${styles.status} ${!live ? styles.warn : ""}`} role="status">
        <span className={`${styles.dot} ${live ? styles.dotOk : ""}`} aria-hidden />
        {status ?? "Aircraft connected"}
      </div>

      {has("obstacle_avoidance") && (
        <section className={styles.section} aria-label="Obstacle avoidance">
          <div className={styles.heading}>Obstacle avoidance</div>
          <div className={styles.segmented}>
            <SegmentedControl
              ariaLabel="Obstacle avoidance"
              options={MODES}
              value={avoidance?.mode ?? ""}
              disabled={!can || !avoidance || pending === "obstacle_avoidance"}
              onChange={(mode) => void setAvoidance(mode as AvoidanceMode)}
            />
          </div>
          <div className={avoidance?.mode === "off" ? styles.problem : styles.hint}>
            {avoidance ? MODE_HINT[avoidance.mode] : "The aircraft hasn't reported its avoidance setting yet."}
          </div>
          {avoidance && (
            <div className={styles.tiles}>
              <Direction label="Horizontal" sensing={avoidance.horizontal} />
              <Direction label="Up" sensing={avoidance.up} />
              <Direction label="Down" sensing={avoidance.down} />
            </div>
          )}
        </section>
      )}

      {has("rtk") && (
        <section className={styles.section} aria-label="RTK">
          <div className={styles.heading}>
            RTK positioning
            {rtk?.present && (
              <Switch
                label="RTK positioning"
                checked={rtk.enabled}
                disabled={!can}
                busy={pending === "rtk_enable"}
                onChange={(enabled) => void run({ kind: "rtk_enable", enabled }, enabled ? "RTK on" : "RTK off")}
              />
            )}
          </div>
          {!rtk ? (
            <div className={styles.hint}>The aircraft hasn't reported its RTK module yet.</div>
          ) : !rtk.present ? (
            <div className={styles.hint}>No RTK module on the aircraft. Fit one to get centimetre positioning.</div>
          ) : (
            <>
              <div className={styles.tiles}>
                <div className={styles.tile}>
                  <span className={styles.tileLabel}>Module</span>
                  <span className={styles.tileValue}>{rtk.enabled ? "On" : "Off"}</span>
                </div>
                <div className={styles.tile}>
                  <span className={styles.tileLabel}>Fix</span>
                  <span className={`${styles.tileValue} ${rtk.enabled && rtk.fix !== "fixed" ? styles.tileOff : ""}`}>
                    {rtk.enabled ? FIX_LABEL[rtk.fix] ?? rtk.fix : "—"}
                  </span>
                </div>
                <div className={styles.tile}>
                  <span className={styles.tileLabel}>Satellites</span>
                  <span className={styles.tileValue}>{rtk.enabled ? rtk.sats : "—"}</span>
                </div>
              </div>
              <div className={styles.hint}>
                {rtk.enabled
                  ? rtk.fix === "fixed"
                    ? "Positions are corrected to a few centimetres."
                    : "Waiting for a fixed solution: positions are GNSS only until then."
                  : "Turn RTK on to correct positions with the module's data."}
              </div>
            </>
          )}
        </section>
      )}

      <p className={styles.note}>
        Health warnings from the aircraft appear in <Link to={`/drones/${encodeURIComponent(droneId)}/messages`}>Messages</Link>.
      </p>
    </div>
  );
}

function Direction({ label, sensing }: { label: string; sensing: boolean }) {
  return (
    <div className={styles.tile}>
      <span className={styles.tileLabel}>{label}</span>
      <span className={`${styles.tileValue} ${sensing ? "" : styles.tileOff}`}>
        <span className={`${styles.dot} ${sensing ? styles.dotOk : ""}`} aria-hidden />
        {sensing ? "Sensing" : "Not sensing"}
      </span>
    </div>
  );
}
