import { useEffect, useState } from "react";
import { useAuth } from "../../context/AuthContext";
import { useDrone } from "../../context/DroneContext";
import type { Notify } from "../../hooks/useToast";
import { api } from "../../services/api";
import { errorText } from "../../services/http";
import { Button } from "../ui/Button";
import type { useConfirm } from "../ui/ConfirmDialog";
import { NumberInput, Select } from "../ui/Field";
import styles from "./FlightPanel.module.css";

/** Modes worth offering in the UI, in a sensible order (all ArduCopter). */
const COMMON_MODES = ["LOITER", "GUIDED", "AUTO", "ALT_HOLD", "POSHOLD", "BRAKE", "RTL", "SMART_RTL", "LAND", "STABILIZE"];

interface FlightPanelProps {
  notify: Notify;
  confirm: ReturnType<typeof useConfirm>;
}

/** Arm / mode / takeoff / land / RTL / pause, each acknowledged by the drone. */
export function FlightPanel({ notify, confirm }: FlightPanelProps) {
  const { canOperate } = useAuth();
  const { id, snapshot } = useDrone();
  const [modes, setModes] = useState<string[]>(COMMON_MODES);
  const [mode, setMode] = useState("LOITER");
  const [takeoffAlt, setTakeoffAlt] = useState<number | undefined>(10);
  const [busy, setBusy] = useState<string | null>(null);

  useEffect(() => {
    api
      .modes(id)
      .then((all) => setModes([...COMMON_MODES.filter((m) => all.includes(m)), ...all.filter((m) => !COMMON_MODES.includes(m))]))
      .catch(() => {});
  }, [id]);

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
      title: "Arm motors?",
      message: (
        <>
          The propellers can start spinning. Make sure the area around <strong>{snapshot?.name}</strong> is clear.
        </>
      ),
      confirmLabel: "Arm",
      danger: true,
    });
    if (ok) await run("arm", () => api.arm(id), "Armed");
  };

  const disarm = async () => {
    if (inAir) {
      const ok = await confirm.ask({
        title: "Disarm in the air?",
        message: "The drone reports it is flying. ArduPilot normally refuses this; if it does not, the drone will fall. Land first.",
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
      message: "The drone switches to GUIDED and climbs vertically to this height above home.",
      confirmLabel: "Take off",
      danger: true,
    });
    if (ok) await run("takeoff", () => api.takeoff(id, takeoffAlt), `Taking off to ${takeoffAlt} m`);
  };

  return (
    <div className={styles.panel}>
      {!canOperate && <p className={styles.notice}>View only: an operator login is needed to fly.</p>}
      {canOperate && !online && <p className={styles.notice}>Drone offline: commands are disabled.</p>}

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

      <div className={styles.row}>
        <Button variant="danger" className={styles.grow} busy={busy === "arm"} disabled={disabled || armed} onClick={arm}>
          Arm
        </Button>
        <Button className={styles.grow} busy={busy === "disarm"} disabled={disabled || !armed} onClick={disarm}>
          Disarm
        </Button>
      </div>

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
        <Button variant="primary" className={styles.grow} busy={busy === "takeoff"} disabled={disabled || !armed} onClick={takeoff}>
          Take off
        </Button>
      </div>

      <div className={styles.row}>
        <Button className={styles.grow} busy={busy === "land"} disabled={disabled} onClick={() => run("land", () => api.land(id), "Landing")}>
          Land
        </Button>
        <Button className={styles.grow} busy={busy === "rtl"} disabled={disabled} onClick={() => run("rtl", () => api.rtl(id), "Returning home")}>
          RTL
        </Button>
        <Button className={styles.grow} busy={busy === "pause"} disabled={disabled} onClick={() => run("pause", () => api.pause(id), "Paused")}>
          Pause
        </Button>
        <Button className={styles.grow} busy={busy === "resume"} disabled={disabled} onClick={() => run("resume", () => api.resume(id), "Resumed")}>
          Resume
        </Button>
      </div>
    </div>
  );
}
