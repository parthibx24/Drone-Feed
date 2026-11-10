import type { ReactNode } from "react";
import styles from "./PageFrame.module.css";

/** The fixed 1880x1200 design canvas every page uses (scaled via rem). */
export function PageFrame({ header, children }: { header: ReactNode; children: ReactNode }) {
  return (
    <div className={styles.page}>
      {header}
      <main className={styles.content}>{children}</main>
    </div>
  );
}
