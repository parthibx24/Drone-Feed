import { useMemo, useState } from "react";
import { useAuth } from "../../context/AuthContext";
import { useDrone } from "../../context/DroneContext";
import type { Notify } from "../../hooks/useToast";
import { api } from "../../services/api";
import { errorText } from "../../services/http";
import { Button } from "../ui/Button";
import type { useConfirm } from "../ui/ConfirmDialog";
import { NumberInput, Select } from "../ui/Field";
import { Presets } from "../ui/Presets";
import styles from "./FlightPanel.module.css";

/** Take-off heights offered as one-tap presets. */
const TAKEOFF_PRESETS_M = [5, 10, 20, 30, 50];

/** Modes worth offering first, in a sensible order (all ArduCopter). */
const COMMON_MODES = ["LOITER", "GUIDED", "AUTO", "ALT_HOLD", "POSHOLD", "BRAKE", "RTL", "SMART_RTL", "LAND", "STABILIZE"];

interface FlightPanelProps {
  notify: Notify;
  confirm: ReturnType<typeof useConfirm>;
}

/**
 * Arm / mode / takeoff / land / RTL / pause, each acknowledged by the drone.
 * Only the commands the drone offers are shown (capabilities): a DJI
 * aircraft has no modes to pick, and none of these while its link is DJI
 * Pilot 2.
 */
export function FlightPanel({ notify, confirm }: FlightPanelProps) {
  const { canOperate } = useAuth();
  const { id, snapshot, capabilities, can } = useDrone();
  const modes = useMemo(() => {
    const all = capabilities?.modes ?? [];
    return [...COMMON_MODES.filter((m) => all.includes(m)), ...all.filter((m) => !COMMON_MODES.includes(m))];
  }, [capabilities]);
  const [picked, setMode] = useState<string | null>(null);
  const mode = picked && modes.includes(picked) ? picked : (modes[0] ?? "");
  const [takeoffAlt, setTakeoffAlt] = useState<number | undefined>(10);
  const [busy, setBusy] = useState<string | null>(null);
  const dji = capabilities?.autopilot === "dji";
  const showModes = can("set_mode") && modes.length > 0;
  const actions = (["land", "rtl", "pause", "resume"] as const).filter(can);
  const anyFlight = showModes || can("arm") || can("takeoff") || actions.length > 0;

  const online = Boolean(snapshot?.online);
  const armed = Boolean(snapshot?.armed);
  const inAir = snapshot?.landed_state === "IN_AIR" || (snapshot?.position?.alt_rel_m ?? 0) > 1;
  const disabled = !canOperate || !online || busy !== null;

  const run = async (key: string, action: () => Promise<unknown>, success: string) => {
    setBusy(key);
    try {
      await action();
      notify(success);
    } catch (error) {
      notify(errorText(error));
    } finally {
      setBusy(null);
    }
  };

  const arm = async () => {
    const ok = await confirm.ask({
      title: dji ? "Take control?" : "Arm motors?",
      message: dji ? (
        <>
          The pilot is asked on the remote controller to hand control of <strong>{snapshot?.name}</strong> to StarWatch.
          Take off next.
        </>
      ) : (
        <>
          The propellers can start spinning. Make sure the area around <strong>{snapshot?.name}</strong> is clear.
        </>
      ),
      confirmLabel: dji ? "Ask the pilot" : "Arm",
      danger: true,
    });
    if (ok) await run("arm", () => api.arm(id), "Armed");
  };

  const disarm = async () => {
    if (inAir) {
      const ok = await confirm.ask({
        title: "Disarm in the air?",
        message: "The drone reports it is flying. The autopilot normally refuses this; if it does not, the drone will fall. Land first.",
        confirmLabel: "Try to disarm",
        danger: true,
      });
      if (!ok) return;
    }
    await run("disarm", () => api.disarm(id), "Disarmed");
  };

  // AUTO starts (or resumes) the uploaded mission, so it gets the same
  // confirmation as "Start" in the mission planner.
  const changeMode = async () => {
    if (mode === "AUTO" && armed) {
      const ok = await confirm.ask({
        title: "Switch to AUTO?",
        message: "The drone starts flying the mission stored on it, from its current item.",
        confirmLabel: "Switch to AUTO",
        danger: true,
      });
      if (!ok) return;
    }
    await run("mode", () => api.setMode(id, mode), `Mode ${mode}`);
  };

  const takeoff = async () => {
    if (!takeoffAlt || takeoffAlt <= 0) return notify("Enter a takeoff altitude");
    const ok = await confirm.ask({
      title: `Take off to ${takeoffAlt} m?`,
      message: dji
        ? "The aircraft takes off and climbs to this height above the take-off point."
        : "The drone switches to GUIDED and climbs vertically to this height above home.",
      confirmLabel: "Take off",
      danger: true,
    });
    if (ok) await run("takeoff", () => api.takeoff(id, takeoffAlt), `Taking off to ${takeoffAlt} m`);
  };

  const labels = { land: "Land", rtl: dji ? "Return home" : "RTL", pause: "Pause", resume: "Resume" } as const;
  const success = { land: "Landing", rtl: "Returning home", pause: "Paused", resume: "Resumed" } as const;
  const calls = { land: api.land, rtl: api.rtl, pause: api.pause, resume: api.resume } as const;

  return (
    <div className={styles.panel}>
      {!canOperate && <p className={styles.notice}>View only: an operator login is needed to fly.</p>}
      {canOperate && !online && anyFlight && <p className={styles.notice}>Drone offline: commands are disabled.</p>}
      {capabilities?.note && <p className={styles.notice}>{capabilities.note}</p>}
      {capabilities && !anyFlight && !capabilities.note && (
        <p className={styles.notice}>This drone takes no flight commands from the dashboard.</p>
      )}

      {showModes && (
        <div className={styles.row}>
          <Select value={mode} onChange={(e) => setMode(e.target.value)} disabled={disabled} aria-label="Flight mode">
            {modes.map((m) => (
              <option key={m} value={m}>
                {m}
              </option>
            ))}
          </Select>
          <Button busy={busy === "mode"} disabled={disabled} onClick={changeMode}>
            Set mode
          </Button>
        </div>
      )}

      {can("arm") && (
        <div className={styles.row}>
          <Button variant="danger" className={styles.grow} busy={busy === "arm"} disabled={disabled || armed} onClick={arm}>
            {dji ? "Take control" : "Arm"}
          </Button>
          <Button className={styles.grow} busy={busy === "disarm"} disabled={disabled || !armed} onClick={disarm}>
            {dji ? "Release" : "Disarm"}
          </Button>
        </div>
      )}

      {can("takeoff") && (
        <div className={styles.row}>
          <NumberInput
            value={takeoffAlt}
            onValue={setTakeoffAlt}
            min={1}
            max={120}
            step={1}
            disabled={disabled}
            aria-label="Takeoff altitude (m)"
            className={styles.alt}
          />
          <span className={styles.unit}>m</span>
          <Button
            variant="primary"
            className={styles.grow}
            busy={busy === "takeoff"}
            disabled={disabled || (can("arm") && !armed)}
            onClick={takeoff}
          >
            Take off
          </Button>
        </div>
      )}
      {can("takeoff") && (
        <Presets
          values={TAKEOFF_PRESETS_M}
          value={takeoffAlt}
          onPick={setTakeoffAlt}
          unit="m"
          disabled={disabled}
          ariaLabel="Takeoff altitude"
        />
      )}

      {actions.length > 0 && (
        <div className={styles.row}>
          {actions.map((a) => (
            <Button
              key={a}
              className={styles.grow}
              busy={busy === a}
              disabled={disabled}
              onClick={() => run(a, () => calls[a](id), success[a])}
            >
              {labels[a]}
            </Button>
          ))}
        </div>
      )}
    </div>
  );
}
