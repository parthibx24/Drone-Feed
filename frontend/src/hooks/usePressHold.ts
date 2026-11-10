import { useEffect, useRef, useState, type PointerEvent } from "react";

/** Tracks a press-and-hold on a button, keeping it through pointer capture.
 *  Losing the pointer, the window or the tab counts as a release, so a held
 *  control can never keep acting after the operator has left it. */
export function usePressHold(onPress: () => void, onRelease: () => void) {
  const [active, setActive] = useState(false);
  const pressed = useRef(false);
  const releaseRef = useRef(onRelease);
  releaseRef.current = onRelease;

  const release = () => {
    if (!pressed.current) return;
    pressed.current = false;
    setActive(false);
    releaseRef.current();
  };
  const releaseRefFn = useRef(release);
  releaseRefFn.current = release;

  useEffect(() => {
    if (!active) return;
    const onBlur = () => releaseRefFn.current();
    const onVisibility = () => {
      if (document.visibilityState === "hidden") releaseRefFn.current();
    };
    window.addEventListener("blur", onBlur);
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      window.removeEventListener("blur", onBlur);
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, [active]);

  // Unmounting while held (e.g. navigating away) also releases.
  useEffect(() => () => releaseRefFn.current(), []);

  const onPointerDown = (event: PointerEvent<HTMLElement>) => {
    if (pressed.current) return;
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    pressed.current = true;
    setActive(true);
    onPress();
  };

  return {
    active,
    pressProps: {
      onPointerDown,
      onPointerUp: release,
      onPointerCancel: release,
      onLostPointerCapture: release,
    },
  };
}
