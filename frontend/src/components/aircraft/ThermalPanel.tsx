import { useEffect, useState } from "react";
import type { AircraftControls } from "../../hooks/useAircraftFeatures";
import type { AircraftCommand, CameraSource, ThermalMeasure } from "../../types/gateway";
import { Button } from "../ui/Button";
import { NumberInput, Select } from "../ui/Field";
import { SegmentedControl } from "../ui/SegmentedControl";
import { Switch } from "../ui/Switch";
import { formatTemp, paletteGradient, paletteLabel } from "./thermal";
import styles from "./AircraftPanels.module.css";

interface ThermalPanelProps {
  aircraft: AircraftControls;
  canOperate: boolean;
  /** The lens on the video (telemetry.camera_source), if the camera reports one. */
  cameraSource: CameraSource | null;
  /** Switches the video to the thermal lens. */
  onThermalLens?: () => void;
  notify: (message: string) => void;
}

const GAINS = [
  { value: "high", label: "High" },
  { value: "low", label: "Low" },
];

const GAIN_HINT: Record<string, string> = {
  high: "−20 to 150 °C, finer detail",
  low: "0 to 500 °C, wider range",
};

const MEASURES: { value: ThermalMeasure; label: string }[] = [
  { value: "off", label: "Off" },
  { value: "spot", label: "Spot" },
  { value: "area", label: "Area" },
];

const MEASURE_DONE: Record<ThermalMeasure, string> = {
  off: "Measuring off",
  spot: "Measuring a spot",
  area: "Measuring an area",
};

/**
 * The aircraft's thermal camera: palette, gain, isotherm and spot or area
 * temperature (read on the video, see ThermalOverlay). Measuring, the live
 * task, comes first; the picture's settings follow, one per row.
 */
export function ThermalPanel({ aircraft, canOperate, cameraSource, onThermalLens, notify }: ThermalPanelProps) {
  const { state, live, offlineReason, has, pending, send } = aircraft;
  const thermal = state?.thermal ?? null;
  const can = canOperate && live && thermal !== null;

  const run = async (command: AircraftCommand, done: string) => {
    const reason = await send(command);
    notify(reason ?? done);
  };

  // The isotherm range is edited locally and sent with Apply; it follows the
  // aircraft until the operator changes a value.
  const iso = thermal?.isotherm ?? null;
  const [low, setLow] = useState<number | undefined>(undefined);
  const [high, setHigh] = useState<number | undefined>(undefined);
  const [editing, setEditing] = useState(false);
  const isoLow = iso?.low_c;
  const isoHigh = iso?.high_c;
  useEffect(() => {
    if (editing || isoLow == null || isoHigh == null) return;
    setLow(isoLow);
    setHigh(isoHigh);
  }, [editing, isoLow, isoHigh]);
  const rangeProblem =
    low == null || high == null ? "Enter both temperatures" : low >= high ? "The first temperature must be the lower one" : null;

  // Only what needs attention is said: offline, no camera, or view only.
  const notice = !live
    ? offlineReason
    : !thermal
      ? "The aircraft reports no thermal camera"
      : !canOperate
        ? "View only: an operator login is needed to change settings"
        : null;
  const fault = !live || !thermal;

  const measure = thermal?.measure ?? "off";
  const wrongLens = measure !== "off" && cameraSource !== null && cameraSource !== "thermal";
  const palette = thermal?.palette ?? "";

  return (
    <div className={styles.panel}>
      {notice && (
        <div className={`${styles.notice} ${fault ? styles.warn : ""}`} role="status">
          {notice}
        </div>
      )}

      {has("thermal_measure") && (
        <>
          <div className={styles.setting}>
            <span className={styles.label}>Measure</span>
            <SegmentedControl
              size="sm"
              ariaLabel="Measure"
              options={MEASURES}
              value={measure}
              disabled={!can || pending === "set_metering"}
              onChange={(mode) => mode !== measure && void run({ kind: "set_metering", mode: mode as ThermalMeasure }, MEASURE_DONE[mode as ThermalMeasure])}
            />
            {!canOperate ? null : measure === "off" ? (
              <span className={styles.sub}>Read a point or an area on the thermal video</span>
            ) : wrongLens ? (
              <span className={styles.sub}>Switch the video to Thermal to measure</span>
            ) : (
              <span className={styles.sub}>
                {measure === "spot" ? "Click the thermal video to read a point" : "Drag on the thermal video to read an area"}
              </span>
            )}
          </div>

          {measure === "spot" && (
            <div className={styles.reading}>
              <Reading label="Spot" value={thermal?.spot?.temp_c ?? null} />
              {wrongLens && onThermalLens ? (
                <Button size="sm" className={styles.push} disabled={!canOperate} onClick={onThermalLens}>
                  Show thermal
                </Button>
              ) : (
                <Button
                  size="sm"
                  className={styles.push}
                  disabled={!can || pending === "meter_spot"}
                  onClick={() => void run({ kind: "meter_spot", x: 0.5, y: 0.5 }, "Measuring the centre")}
                >
                  Read centre
                </Button>
              )}
            </div>
          )}
          {measure === "area" && (
            <div className={styles.reading}>
              <Reading label="Avg" value={thermal?.area?.avg_c ?? null} />
              <Reading label="Min" value={thermal?.area?.min_c ?? null} />
              <Reading label="Max" value={thermal?.area?.max_c ?? null} />
              {wrongLens && onThermalLens && (
                <Button size="sm" className={styles.push} disabled={!canOperate} onClick={onThermalLens}>
                  Show thermal
                </Button>
              )}
            </div>
          )}
        </>
      )}

      {has("thermal_palette") && (
        <div className={styles.setting}>
          <label className={styles.label} htmlFor="thermal-palette">
            Palette
          </label>
          <div className={styles.paletteBox}>
            <span className={styles.swatch} style={{ backgroundImage: palette ? paletteGradient(palette) : undefined }} aria-hidden />
            <Select
              id="thermal-palette"
              className={styles.select}
              value={palette}
              disabled={!can || pending === "thermal_palette"}
              onChange={(e) => {
                const next = e.target.value;
                void run({ kind: "thermal_palette", palette: next }, `Palette set to ${paletteLabel(next)}`);
              }}
            >
              {thermal && !thermal.palettes.includes(thermal.palette) && <option value={thermal.palette}>{paletteLabel(thermal.palette)}</option>}
              {(thermal?.palettes ?? []).map((p) => (
                <option key={p} value={p}>
                  {paletteLabel(p)}
                </option>
              ))}
            </Select>
          </div>
        </div>
      )}

      {has("thermal_gain") && (
        <div className={styles.setting}>
          <span className={styles.label}>Gain</span>
          <SegmentedControl
            size="sm"
            ariaLabel="Gain"
            options={GAINS}
            value={thermal?.gain ?? ""}
            disabled={!can || pending === "thermal_gain"}
            onChange={(gain) =>
              gain !== thermal?.gain &&
              void run({ kind: "thermal_gain", gain: gain as "high" | "low" }, `Gain set to ${gain === "high" ? "High" : "Low"}`)
            }
          />
          <span className={styles.sub}>{GAIN_HINT[thermal?.gain ?? ""] ?? "High: −20 to 150 °C. Low: 0 to 500 °C."}</span>
        </div>
      )}

      {has("isotherm") && (
        <div className={styles.setting}>
          <span className={styles.label} id="isotherm-label">
            Isotherm
          </span>
          <div className={styles.control} role="group" aria-labelledby="isotherm-label">
            <NumberInput
              className={styles.num}
              aria-label="Isotherm from, °C"
              value={low}
              step={0.5}
              disabled={!can}
              onValue={(v) => {
                setEditing(true);
                setLow(v);
              }}
            />
            <span className={styles.unit} aria-hidden>
              –
            </span>
            <NumberInput
              className={styles.num}
              aria-label="Isotherm to, °C"
              value={high}
              step={0.5}
              disabled={!can}
              onValue={(v) => {
                setEditing(true);
                setHigh(v);
              }}
            />
            <span className={styles.unit}>°C</span>
            <Button
              size="sm"
              className={styles.push}
              disabled={!can || !editing || rangeProblem !== null}
              busy={pending === "thermal_isotherm" && editing}
              onClick={async () => {
                await run(
                  { kind: "thermal_isotherm", enabled: true, low_c: low!, high_c: high! },
                  `Isotherm set to ${formatTemp(low!)} – ${formatTemp(high!)}`,
                );
                setEditing(false);
              }}
            >
              Apply
            </Button>
            <Switch
              label="Isotherm"
              checked={Boolean(iso?.enabled)}
              disabled={!can || rangeProblem !== null}
              busy={pending === "thermal_isotherm" && !editing}
              onChange={(enabled) =>
                void run({ kind: "thermal_isotherm", enabled, low_c: low!, high_c: high! }, enabled ? "Isotherm on" : "Isotherm off")
              }
            />
          </div>
          {editing && rangeProblem && <span className={`${styles.sub} ${styles.problem}`}>{rangeProblem}</span>}
        </div>
      )}

    </div>
  );
}

function Reading({ label, value }: { label: string; value: number | null }) {
  return (
    <div className={styles.stat}>
      <span className={styles.statLabel}>{label}</span>
      <span className={`${styles.statValue} ${value == null ? styles.empty : ""}`}>{formatTemp(value)}</span>
    </div>
  );
}
