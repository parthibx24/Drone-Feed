import { useCallback, useRef, useState, type ReactNode } from "react";
import { Button } from "./Button";
import { Modal } from "./Modal";
import styles from "./ConfirmDialog.module.css";

export interface ConfirmOptions {
  title: string;
  message: ReactNode;
  confirmLabel?: string;
  danger?: boolean;
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

  const close = (ok: boolean) => {
    resolver.current?.(ok);
    resolver.current = null;
    setOptions(null);
  };

  const element = options ? (
    <Modal title={options.title} onClose={() => close(false)} narrow>
      <div className={styles.message}>{options.message}</div>
      <div className={styles.actions}>
        <Button onClick={() => close(false)}>Cancel</Button>
        <Button variant={options.danger ? "danger" : "primary"} onClick={() => close(true)} autoFocus>
          {options.confirmLabel ?? "Confirm"}
        </Button>
      </div>
    </Modal>
  ) : null;

  return { ask, element };
}
