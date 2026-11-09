import { useState } from "react";
import { useRecordingsList } from "../../hooks/useRecordingsList";
import type { Recording, RecordingSort, RecordingStream } from "../../types/recording";
import { EMPTY_VALUE, formatRecordingDate, formatRecordingTime } from "../../utils/format";
import { Modal, ModalButton, ModalMessage } from "../ui/Modal";
import { RecordingActions, type RecordingActionHandlers } from "./RecordingActions";
import styles from "./RecordingsLibraryModal.module.css";

const SORT_OPTIONS: { label: string; value: RecordingSort }[] = [
  { label: "Newest First", value: { sortBy: "time", order: "desc" } },
  { label: "Oldest First", value: { sortBy: "time", order: "asc" } },
  { label: "Longest Duration", value: { sortBy: "duration", order: "desc" } },
  { label: "Shortest Duration", value: { sortBy: "duration", order: "asc" } },
  { label: "Largest Size", value: { sortBy: "size", order: "desc" } },
];

function streamDescription(stream: RecordingStream): string {
  const resolution =
    stream.resolution ?? (stream.width && stream.height ? `${stream.width}x${stream.height}` : null);
  return [stream.codec_name?.toUpperCase(), resolution, stream.fps ? `${stream.fps}fps` : null]
    .filter(Boolean)
    .join(" · ");
}

function LibraryItem({ item, ...handlers }: RecordingActionHandlers & { item: Recording }) {
  return (
    <div className={`${styles.item} ${item.is_active ? styles.activeItem : ""}`}>
      <div className={styles.info}>
        <div className={styles.titleRow}>
          <span className={styles.filename}>{item.filename}</span>
          {item.is_active && <span className={styles.live}>● RECORDING NOW</span>}
          <span className={styles.badge}>{item.drone_name || "drone"}</span>
        </div>
        <div className={styles.meta}>
          <span>{formatRecordingDate(item.created_at)}</span>
          <span>{formatRecordingTime(item.created_at)}</span>
          <span>{item.duration_formatted || EMPTY_VALUE}</span>
          <span>{item.file_size_human || EMPTY_VALUE}</span>
        </div>
        <div className={styles.streams}>
          {item.streams.map((stream) => {
            const thermal = stream.role === "thermal" || stream.index === 1;
            return (
              <span key={stream.index} className={`${styles.stream} ${thermal ? styles.thermal : ""}`}>
                {thermal ? "Thermal" : "Visible"}: {streamDescription(stream)}
              </span>
            );
          })}
        </div>
      </div>
      <RecordingActions item={item} {...handlers} />
    </div>
  );
}

interface RecordingsLibraryModalProps extends RecordingActionHandlers {
  listVersion: number;
  onClose: () => void;
}

export function RecordingsLibraryModal({
  listVersion,
  onClose,
  onPreview,
  onDelete,
}: RecordingsLibraryModalProps) {
  const [sortIndex, setSortIndex] = useState(0);
  const { items, error, loading, reload } = useRecordingsList(SORT_OPTIONS[sortIndex].value, listVersion);

  return (
    <Modal
      title="Recordings Library"
      badge={
        <span className={styles.count}>
          {items.length} {items.length === 1 ? "clip" : "clips"}
        </span>
      }
      actions={
        <>
          <label className={styles.sort}>
            Sort:
            <select
              className={styles.select}
              value={sortIndex}
              onChange={(event) => setSortIndex(Number(event.target.value))}
            >
              {SORT_OPTIONS.map((option, index) => (
                <option key={option.label} value={index}>
                  {option.label}
                </option>
              ))}
            </select>
          </label>
          <ModalButton onClick={reload}>↻ Refresh</ModalButton>
        </>
      }
      onClose={onClose}
    >
      {error && <ModalMessage>Failed to load recordings: {error}</ModalMessage>}
      {!error && items.length === 0 && (
        <ModalMessage>
          {loading ? "Loading recordings…" : "No recordings found. Click Start Record to capture the camera streams."}
        </ModalMessage>
      )}
      <div className={styles.list}>
        {items.map((item) => (
          <LibraryItem key={item.relative_path} item={item} onPreview={onPreview} onDelete={onDelete} />
        ))}
      </div>
    </Modal>
  );
}
