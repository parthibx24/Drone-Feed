import addIcon from "../../assets/icons/add.svg";
import minusIcon from "../../assets/icons/minus.svg";
import type { GimbalControls } from "../../hooks/useGimbal";
import { useHoldRepeat } from "../../hooks/useHoldRepeat";
import { ControlGroup } from "../ui/ControlGroup";
import { IconButton } from "../ui/IconButton";
import styles from "./ZoomControl.module.css";

type ZoomControlProps = Pick<GimbalControls, "zoom" | "zoomStep" | "zoomReset">;

export function ZoomControl({ zoom, zoomStep, zoomReset }: ZoomControlProps) {
  const zoomIn = useHoldRepeat(() => zoomStep("in"));
  const zoomOut = useHoldRepeat(() => zoomStep("out"));

  return (
    <ControlGroup className={styles.group}>
      <IconButton icon={addIcon} label="Zoom in" active={zoomIn.active} {...zoomIn.pressProps} />
      <button type="button" className={styles.level} title="Reset zoom (1×)" onClick={zoomReset}>
        {Math.round(zoom * 100)}%
      </button>
      <IconButton icon={minusIcon} label="Zoom out" active={zoomOut.active} {...zoomOut.pressProps} />
    </ControlGroup>
  );
}
