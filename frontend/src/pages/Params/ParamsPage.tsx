import { useCallback, useEffect, useMemo, useState } from "react";
import { DroneTopBar } from "../../components/layout/DroneTopBar";
import { PageFrame } from "../../components/layout/PageFrame";
import { Button } from "../../components/ui/Button";
import { useConfirm } from "../../components/ui/ConfirmDialog";
import { TextInput } from "../../components/ui/Field";
import { Panel, PanelTitle } from "../../components/ui/Panel";
import { Toast } from "../../components/ui/Toast";
import { useAuth } from "../../context/AuthContext";
import { useDrone } from "../../context/DroneContext";
import { useToast } from "../../hooks/useToast";
import { api } from "../../services/api";
import { errorText } from "../../services/http";
import type { Param } from "../../types/gateway";
import styles from "./ParamsPage.module.css";

const MAX_ROWS = 300;
const TYPE_NAMES: Record<number, string> = { 1: "uint8", 2: "int8", 3: "uint16", 4: "int16", 5: "uint32", 6: "int32", 9: "float", 10: "double" };

const show = (v: number) => (Number.isInteger(v) ? String(v) : String(Number(v.toPrecision(7))));

export function ParamsPage() {
  const { canOperate } = useAuth();
  const { id, snapshot } = useDrone();
  const { message, notify } = useToast();
  const confirm = useConfirm();

  const [params, setParams] = useState<Param[]>([]);
  const [cached, setCached] = useState(true);
  const [loading, setLoading] = useState(false);
  const [query, setQuery] = useState("");
  const [edits, setEdits] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState<string | null>(null);

  const load = useCallback(
    async (refresh: boolean) => {
      setLoading(true);
      try {
        const res = await api.params(id, refresh);
        setParams(res.params);
        setCached(res.cached);
        if (refresh) notify(`Loaded ${res.params.length} parameters from the drone`);
      } catch (error) {
        notify(errorText(error));
      } finally {
        setLoading(false);
      }
    },
    [id, notify],
  );

  useEffect(() => {
    void load(false);
  }, [load]);

  const filtered = useMemo(() => {
    const q = query.trim().toUpperCase();
    return q ? params.filter((p) => p.name.includes(q)) : params;
  }, [params, query]);

  const save = async (p: Param) => {
    const raw = edits[p.name];
    const value = Number(raw);
    if (raw === undefined || raw.trim() === "" || Number.isNaN(value)) return notify("Enter a number");
    const ok = await confirm.ask({
      title: `Change ${p.name}?`,
      message: (
        <>
          <strong>{p.name}</strong>: {show(p.value)} → <strong>{show(value)}</strong>. Wrong parameter values can make the
          drone unsafe to fly.
        </>
      ),
      confirmLabel: "Write parameter",
      danger: true,
    });
    if (!ok) return;
    setSaving(p.name);
    try {
      const res = await api.setParam(id, p.name, value);
      setParams((list) => list.map((x) => (x.name === p.name ? { ...x, value: res.param.value } : x)));
      setEdits(({ [p.name]: _, ...rest }) => rest);
      notify(`${p.name} = ${show(res.param.value)}`);
    } catch (error) {
      notify(errorText(error));
    } finally {
      setSaving(null);
    }
  };

  const online = Boolean(snapshot?.online);

  return (
    <PageFrame header={<DroneTopBar />}>
      <Panel className={styles.panel}>
        <div className={styles.header}>
          <PanelTitle>Parameters</PanelTitle>
          <span className={styles.count}>
            {params.length ? `${params.length} parameters${cached ? " (cached)" : ""}` : "Not loaded"}
          </span>
          <TextInput
            className={styles.search}
            placeholder="Search, e.g. RTL or FS_"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
          <Button variant="primary" busy={loading} disabled={!online || loading} onClick={() => load(canOperate)}>
            Load from drone
          </Button>
        </div>
        {!canOperate && <p className={styles.notice}>View only: an operator login is needed to change parameters.</p>}
        {loading && params.length === 0 && <p className={styles.notice}>Downloading ~1,000 parameters, about 20 seconds…</p>}

        <div className={styles.tableWrap}>
          <table className={styles.table}>
            <thead>
              <tr>
                <th>Name</th>
                <th>Value</th>
                <th>Type</th>
                {canOperate && <th>New value</th>}
              </tr>
            </thead>
            <tbody>
              {filtered.slice(0, MAX_ROWS).map((p) => {
                const pending = edits[p.name];
                const changed = pending !== undefined && pending !== "" && Number(pending) !== p.value;
                return (
                  <tr key={p.name} className={changed ? styles.changed : ""}>
                    <td className={styles.name}>{p.name}</td>
                    <td className={styles.value}>{show(p.value)}</td>
                    <td className={styles.type}>{TYPE_NAMES[p.type] ?? p.type}</td>
                    {canOperate && (
                      <td className={styles.editCell}>
                        <TextInput
                          className={styles.editInput}
                          inputMode="decimal"
                          value={pending ?? ""}
                          placeholder={show(p.value)}
                          onChange={(e) => setEdits((all) => ({ ...all, [p.name]: e.target.value }))}
                          onKeyDown={(e) => e.key === "Enter" && changed && void save(p)}
                        />
                        <Button size="sm" busy={saving === p.name} disabled={!changed || !online || saving !== null} onClick={() => save(p)}>
                          Write
                        </Button>
                      </td>
                    )}
                  </tr>
                );
              })}
            </tbody>
          </table>
          {filtered.length > MAX_ROWS && (
            <p className={styles.more}>Showing {MAX_ROWS} of {filtered.length}. Refine the search to see more.</p>
          )}
          {params.length > 0 && filtered.length === 0 && <p className={styles.more}>No parameter matches “{query}”.</p>}
          {params.length === 0 && !loading && (
            <p className={styles.more}>No parameters cached yet. Use “Load from drone”.</p>
          )}
        </div>
      </Panel>
      {confirm.element}
      <Toast message={message} />
    </PageFrame>
  );
}
