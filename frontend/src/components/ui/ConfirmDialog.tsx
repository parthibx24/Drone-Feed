import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { Button } from "./Button";
import { Modal } from "./Modal";
import styles from "./ConfirmDialog.module.css";

export interface ConfirmOptions {
  title: string;
  message: ReactNode;
  confirmLabel?: string;
  danger?: boolean;
  /** Focus Cancel instead of the action, so Enter does not confirm (for settings that remove a safeguard). */
  focusCancel?: boolean;
}

/**
 * Promise-based confirmation dialog for safety-relevant actions:
 *   const confirm = useConfirm();
 *   if (await confirm.ask({ title: "Arm motors?", ... })) { ... }
 *   ... render {confirm.element}
 */
export function useConfirm() {
  const [options, setOptions] = useState<ConfirmOptions | null>(null);
  const resolver = useRef<((ok: boolean) => void) | null>(null);

  const ask = useCallback((next: ConfirmOptions) => {
    resolver.current?.(false);
    setOptions(next);
    return new Promise<boolean>((resolve) => {
      resolver.current = resolve;
    });
  }, []);

  const close = useCallback((ok: boolean) => {
    resolver.current?.(ok);
    resolver.current = null;
    setOptions(null);
  }, []);

  // Escape cancels, as the close button and the backdrop do.
  const open = options !== null;
  useEffect(() => {
    if (!open) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") close(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, close]);

  const element = options ? (
    <Modal title={options.title} onClose={() => close(false)} narrow>
      <div className={styles.message}>{options.message}</div>
      <div className={styles.actions}>
        <Button onClick={() => close(false)} autoFocus={options.focusCancel}>
          Cancel
        </Button>
        <Button variant={options.danger ? "danger" : "primary"} onClick={() => close(true)} autoFocus={!options.focusCancel}>
          {options.confirmLabel ?? "Confirm"}
        </Button>
      </div>
    </Modal>
  ) : null;

  return { ask, element };
}
