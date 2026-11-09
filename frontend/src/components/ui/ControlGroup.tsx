import type { HTMLAttributes } from "react";
import styles from "./ControlGroup.module.css";

interface ControlGroupProps extends HTMLAttributes<HTMLDivElement> {
  strong?: boolean;
}

/** The dark rounded tray that holds the controls floating over the video. */
export function ControlGroup({ strong = false, className = "", ...props }: ControlGroupProps) {
  return (
    <div className={`${styles.group} ${strong ? styles.strong : ""} ${className}`} {...props} />
  );
}
