import type { CSSProperties } from "react";
import headingDot from "../../assets/compass/heading-dot.svg";
import hub from "../../assets/compass/hub.svg";
import innerRing from "../../assets/compass/inner-ring.svg";
import outerRing from "../../assets/compass/outer-ring.svg";
import ticks from "../../assets/compass/ticks.svg";
import { cardinalOf, formatBearing } from "../../utils/format";
import { Panel } from "../ui/Panel";
import styles from "./Compass.module.css";

const DEGREE_MARKS = Array.from({ length: 12 }, (_, index) => index * 30);
const CARDINAL_LETTERS = ["n", "e", "s", "w"] as const;

export function Compass({ heading }: { heading: number | null | undefined }) {
  const hasHeading = heading != null;

  return (
    <Panel className={styles.panel}>
      <div className={styles.outerRing}>
        <img className={styles.fill} src={outerRing} alt="" />
      </div>
      <img className={styles.innerRing} src={innerRing} alt="" />
      <div className={styles.hub}>
        <img className={styles.fill} src={hub} alt="" />
      </div>
      <img className={styles.ticks} src={ticks} alt="" />

      {DEGREE_MARKS.map((mark) => (
        <span
          key={mark}
          className={`${styles.degree} ${mark % 90 === 0 ? styles.major : styles.minor}`}
          style={{ "--angle": `${mark}deg` } as CSSProperties}
        >
          {mark}°
        </span>
      ))}

      {CARDINAL_LETTERS.map((letter) => (
        <span key={letter} className={`${styles.letter} ${styles[letter]}`}>
          {letter.toUpperCase()}
        </span>
      ))}

      {hasHeading && (
        <div className={styles.orbit} style={{ "--heading": `${heading}deg` } as CSSProperties}>
          <div className={styles.dot}>
            <img className={styles.fill} src={headingDot} alt="" />
          </div>
        </div>
      )}

      <span className={styles.value}>{formatBearing(heading)}</span>
      {hasHeading && <span className={styles.cardinal}>{cardinalOf(heading)}</span>}
    </Panel>
  );
}
