import padArtwork from "../../assets/gimbal/pad.svg";
import type { GimbalControls } from "../../hooks/useGimbal";
import { usePressHold } from "../../hooks/usePressHold";
import { Panel } from "../ui/Panel";
import styles from "./GimbalPad.module.css";

const DIRECTIONS = [
  { id: "up", label: "Up", yaw: 0, pitch: 1 },
  { id: "left", label: "Left", yaw: -1, pitch: 0 },
  { id: "right", label: "Right", yaw: 1, pitch: 0 },
  { id: "down", label: "Down", yaw: 0, pitch: -1 },
] as const;

interface DirectionButtonProps {
  id: (typeof DIRECTIONS)[number]["id"];
  label: string;
  onPress: () => void;
  onRelease: () => void;
}

function DirectionButton({ id, label, onPress, onRelease }: DirectionButtonProps) {
  const { active, pressProps } = usePressHold(onPress, onRelease);
  return (
    <button
      type="button"
      className={`${styles.direction} ${styles[id]} ${active ? styles.active : ""}`}
      aria-label={label}
      title={label}
      {...pressProps}
    />
  );
}

type GimbalPadProps = Pick<GimbalControls, "move" | "stop" | "center">;

export function GimbalPad({ move, stop, center }: GimbalPadProps) {
  return (
    <div className={styles.pad}>
      <Panel className={styles.artwork}>
        <img className={styles.image} src={padArtwork} alt="" />
      </Panel>
      {DIRECTIONS.map(({ id, label, yaw, pitch }) => (
        <DirectionButton
          key={id}
          id={id}
          label={label}
          onPress={() => move(yaw, pitch)}
          onRelease={stop}
        />
      ))}
      <button
        type="button"
        className={styles.center}
        aria-label="Center"
        title="Center"
        onClick={center}
      />
    </div>
  );
}
