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

const MODE_HINT: Record<AvoidanceMode, string> = {
  brake: "Stops before obstacles it senses",
  bypass: "Flies around obstacles where it can, else stops",
  off: "Won't stop for obstacles: keep the aircraft in sight",
};

const MODE_DONE: Record<AvoidanceMode, string> = {
  brake: "Obstacle avoidance set to Brake",
  bypass: "Obstacle avoidance set to Bypass",
  off: "Obstacle avoidance off",
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
 * Same one-setting-per-row layout as the Thermal tab.
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
        title: "Turn off obstacle avoidance?",
        message: (
          <>
            The aircraft <strong>won't stop for obstacles</strong> in any direction until you turn avoidance back on. Keep it
            in sight.
          </>
        ),
        confirmLabel: "Turn off",
        danger: true,
        focusCancel: true,
      });
      if (!ok) return;
    }
    await run({ kind: "obstacle_avoidance", mode }, MODE_DONE[mode]);
  };

  const notice = !live ? offlineReason : !canOperate ? "View only: an operator login is needed to change settings" : null;
  const fixTone = !rtk?.enabled ? "" : rtk.fix === "fixed" ? styles.dotOk : rtk.fix === "none" ? "" : styles.dotWarn;

  return (
    <div className={`${styles.panel} ${live ? "" : styles.stale}`}>
      {notice && (
        <div className={`${styles.notice} ${!live ? styles.warn : ""}`} role="status">
          {notice}
        </div>
      )}

      {has("obstacle_avoidance") && (
        <>
          <div className={styles.setting}>
            <span className={styles.label}>Avoidance</span>
            <SegmentedControl
              size="sm"
              ariaLabel="Obstacle avoidance"
              options={MODES}
              value={avoidance?.mode ?? ""}
              disabled={!can || !avoidance || pending === "obstacle_avoidance"}
              onChange={(mode) => void setAvoidance(mode as AvoidanceMode)}
            />
            <span className={`${styles.sub} ${avoidance?.mode === "off" ? styles.problem : ""}`}>
              {avoidance ? MODE_HINT[avoidance.mode] : "Not reported yet"}
            </span>
          </div>
          <div className={styles.setting}>
            <span className={styles.label}>Sensing</span>
            {avoidance ? (
              <ul className={styles.directions}>
                <Direction label="Horizontal" sensing={avoidance.horizontal} />
                <Direction label="Up" sensing={avoidance.up} />
                <Direction label="Down" sensing={avoidance.down} />
              </ul>
            ) : (
              <span className={styles.value}>—</span>
            )}
          </div>
        </>
      )}

      {has("rtk") && (
        <div className={styles.setting}>
          <span className={styles.label}>RTK</span>
          <div className={styles.control}>
            {!rtk ? (
              <span className={styles.value}>Not reported yet</span>
            ) : !rtk.present ? (
              <span className={`${styles.value} ${styles.muted}`}>No module fitted</span>
            ) : (
              <span className={`${styles.value} ${rtk.enabled ? "" : styles.muted}`}>
                {rtk.enabled && <span className={`${styles.dot} ${fixTone}`} aria-hidden />}
                {rtk.enabled ? `${FIX_LABEL[rtk.fix] ?? rtk.fix}, ${rtk.sats} satellites` : "Off"}
              </span>
            )}
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
          {rtk?.present && rtk.enabled && rtk.fix !== "fixed" && <span className={styles.sub}>GNSS only until the fix is Fixed</span>}
        </div>
      )}

      <p className={styles.note}>
        Health warnings from the aircraft appear in <Link to={`/drones/${encodeURIComponent(droneId)}/messages`}>Messages</Link>.
      </p>
    </div>
  );
}

function Direction({ label, sensing }: { label: string; sensing: boolean }) {
  return (
    <li className={`${styles.direction} ${sensing ? "" : styles.muted}`}>
      <span className={`${styles.dot} ${sensing ? styles.dotOk : styles.dotOff}`} aria-hidden />
      {label}
      <span className={styles.srOnly}>{sensing ? ": sensing" : ": not sensing"}</span>
    </li>
  );
}
