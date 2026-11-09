import type { HTMLAttributes } from "react";
import styles from "./Panel.module.css";

export function Panel({ className = "", ...props }: HTMLAttributes<HTMLElement>) {
  return <section className={`${styles.panel} ${className}`} {...props} />;
}

export function PanelTitle({ className = "", ...props }: HTMLAttributes<HTMLHeadingElement>) {
  return <h2 className={`${styles.title} ${className}`} {...props} />;
}
