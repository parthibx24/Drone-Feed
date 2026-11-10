import { useEffect, useState } from "react";
import { Navigate, useNavigate } from "react-router";
import { TopBar } from "../../components/dashboard/TopBar";
import { PageFrame } from "../../components/layout/PageFrame";
import { Panel, PanelTitle } from "../../components/ui/Panel";
import { api } from "../../services/api";
import { errorText } from "../../services/http";
import type { DroneSummary } from "../../types/gateway";
import { EMPTY_VALUE } from "../../utils/format";
import styles from "./FleetPage.module.css";

const POLL_MS = 3000;

/** Old bookmarks used /?host=<ip>&name=<prefix>; send them to the matching drone. */
function legacyTarget(drones: DroneSummary[]): string | null {
  const params = new URLSearchParams(window.location.search);
  const name = params.get("name")?.replace(/^["']|["']$/g, "");
  if (!name) return null;
  const match = drones.find((d) => d.stream_prefix === name || d.id === name || d.name === name);
  return match ? `/drones/${encodeURIComponent(match.id)}` : null;
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className={styles.stat}>
      <span className={styles.statLabel}>{label}</span>
      <span className={styles.statValue}>{value}</span>
    </div>
  );
}

function DroneCard({ drone, onOpen }: { drone: DroneSummary; onOpen: () => void }) {
  const t = drone.telemetry;
  const battery = t.battery?.remaining_pct != null
    ? `${t.battery.remaining_pct}%`
    : t.battery?.voltage_v != null
      ? `${t.battery.voltage_v.toFixed(1)}V`
      : EMPTY_VALUE;
  const gps = t.gps ? `${t.gps.fix_type.replace("_FIX", "")} · ${t.gps.satellites}` : EMPTY_VALUE;
  const alt = t.position ? `${t.position.alt_rel_m.toFixed(1)}m` : EMPTY_VALUE;

  return (
    <button type="button" className={styles.cardButton} onClick={onOpen}>
      <Panel className={styles.card}>
        <div className={styles.cardHeader}>
          <div>
            <div className={styles.name}>{drone.name}</div>
            <div className={styles.id}>{drone.id}{t.firmware ? ` · ArduCopter ${t.firmware}` : ""}</div>
          </div>
          <span className={`${styles.status} ${drone.online ? styles.online : styles.offline}`}>
            <span className={styles.dot} />
            {drone.online ? "Online" : "Offline"}
          </span>
        </div>
        <div className={styles.chips}>
          <span className={styles.chip}>{drone.online ? drone.mode : EMPTY_VALUE}</span>
          {drone.online && (
            <span className={`${styles.chip} ${drone.armed ? styles.armed : ""}`}>{drone.armed ? "ARMED" : "DISARMED"}</span>
          )}
        </div>
        <div className={styles.stats}>
          <Stat label="Battery" value={battery} />
          <Stat label="GPS" value={gps} />
          <Stat label="Altitude" value={alt} />
        </div>
      </Panel>
    </button>
  );
}

export function FleetPage() {
  const navigate = useNavigate();
  const [drones, setDrones] = useState<DroneSummary[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    const load = () =>
      api
        .drones()
        .then((list) => {
          if (!cancelled) {
            setDrones(list);
            setError(null);
          }
        })
        .catch((err) => !cancelled && setError(errorText(err)));
    void load();
    const timer = setInterval(load, POLL_MS);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, []);

  const legacy = drones ? legacyTarget(drones) : null;
  if (legacy) return <Navigate to={legacy} replace />;

  return (
    <PageFrame header={<TopBar title="Fleet" />}>
      <div className={styles.wrap}>
        <PanelTitle className={styles.heading}>Drones</PanelTitle>
        {error && <p className={styles.message}>Could not load drones: {error}</p>}
        {!drones && !error && <p className={styles.message}>Loading…</p>}
        {drones?.length === 0 && <p className={styles.message}>No drones are configured on the server.</p>}
        <div className={styles.grid}>
          {drones?.map((drone) => (
            <DroneCard key={drone.id} drone={drone} onOpen={() => navigate(`/drones/${encodeURIComponent(drone.id)}`)} />
          ))}
        </div>
      </div>
    </PageFrame>
  );
}
