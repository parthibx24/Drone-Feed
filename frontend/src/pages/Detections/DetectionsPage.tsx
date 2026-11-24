import { useCallback, useEffect, useState } from "react";
import { EventDetails } from "../../components/ai/EventDetails";
import { DroneTopBar } from "../../components/layout/DroneTopBar";
import { PageFrame } from "../../components/layout/PageFrame";
import { Button } from "../../components/ui/Button";
import { Field, Select, TextInput } from "../../components/ui/Field";
import { Panel, PanelTitle } from "../../components/ui/Panel";
import { useDrone } from "../../context/DroneContext";
import { api } from "../../services/api";
import { errorText } from "../../services/http";
import { AI_LABELS, type AIEvent, type AIEventQuery } from "../../types/ai";
import type { DroneSummary } from "../../types/gateway";
import { formatDistance, formatEventDateTime, groupColor, labelName, percent, streamLabel } from "../../utils/ai";
import styles from "./DetectionsPage.module.css";

const LIMIT = 500;
const AUTO_REFRESH_MS = 30_000;

const RANGES = [
  { value: "1h", label: "Last hour", ms: 3_600_000 },
  { value: "24h", label: "Last 24 hours", ms: 86_400_000 },
  { value: "7d", label: "Last 7 days", ms: 7 * 86_400_000 },
  { value: "custom", label: "Custom range", ms: 0 },
] as const;
type Range = (typeof RANGES)[number]["value"];

const GROUPS = [
  { value: "", label: "Everything" },
  { value: "person", label: "People" },
  { value: "vehicle", label: "Vehicles" },
  { value: "other", label: "Other" },
];

/** datetime-local value ("2026-11-11T14:05") → RFC 3339 in UTC. */
function localToIso(value: string): string | undefined {
  if (!value) return undefined;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString();
}

/** Searchable log of everything the detector reported, with snapshots. */
export function DetectionsPage() {
  const { id } = useDrone();
  const [drones, setDrones] = useState<DroneSummary[]>([]);
  const [drone, setDrone] = useState(id);
  const [group, setGroup] = useState("");
  const [label, setLabel] = useState("");
  const [range, setRange] = useState<Range>("24h");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [events, setEvents] = useState<AIEvent[]>([]);
  const [selected, setSelected] = useState<AIEvent | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api.drones().then(setDrones).catch(() => {});
  }, []);

  const load = useCallback(async () => {
    const q: AIEventQuery = { drone: drone || undefined, group: group || undefined, label: label || undefined, limit: LIMIT };
    if (range === "custom") {
      q.since = localToIso(from);
      q.until = localToIso(to);
    } else {
      q.since = new Date(Date.now() - RANGES.find((r) => r.value === range)!.ms).toISOString();
    }
    setLoading(true);
    try {
      const res = await api.aiEvents(q);
      const list = res.events ?? [];
      setEvents(list);
      setError(null);
      setSelected((current) => (current ? (list.find((e) => e.id === current.id) ?? current) : null));
    } catch (err) {
      setError(errorText(err));
    } finally {
      setLoading(false);
    }
  }, [drone, group, label, range, from, to]);

  useEffect(() => {
    void load();
    if (range === "custom") return;
    const timer = setInterval(() => void load(), AUTO_REFRESH_MS);
    return () => clearInterval(timer);
  }, [load, range]);

  return (
    <PageFrame header={<DroneTopBar />}>
      <Panel className={styles.panel}>
        <div className={styles.header}>
          <PanelTitle>Detections</PanelTitle>
          <span className={styles.count}>
            {loading ? "Loading…" : `${events.length}${events.length === LIMIT ? "+" : ""} shown, newest first`}
          </span>
          <Button size="sm" className={styles.refresh} onClick={() => void load()} busy={loading}>
            Refresh
          </Button>
        </div>

        <div className={styles.filters}>
          <Field label="Drone">
            <Select value={drone} onChange={(e) => setDrone(e.target.value)}>
              <option value="">All drones</option>
              {drones.map((d) => (
                <option key={d.id} value={d.id}>
                  {d.name || d.id}
                </option>
              ))}
              {!drones.some((d) => d.id === id) && <option value={id}>{id}</option>}
            </Select>
          </Field>
          <Field label="Type">
            <Select value={group} onChange={(e) => setGroup(e.target.value)}>
              {GROUPS.map((g) => (
                <option key={g.value} value={g.value}>
                  {g.label}
                </option>
              ))}
            </Select>
          </Field>
          <Field label="Label">
            <Select value={label} onChange={(e) => setLabel(e.target.value)}>
              <option value="">Any label</option>
              {AI_LABELS.map((l) => (
                <option key={l} value={l}>
                  {labelName(l)}
                </option>
              ))}
            </Select>
          </Field>
          <Field label="Time">
            <Select value={range} onChange={(e) => setRange(e.target.value as Range)}>
              {RANGES.map((r) => (
                <option key={r.value} value={r.value}>
                  {r.label}
                </option>
              ))}
            </Select>
          </Field>
          {range === "custom" && (
            <>
              <Field label="From">
                <TextInput type="datetime-local" value={from} onChange={(e) => setFrom(e.target.value)} />
              </Field>
              <Field label="To">
                <TextInput type="datetime-local" value={to} onChange={(e) => setTo(e.target.value)} />
              </Field>
            </>
          )}
        </div>

        <div className={styles.body}>
          <div className={styles.grid}>
            {error && <p className={styles.empty}>Could not load detections: {error}</p>}
            {!error && events.length === 0 && !loading && (
              <p className={styles.empty}>No detections match these filters.</p>
            )}
            {events.map((event) => (
              <button
                key={event.id}
                type="button"
                className={`${styles.card} ${selected?.id === event.id ? styles.cardSelected : ""}`}
                onClick={() => setSelected(event)}
                aria-pressed={selected?.id === event.id}
              >
                <img className={styles.thumb} src={event.snapshot_url} alt="" loading="lazy" />
                <span className={styles.cardBody}>
                  <span className={styles.cardTitle}>
                    <span className={styles.dot} style={{ background: groupColor(event.group) }} aria-hidden />
                    {labelName(event.label)} <span className={styles.muted}>{percent(event.confidence)}</span>
                  </span>
                  <span className={styles.cardMeta}>{formatEventDateTime(event.at)}</span>
                  <span className={styles.cardMeta}>
                    {event.drone_name || event.drone_id} · {streamLabel(event.stream)}
                    {event.geo && ` · ${formatDistance(event.geo.distance_m)}`}
                  </span>
                </span>
              </button>
            ))}
          </div>

          <aside className={styles.detail} aria-label="Selected detection">
            {selected ? (
              <>
                <div className={styles.detailHeader}>
                  <h3 className={styles.detailTitle}>
                    {labelName(selected.label)} · {percent(selected.confidence)}
                  </h3>
                  <button type="button" className={styles.close} aria-label="Close details" onClick={() => setSelected(null)}>
                    ✕
                  </button>
                </div>
                <EventDetails event={selected} />
              </>
            ) : (
              <p className={styles.empty}>Select a detection to see its snapshot and position.</p>
            )}
          </aside>
        </div>
      </Panel>
    </PageFrame>
  );
}
