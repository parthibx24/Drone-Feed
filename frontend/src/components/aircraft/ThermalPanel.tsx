import { useEffect, useState } from "react";
import type { AircraftControls } from "../../hooks/useAircraftFeatures";
import type { AircraftCommand, CameraSource, ThermalMeasure } from "../../types/gateway";
import { Button } from "../ui/Button";
import { Field, NumberInput, Select } from "../ui/Field";
import { SegmentedControl } from "../ui/SegmentedControl";
import { Switch } from "../ui/Switch";
import { formatTemp, paletteLabel } from "./thermal";
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

const MEASURES: { value: ThermalMeasure; label: string }[] = [
  { value: "off", label: "Off" },
  { value: "spot", label: "Spot" },
  { value: "area", label: "Area" },
];

const MEASURE_DONE: Record<ThermalMeasure, string> = {
  off: "Measurement off",
  spot: "Measuring a spot",
  area: "Measuring an area",
};

/**
 * The aircraft's thermal camera: palette, gain, isotherm and spot or area
 * temperature (read on the video, see ThermalOverlay).
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
    low == null || high == null ? "Enter both temperatures" : low >= high ? "The low end must be below the high end" : null;

  const status = !live
    ? offlineReason
    : !thermal
      ? "The aircraft reports no thermal camera"
      : !canOperate
        ? "View only: changing settings needs the operator role"
        : null;

  const measure = thermal?.measure ?? "off";
  const onThermal = cameraSource === "thermal";

  return (
    <div className={styles.panel}>
      <div className={`${styles.status} ${status && !(live && thermal) ? styles.warn : ""}`} role="status">
        <span className={`${styles.dot} ${live && thermal ? styles.dotOk : ""}`} aria-hidden />
        {status ?? "Thermal camera ready"}
      </div>

      {has("thermal_palette") && (
        <Field label="Palette" hint="How temperatures are coloured on the thermal video.">
          <Select
            value={thermal?.palette ?? ""}
            disabled={!can || pending === "thermal_palette"}
            onChange={(e) => {
              const palette = e.target.value;
              void run({ kind: "thermal_palette", palette }, `Palette set to ${paletteLabel(palette)}`);
            }}
          >
            {thermal && !thermal.palettes.includes(thermal.palette) && <option value={thermal.palette}>{paletteLabel(thermal.palette)}</option>}
            {(thermal?.palettes ?? []).map((p) => (
              <option key={p} value={p}>
                {paletteLabel(p)}
              </option>
            ))}
          </Select>
        </Field>
      )}

      {has("thermal_gain") && (
        <section className={styles.section} aria-label="Gain">
          <div className={styles.heading}>Gain</div>
          <div className={styles.segmented}>
            <SegmentedControl
              ariaLabel="Gain"
              options={GAINS}
              value={thermal?.gain ?? ""}
              disabled={!can || pending === "thermal_gain"}
              onChange={(gain) =>
                gain !== thermal?.gain &&
                void run({ kind: "thermal_gain", gain: gain as "high" | "low" }, `Gain set to ${gain === "high" ? "High" : "Low"}`)
              }
            />
          </div>
          <div className={styles.hint}>High shows finer differences, −20 to 150 °C. Low covers a wider range, 0 to 500 °C.</div>
        </section>
      )}

      {has("isotherm") && (
        <section className={styles.section} aria-label="Isotherm">
          <div className={styles.heading}>
            Isotherm
            <Switch
              label="Isotherm"
              checked={Boolean(iso?.enabled)}
              disabled={!can || rangeProblem !== null}
              busy={pending === "thermal_isotherm"}
              onChange={(enabled) =>
                void run({ kind: "thermal_isotherm", enabled, low_c: low!, high_c: high! }, enabled ? "Isotherm on" : "Isotherm off")
              }
            />
          </div>
          <div className={styles.row}>
            <Field label="From °C" className={styles.grow}>
              <NumberInput
                value={low}
                step={0.5}
                disabled={!can}
                onValue={(v) => {
                  setEditing(true);
                  setLow(v);
                }}
              />
            </Field>
            <Field label="To °C" className={styles.grow}>
              <NumberInput
                value={high}
                step={0.5}
                disabled={!can}
                onValue={(v) => {
                  setEditing(true);
                  setHigh(v);
                }}
              />
            </Field>
            <Button
              disabled={!can || !editing || rangeProblem !== null}
              busy={pending === "thermal_isotherm"}
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
          </div>
          <div className={editing && rangeProblem ? styles.problem : styles.hint}>
            {editing && rangeProblem ? rangeProblem : "Picks out temperatures in this range on the thermal video."}
          </div>
        </section>
      )}

      {has("thermal_measure") && (
        <section className={styles.section} aria-label="Measure">
          <div className={styles.heading}>Measure</div>
          <div className={styles.segmented}>
            <SegmentedControl
              ariaLabel="Measure"
              options={MEASURES}
              value={measure}
              disabled={!can || pending === "set_metering"}
              onChange={(mode) => mode !== measure && void run({ kind: "set_metering", mode: mode as ThermalMeasure }, MEASURE_DONE[mode as ThermalMeasure])}
            />
          </div>
          {measure !== "off" && !onThermal && cameraSource !== null ? (
            <div className={styles.row}>
              <div className={`${styles.hint} ${styles.grow}`}>Switch the lens to Thermal to measure on the video.</div>
              {onThermalLens && (
                <Button size="sm" disabled={!canOperate} onClick={onThermalLens}>
                  Use thermal lens
                </Button>
              )}
            </div>
          ) : (
            <div className={styles.hint}>
              {measure === "spot"
                ? canOperate
                  ? "Click the thermal video to read the temperature at that point."
                  : "The aircraft reads the temperature at the marked point."
                : measure === "area"
                  ? canOperate
                    ? "Drag a box on the thermal video to read its average, lowest and highest temperature."
                    : "The aircraft reads the temperatures inside the marked box."
                  : "Read the temperature of a point or an area on the thermal video."}
            </div>
          )}

          {measure === "spot" && (
            <div className={styles.reading}>
              <span className={styles.readingValue}>{formatTemp(thermal?.spot?.temp_c ?? null)}</span>
              <Button
                size="sm"
                disabled={!can || pending === "meter_spot"}
                onClick={() => void run({ kind: "meter_spot", x: 0.5, y: 0.5 }, "Measuring the centre")}
              >
                Read the centre
              </Button>
            </div>
          )}
          {measure === "area" && (
            <div className={styles.reading}>
              {thermal?.area ? (
                <>
                  <span className={styles.readingValue}>{formatTemp(thermal.area.avg_c)}</span>
                  <span className={styles.range}>
                    <span>Min {formatTemp(thermal.area.min_c)}</span>
                    <span>Max {formatTemp(thermal.area.max_c)}</span>
                  </span>
                </>
              ) : (
                <span className={styles.readingNote}>No area yet</span>
              )}
            </div>
          )}
        </section>
      )}
    </div>
  );
}
