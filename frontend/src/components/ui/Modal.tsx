import type { ReactNode } from "react";
import styles from "./Modal.module.css";

interface ModalProps {
  title: string;
  badge?: ReactNode;
  actions?: ReactNode;
  wide?: boolean;
  narrow?: boolean;
  onClose: () => void;
  children: ReactNode;
}

export function Modal({ title, badge, actions, wide = false, narrow = false, onClose, children }: ModalProps) {
  return (
    <div className={styles.root} role="dialog" aria-modal="true" aria-label={title}>
      <div className={styles.backdrop} onClick={onClose} />
      <div className={`${styles.card} ${wide ? styles.wide : ""} ${narrow ? styles.narrow : ""}`}>
        <header className={styles.header}>
          <div className={styles.heading}>
            <h2 className={styles.title}>{title}</h2>
            {badge}
          </div>
          <div className={styles.actions}>
            {actions}
            <button type="button" className={styles.close} aria-label="Close" onClick={onClose}>
              ✕
            </button>
          </div>
        </header>
        <div className={styles.body}>{children}</div>
      </div>
    </div>
  );
}

export function ModalButton(props: React.ButtonHTMLAttributes<HTMLButtonElement>) {
  return <button type="button" className={styles.button} {...props} />;
}

export function ModalMessage({ children }: { children: ReactNode }) {
  return <div className={styles.message}>{children}</div>;
}
