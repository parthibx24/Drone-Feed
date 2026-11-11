import { useCallback, useEffect, useMemo, useState } from "react";
import { isRealPosition } from "../../utils/telemetry";
import { DroneTopBar } from "../../components/layout/DroneTopBar";
import { PageFrame } from "../../components/layout/PageFrame";
import { MissionMap } from "../../components/mission/MissionMap";
import { Button } from "../../components/ui/Button";
import { useConfirm } from "../../components/ui/ConfirmDialog";
import { Field, NumberInput, Select } from "../../components/ui/Field";
import { Panel, PanelTitle } from "../../components/ui/Panel";
import { Toast } from "../../components/ui/Toast";
import { useAuth } from "../../context/AuthContext";
import { useDrone } from "../../context/DroneContext";
import { useToast } from "../../hooks/useToast";
import { api } from "../../services/api";
import { errorText } from "../../services/http";
import type { Limits, MissionItem, MissionItemType } from "../../types/gateway";
import {
  ADDABLE_TYPES,
  FIELD_LABELS,
  altLabel,
  ITEM_SPECS,
  describe,
  forUpload,
  validate,
  warnings,
  type FieldKey,
} from "../../utils/mission";
import styles from "./MissionPage.module.css";

const round7 = (v: number) => Math.round(v * 1e7) / 1e7;

export function MissionPage() {
  const { canOperate } = useAuth();
  const { id, snapshot } = useDrone();
  const { message, notify } = useToast();
  const confirm = useConfirm();

  const [items, setItems] = useState<MissionItem[]>([]);
  const [selected, setSelected] = useState<number | null>(null);
  const [dirty, setDirty] = useState(false);
  const [limits, setLimits] = useState<Limits | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [addOnClick, setAddOnClick] = useState(true);
  const [defaultAlt, setDefaultAlt] = useState<number | undefined>(30);
  const [newType, setNewType] = useState<MissionItemType>("waypoint");

  const online = Boolean(snapshot?.online);
  const editable = canOperate;

  const edit = useCallback((next: MissionItem[]) => {
    setItems(next);
    setDirty(true);
  }, []);

  const download = useCallback(
    async (quiet = false) => {
      setBusy("download");
      try {
        const res = await api.mission(id);
        setItems(res.items);
        setSelected(null);
        setDirty(false);
        if (!quiet) notify(`Read ${res.items.length} items from the drone`);
      } catch (error) {
        if (!quiet) notify(errorText(error));
      } finally {
        setBusy(null);
      }
    },
    [id, notify],
  );

  useEffect(() => {
    api.limits().then(setLimits).catch(() => {});
  }, []);

  // Load the drone's mission once it is online (only if nothing is being edited).
  const [autoLoaded, setAutoLoaded] = useState(false);
  useEffect(() => {
    if (online && !autoLoaded) {
      setAutoLoaded(true);
      void download(true);
    }
  }, [online, autoLoaded, download]);

  const errors = useMemo(() => validate(items, limits), [items, limits]);
  const warns = useMemo(() => warnings(items), [items]);

  // Telemetry seq N is items[N-1] (seq 0 is home on the drone).
  const activeIndex =
    !dirty && snapshot?.mission && snapshot.mission.current_seq > 0 ? snapshot.mission.current_seq - 1 : null;
  const home: [number, number] | null = snapshot?.home ? [snapshot.home.lat, snapshot.home.lon] : null;
  const dronePos = isRealPosition(snapshot?.position)
    ? { lat: snapshot.position.lat, lon: snapshot.position.lon, heading: snapshot.position.heading_deg ?? 0 }
    : null;

  const addItem = (type: MissionItemType, lat?: number, lon?: number) => {
    const item: MissionItem = { type };
    const spec = ITEM_SPECS[type];
    if (spec.fields.includes("alt_m")) item.alt_m = defaultAlt ?? 30;
    if (type === "takeoff") item.alt_m = Math.min(defaultAlt ?? 30, 30);
    if (type === "loiter_time" || type === "delay") item.seconds = 10;
    if (type === "photo_interval") item.seconds = 2;
    if (type === "speed") item.speed_m_s = 5;
    if (lat != null && lon != null) {
      item.lat = round7(lat);
      item.lon = round7(lon);
    } else if (spec.position === "always") {
      const anchor = [...items].reverse().find((i) => i.lat != null) ?? null;
      const base = anchor ? { lat: anchor.lat!, lon: anchor.lon! } : dronePos;
      if (!base) return notify("Click the map to place this item");
      item.lat = round7(base.lat + 0.0003);
      item.lon = round7(base.lon + 0.0003);
    }
    const insertAt = selected != null ? selected + 1 : items.length;
    const next = [...items.slice(0, insertAt), item, ...items.slice(insertAt)];
    edit(next);
    setSelected(insertAt);
  };

  const updateItem = (index: number, patch: Partial<MissionItem>) =>
    edit(items.map((item, i) => (i === index ? { ...item, ...patch } : item)));

  const removeItem = (index: number) => {
    edit(items.filter((_, i) => i !== index));
    setSelected(null);
  };

  const moveItem = (index: number, delta: number) => {
    const to = index + delta;
    if (to < 0 || to >= items.length) return;
    const next = [...items];
    [next[index], next[to]] = [next[to], next[index]];
    edit(next);
    setSelected(to);
  };

  const upload = async () => {
    if (errors.length) return notify("Fix the mission errors first");
    const ok = await confirm.ask({
      title: `Write ${items.length} items to the drone?`,
      message: (
        <>
          This replaces the mission stored on the drone.
          {warns.length > 0 && (
            <ul className={styles.confirmWarnings}>
              {warns.map((w) => (
                <li key={w}>{w}</li>
              ))}
            </ul>
          )}
        </>
      ),
      confirmLabel: "Write mission",
    });
    if (!ok) return;
    setBusy("upload");
    try {
      const res = await api.uploadMission(id, forUpload(items));
      setDirty(false);
      notify(`Mission written (${res.count} items)`);
    } catch (error) {
      notify(errorText(error));
    } finally {
      setBusy(null);
    }
  };

  const clearOnDrone = async () => {
    const ok = await confirm.ask({
      title: "Delete the mission on the drone?",
      message: "The drone's stored mission is erased. Your editor is not changed.",
      confirmLabel: "Delete on drone",
      danger: true,
    });
    if (!ok) return;
    setBusy("clear");
    try {
      await api.clearMission(id);
      notify("Mission deleted on the drone");
      setDirty(true);
    } catch (error) {
      notify(errorText(error));
    } finally {
      setBusy(null);
    }
  };

  const start = async () => {
    if (dirty) return notify("Write the mission to the drone first");
    const ok = await confirm.ask({
      title: "Start the mission?",
      message: (
        <>
          The drone switches to <strong>AUTO</strong> and flies the {items.length}-item mission now.
          {!snapshot?.armed && " It must be armed first."}
        </>
      ),
      confirmLabel: "Start mission",
      danger: true,
    });
    if (!ok) return;
    setBusy("start");
    try {
      await api.startMission(id);
      notify("Mission started");
    } catch (error) {
      notify(errorText(error));
    } finally {
      setBusy(null);
    }
  };

  const readFromDrone = async () => {
    if (dirty) {
      const ok = await confirm.ask({
        title: "Discard your changes?",
        message: "Reading from the drone replaces the mission in the editor.",
        confirmLabel: "Read from drone",
      });
      if (!ok) return;
    }
    await download();
  };

  const sel = selected != null ? items[selected] : null;

  return (
    <PageFrame header={<DroneTopBar />}>
      <Panel className={styles.sidebar}>
        <div className={styles.header}>
          <PanelTitle>Mission</PanelTitle>
          <span className={`${styles.badge} ${dirty ? styles.badgeDirty : ""}`}>
            {dirty ? "Not on drone" : "Synced"}
          </span>
        </div>

        <div className={styles.toolbar}>
          <Button size="sm" busy={busy === "download"} disabled={!online || busy !== null} onClick={readFromDrone}>
            Read from drone
          </Button>
          <Button size="sm" variant="primary" busy={busy === "upload"} disabled={!editable || !online || busy !== null || !items.length} onClick={upload}>
            Write to drone
          </Button>
          <Button size="sm" variant="danger" busy={busy === "start"} disabled={!editable || !online || busy !== null || !items.length} onClick={start}>
            Start
          </Button>
          <Button size="sm" variant="ghost" busy={busy === "clear"} disabled={!editable || !online || busy !== null} onClick={clearOnDrone}>
            Delete on drone
          </Button>
        </div>

        {editable && (
          <div className={styles.addRow}>
            <Select value={newType} onChange={(e) => setNewType(e.target.value as MissionItemType)} aria-label="Item type">
              {ADDABLE_TYPES.map((t) => (
                <option key={t} value={t}>
                  {ITEM_SPECS[t].label}
                </option>
              ))}
            </Select>
            <Button onClick={() => addItem(newType)}>Add</Button>
          </div>
        )}
        {editable && (
          <div className={styles.options}>
            <label className={styles.check}>
              <input type="checkbox" checked={addOnClick} onChange={(e) => setAddOnClick(e.target.checked)} />
              Map click adds a waypoint
            </label>
            <label className={styles.altDefault}>
              Default alt
              <NumberInput value={defaultAlt} onValue={setDefaultAlt} min={1} className={styles.smallInput} />m
            </label>
          </div>
        )}

        <ol className={styles.list}>
          {items.length === 0 && <li className={styles.empty}>{editable ? "Click the map or use Add to build a mission." : "No mission."}</li>}
          {items.map((item, index) => (
            <li
              key={index}
              className={`${styles.item} ${index === selected ? styles.itemSelected : ""} ${index === activeIndex ? styles.itemActive : ""}`}
              onClick={() => setSelected(index === selected ? null : index)}
            >
              <span className={styles.num}>{index + 1}</span>
              <span className={styles.desc}>{describe(item)}</span>
              {index === activeIndex && <span className={styles.flying}>flying</span>}
              {editable && (
                <span className={styles.itemActions} onClick={(e) => e.stopPropagation()}>
                  <button type="button" title="Move up" onClick={() => moveItem(index, -1)} disabled={index === 0}>↑</button>
                  <button type="button" title="Move down" onClick={() => moveItem(index, 1)} disabled={index === items.length - 1}>↓</button>
                  <button type="button" title="Delete" onClick={() => removeItem(index)}>✕</button>
                </span>
              )}
            </li>
          ))}
        </ol>

        {sel && selected != null && (
          <div className={styles.editor}>
            <div className={styles.editorTitle}>
              Item {selected + 1}
              {editable ? (
                <Select
                  value={sel.type}
                  onChange={(e) => updateItem(selected, { type: e.target.value as MissionItemType })}
                  className={styles.typeSelect}
                  aria-label="Change item type"
                >
                  {[...ADDABLE_TYPES, ...(sel.type === "raw" ? (["raw"] as const) : [])].map((t) => (
                    <option key={t} value={t}>
                      {ITEM_SPECS[t].label}
                    </option>
                  ))}
                </Select>
              ) : (
                <span> · {ITEM_SPECS[sel.type].label}</span>
              )}
            </div>
            <div className={styles.fields}>
              {ITEM_SPECS[sel.type].fields.map((key: FieldKey) => (
                <Field key={key} label={key === "alt_m" ? altLabel(sel) : FIELD_LABELS[key]}>
                  <NumberInput
                    value={sel[key]}
                    onValue={(v) => updateItem(selected, { [key]: v })}
                    step={key === "lat" || key === "lon" ? 0.000001 : 1}
                    disabled={!editable}
                  />
                </Field>
              ))}
              {ITEM_SPECS[sel.type].fields.length === 0 && <p className={styles.noFields}>No settings for this item.</p>}
            </div>
          </div>
        )}

        {(errors.length > 0 || warns.length > 0) && items.length > 0 && (
          <div className={styles.problems}>
            {errors.slice(0, 4).map((e) => (
              <p key={e} className={styles.error}>{e}</p>
            ))}
            {warns.map((w) => (
              <p key={w} className={styles.warning}>{w}</p>
            ))}
          </div>
        )}
      </Panel>

      <Panel className={styles.mapPanel}>
        <MissionMap
          items={items}
          selected={selected}
          activeIndex={activeIndex}
          home={home}
          drone={dronePos}
          addOnClick={addOnClick}
          editable={editable}
          onAdd={(lat, lon) => addItem("waypoint", lat, lon)}
          onMove={(index, lat, lon) => updateItem(index, { lat: round7(lat), lon: round7(lon) })}
          onSelect={setSelected}
        />
        {snapshot?.mission && (
          <div className={styles.progress}>
            {snapshot.mode} · item {Math.max(0, snapshot.mission.current_seq)} of {Math.max(0, snapshot.mission.total - 1)}
            {snapshot.mission.state ? ` · ${snapshot.mission.state.replace(/_/g, " ").toLowerCase()}` : ""}
          </div>
        )}
      </Panel>

      {confirm.element}
      <Toast message={message} />
    </PageFrame>
  );
}
