import styles from "./Toast.module.css";

export function Toast({ message }: { message: string }) {
  if (!message) return null;
  return (
    <div className={styles.toast} role="status">
      {message}
    </div>
  );
}
