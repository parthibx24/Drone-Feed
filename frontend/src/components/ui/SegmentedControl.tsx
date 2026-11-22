import { ControlGroup } from "./ControlGroup";
import styles from "./SegmentedControl.module.css";

interface SegmentedControlProps {
  options: { value: string; label: string }[];
  value: string;
  onChange: (value: string) => void;
  ariaLabel: string;
  disabled?: boolean;
  /** "md" floats over the video; "sm" is the same control sized for the sidebar, filling its row. */
  size?: "md" | "sm";
}

export function SegmentedControl({ options, value, onChange, ariaLabel, disabled = false, size = "md" }: SegmentedControlProps) {
  return (
    <ControlGroup strong className={`${styles.group} ${size === "sm" ? styles.sm : ""}`} role="tablist" aria-label={ariaLabel}>
      {options.map((option) => (
        <button
          key={option.value}
          type="button"
          role="tab"
          aria-selected={option.value === value}
          className={`${styles.option} ${option.value === value ? styles.selected : ""}`}
          disabled={disabled}
          onClick={() => onChange(option.value)}
        >
          {option.label}
        </button>
      ))}
    </ControlGroup>
  );
}
