import { PanelTitle } from "../ui/Panel";
import type { Stat } from "../../utils/format";
import styles from "./StatsPanel.module.css";

function StatCard({ label, value }: Stat) {
  return (
    <div className={styles.card}>
      <span className={styles.label}>{label}</span>
      <span className={styles.value}>{value}</span>
    </div>
  );
}

export function StatsPanel({ stats }: { stats: Stat[] }) {
  return (
    <div className={styles.section}>
      <PanelTitle className={styles.title}>Real-Time Statistics</PanelTitle>
      <div className={styles.grid}>
        {stats.map((stat) => (
          <StatCard key={stat.label} {...stat} />
        ))}
      </div>
    </div>
  );
}
