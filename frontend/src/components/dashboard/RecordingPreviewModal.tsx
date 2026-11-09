import { useEffect, useRef, useState } from "react";
import { MultiTrackPlayer, type TransportState } from "../../services/MultiTrackPlayer";
import type { Recording } from "../../types/recording";
import { formatClock } from "../../utils/format";
import { Modal, ModalButton, ModalMessage } from "../ui/Modal";
import styles from "./RecordingPreviewModal.module.css";

function trackLabels(item: Recording): string[] {
  const count = Math.max(1, item.stream_count || item.streams.length);
  return Array.from({ length: count }, (_, index) => item.streams[index]?.role ?? `Track ${index + 1}`);
}

interface RecordingPreviewModalProps {
  item: Recording;
  onClose: () => void;
}

export function RecordingPreviewModal({ item, onClose }: RecordingPreviewModalProps) {
  const gridRef = useRef<HTMLDivElement>(null);
  const playerRef = useRef<MultiTrackPlayer | null>(null);
  const [transport, setTransport] = useState<TransportState>({ playing: false, time: 0, duration: 0 });
  const [seekPreview, setSeekPreview] = useState<number | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const player = new MultiTrackPlayer(
      gridRef.current!,
      trackLabels(item),
      { cell: styles.cell, label: styles.cellLabel },
      setTransport,
    );
    playerRef.current = player;
    player
      .load(item.download_url)
      .catch((reason: Error) => {
        console.warn("MKV preview failed:", reason);
        setError(reason.message);
      })
      .finally(() => setLoading(false));
    return () => {
      void player.dispose();
      playerRef.current = null;
    };
  }, [item]);

  const commitSeek = () => {
    if (seekPreview == null) return;
    void playerRef.current?.seekTo(seekPreview);
    setSeekPreview(null);
  };

  const shownTime = seekPreview ?? transport.time;

  return (
    <Modal
      wide
      title={item.filename}
      actions={
        <>
          <span className={styles.time}>
            {formatClock(shownTime)} / {formatClock(transport.duration)}
          </span>
          <input
            className={styles.seek}
            type="range"
            min={0}
            max={Math.max(0.1, transport.duration)}
            step={0.1}
            value={shownTime}
            onChange={(event) => setSeekPreview(Number(event.target.value))}
            onPointerUp={commitSeek}
            onKeyUp={commitSeek}
          />
          <ModalButton onClick={() => playerRef.current?.toggle()}>
            {transport.playing ? "⏸ Pause" : "▶ Play all"}
          </ModalButton>
        </>
      }
      onClose={onClose}
    >
      {loading && <ModalMessage>Loading preview…</ModalMessage>}
      {error && <ModalMessage>Preview failed: {error}. Download the MKV instead.</ModalMessage>}
      <div ref={gridRef} className={styles.grid} />
    </Modal>
  );
}
