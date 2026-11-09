import { useCallback, useRef, useState } from "react";

const TOAST_MS = 2800;

export type Notify = (message: string) => void;

export function useToast() {
  const [message, setMessage] = useState("");
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  const notify = useCallback<Notify>((next) => {
    setMessage(next);
    clearTimeout(timer.current);
    timer.current = setTimeout(() => setMessage(""), TOAST_MS);
  }, []);

  return { message, notify };
}
