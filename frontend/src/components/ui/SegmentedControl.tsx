import { ControlGroup } from "./ControlGroup";
import styles from "./SegmentedControl.module.css";

interface SegmentedControlProps {
  options: { value: string; label: string }[];
  value: string;
  onChange: (value: string) => void;
  ariaLabel: string;
}

export function SegmentedControl({ options, value, onChange, ariaLabel }: SegmentedControlProps) {
  return (
    <ControlGroup strong className={styles.group} role="tablist" aria-label={ariaLabel}>
      {options.map((option) => (
        <button
          key={option.value}
          type="button"
          role="tab"
          aria-selected={option.value === value}
          className={`${styles.option} ${option.value === value ? styles.selected : ""}`}
          onClick={() => onChange(option.value)}
        >
          {option.label}
        </button>
      ))}
    </ControlGroup>
  );
}
