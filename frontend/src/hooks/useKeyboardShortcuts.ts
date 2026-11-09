import { useEffect, useRef } from "react";
import type { GimbalControls } from "./useGimbal";

const ARROW_DIRECTIONS: Record<string, [yaw: number, pitch: number]> = {
  ArrowUp: [0, 1],
  ArrowDown: [0, -1],
  ArrowLeft: [-1, 0],
  ArrowRight: [1, 0],
};

interface ShortcutActions {
  gimbal: GimbalControls;
  toggleRecording: () => void;
}

/** Arrows jog the gimbal, Space stops, C centres, 1/Z resets zoom, R toggles recording. */
export function useKeyboardShortcuts(actions: ShortcutActions): void {
  const latest = useRef(actions);

  useEffect(() => {
    latest.current = actions;
  });

  useEffect(() => {
    let activeKey: string | null = null;

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.repeat) return;
      if (document.activeElement?.matches("input, textarea, select")) return;
      const { gimbal, toggleRecording } = latest.current;

      if (event.code === "Space") {
        event.preventDefault();
        gimbal.stop();
        return;
      }
      const key = event.key.toLowerCase();
      if (key === "c") return gimbal.center();
      if (key === "1" || key === "z") return gimbal.zoomReset();
      if (key === "r") return toggleRecording();

      const direction = ARROW_DIRECTIONS[event.key];
      if (direction && activeKey !== event.key) {
        event.preventDefault();
        activeKey = event.key;
        gimbal.move(direction[0], direction[1]);
      }
    };

    const onKeyUp = (event: KeyboardEvent) => {
      if (ARROW_DIRECTIONS[event.key] && activeKey === event.key) {
        activeKey = null;
        latest.current.gimbal.stop();
      }
    };

    window.addEventListener("keydown", onKeyDown);
    window.addEventListener("keyup", onKeyUp);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
      window.removeEventListener("keyup", onKeyUp);
    };
  }, []);
}
