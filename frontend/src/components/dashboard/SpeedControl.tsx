import type { CSSProperties } from "react";
import type { GimbalControls } from "../../hooks/useGimbal";
import { ControlGroup } from "../ui/ControlGroup";
import styles from "./SpeedControl.module.css";

const MIN_SPEED = 1;
const MAX_SPEED = 20;

type SpeedControlProps = Pick<GimbalControls, "speed" | "setSpeed">;

export function SpeedControl({ speed, setSpeed }: SpeedControlProps) {
  const progress = (speed - MIN_SPEED) / (MAX_SPEED - MIN_SPEED);

  return (
    <ControlGroup className={styles.group}>
      <label className={styles.label} htmlFor="gimbal-speed">
        Spd
      </label>
      <input
        id="gimbal-speed"
        className={styles.slider}
        type="range"
        min={MIN_SPEED}
        max={MAX_SPEED}
        step={1}
        value={speed}
        style={{ "--progress": progress } as CSSProperties}
        onChange={(event) => setSpeed(Number(event.target.value))}
      />
      <span className={styles.value}>{String(speed).padStart(2, "0")}</span>
    </ControlGroup>
  );
}
