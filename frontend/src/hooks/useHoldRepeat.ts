import { useRef } from "react";
import { usePressHold } from "./usePressHold";

const REPEAT_MS = 66;

/** Fires once on press, then repeats for as long as the button is held. */
export function useHoldRepeat(action: () => void) {
  const timer = useRef<ReturnType<typeof setInterval> | undefined>(undefined);

  return usePressHold(
    () => {
      action();
      timer.current = setInterval(action, REPEAT_MS);
    },
    () => clearInterval(timer.current),
  );
}
