import { useState, type MutableRefObject } from "react";
import type { Limits } from "../../types/gateway";
import { heightPresets, speedPresets } from "../../utils/presets";
import { Field, NumberInput } from "../ui/Field";
import { Presets } from "../ui/Presets";
import styles from "./GotoForm.module.css";

export interface GotoChoice {
  altM: number | undefined;
  speedMS: number | undefined;
}

interface GotoFormProps {
  /** The drone's height now, offered first so "stay at this height" is one tap. */
  currentAltM: number;
  limits: Limits | null;
  /** Filled as the operator picks; read when the dialog is confirmed. */
  choice: MutableRefObject<GotoChoice>;
  /** Height above the take-off point (DJI) or above home (ArduPilot). */
  heightLabel: string;
}

/** Height and speed for a go-to, inside its confirmation dialog. */
export function GotoForm({ currentAltM, limits, choice, heightLabel }: GotoFormProps) {
  const [alt, setAlt] = useState<number | undefined>(choice.current.altM);
  const [speed, setSpeed] = useState<number | undefined>(choice.current.speedMS);
  const pickAlt = (v: number | undefined) => {
    setAlt(v);
    choice.current = { ...choice.current, altM: v };
  };
  const pickSpeed = (v: number | undefined) => {
    setSpeed(v);
    choice.current = { ...choice.current, speedMS: v };
  };
  const heights = [currentAltM, ...heightPresets(limits).filter((h) => h !== currentAltM)];

  return (
    <div className={styles.form}>
      <Field label={heightLabel}>
        <NumberInput value={alt} onValue={pickAlt} min={1} max={limits?.max_alt_m} step={1} aria-label={heightLabel} />
        <Presets
          values={heights}
          value={alt}
          onPick={pickAlt}
          label={(v) => (v === currentAltM ? `Current ${v} m` : `${v} m`)}
          ariaLabel={heightLabel}
        />
      </Field>
      <Field label="Speed (m/s)">
        <NumberInput value={speed} onValue={pickSpeed} min={1} max={limits?.max_speed_m_s} step={1} aria-label="Speed (m/s)" />
        <Presets values={speedPresets(limits)} value={speed} onPick={pickSpeed} unit="m/s" ariaLabel="Speed" />
      </Field>
    </div>
  );
}
