import { useRef, useState, type PointerEvent } from "react";

/** Tracks a press-and-hold on a button, keeping it through pointer capture. */
export function usePressHold(onPress: () => void, onRelease: () => void) {
  const [active, setActive] = useState(false);
  const pressed = useRef(false);

  const release = () => {
    if (!pressed.current) return;
    pressed.current = false;
    setActive(false);
    onRelease();
  };

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
    pressProps: { onPointerDown, onPointerUp: release, onPointerCancel: release },
  };
}
