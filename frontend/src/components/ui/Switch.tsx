import styles from "./Switch.module.css";

interface SwitchProps {
  checked: boolean;
  onChange: (checked: boolean) => void;
  label: string;
  disabled?: boolean;
  busy?: boolean;
}

/** An on/off switch; label names what it turns on, for screen readers. */
export function Switch({ checked, onChange, label, disabled = false, busy = false }: SwitchProps) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      aria-busy={busy}
      className={`${styles.switch} ${checked ? styles.on : ""} ${busy ? styles.busy : ""}`}
      disabled={disabled || busy}
      onClick={() => onChange(!checked)}
    >
      <span className={styles.knob} aria-hidden />
    </button>
  );
}
