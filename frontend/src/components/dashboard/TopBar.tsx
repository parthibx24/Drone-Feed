import logo from "../../assets/brand/logo.png";
import arrowDown from "../../assets/icons/arrow-down.svg";
import policeBadge from "../../assets/icons/police-badge.svg";
import type { LinkState } from "../../types/drone";
import styles from "./TopBar.module.css";

export interface LinkStatus {
  label: string;
  state: LinkState;
}

interface TopBarProps {
  droneName: string;
  links: LinkStatus[];
}

export function TopBar({ droneName, links }: TopBarProps) {
  return (
    <header className={styles.bar}>
      <div className={styles.logoFrame}>
        <img className={styles.logo} src={logo} alt="StarWatch" />
      </div>

      <div className={styles.links}>
        {links.map((link) => (
          <div key={link.label} className={styles.link}>
            <span className={`${styles.dot} ${styles[link.state]}`} />
            {link.label}
          </div>
        ))}
      </div>

      <div className={styles.account}>
        <span className={styles.avatar}>
          <img className={styles.badge} src={policeBadge} alt="" />
        </span>
        <span className={styles.droneName}>{droneName}</span>
        <img className={styles.arrow} src={arrowDown} alt="" />
      </div>
    </header>
  );
}
