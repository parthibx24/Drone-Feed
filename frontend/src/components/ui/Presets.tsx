import styles from "./Presets.module.css";

interface PresetsProps {
  /** Values offered, in order; the one equal to `value` shows as selected. */
  values: number[];
  value: number | undefined;
  onPick: (value: number) => void;
  /** Unit after each value ("m", "s", "m/s"); omitted for plain counts. */
  unit?: string;
  /** Text for a value instead of "<value> <unit>" (e.g. 0 → "Until stopped"). */
  label?: (value: number) => string;
  disabled?: boolean;
  ariaLabel: string;
}

/**
 * One-tap values next to a number box, so the usual heights, speeds and
 * times don't have to be typed. The box stays for anything else.
 */
export function Presets({ values, value, onPick, unit, label, disabled = false, ariaLabel }: PresetsProps) {
  if (values.length === 0) return null;
  return (
    <div className={styles.row} role="group" aria-label={ariaLabel}>
      {values.map((v) => (
        <button
          key={v}
          type="button"
          className={`${styles.chip} ${v === value ? styles.selected : ""}`}
          aria-pressed={v === value}
          disabled={disabled}
          onClick={() => onPick(v)}
        >
          {label ? label(v) : unit ? `${v} ${unit}` : String(v)}
        </button>
      ))}
    </div>
  );
}
