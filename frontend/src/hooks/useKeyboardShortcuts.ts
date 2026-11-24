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
  /** False for viewers: shortcuts do nothing. */
  enabled?: boolean;
  /** False when the drone has no gimbal (or no zoom): those keys do nothing. */
  gimbalEnabled?: boolean;
  zoomEnabled?: boolean;
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
      const { gimbal, toggleRecording, enabled = true, gimbalEnabled = true, zoomEnabled = true } = latest.current;
      if (!enabled) return;
      const key = event.key.toLowerCase();
      if (key === "r") return toggleRecording();
      if ((key === "1" || key === "z") && zoomEnabled) return gimbal.zoomReset();
      if (!gimbalEnabled) return;

      if (event.code === "Space") {
        event.preventDefault();
        gimbal.stop();
        return;
      }
      if (key === "c") return gimbal.center();

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

    // Switching window or tab swallows the keyup: stop instead of moving on.
    const onFocusLost = () => {
      if (activeKey === null) return;
      activeKey = null;
      latest.current.gimbal.stop();
    };
    const onVisibility = () => {
      if (document.visibilityState === "hidden") onFocusLost();
    };

    window.addEventListener("keydown", onKeyDown);
    window.addEventListener("keyup", onKeyUp);
    window.addEventListener("blur", onFocusLost);
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      onFocusLost();
      window.removeEventListener("keydown", onKeyDown);
      window.removeEventListener("keyup", onKeyUp);
      window.removeEventListener("blur", onFocusLost);
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, []);
}
