import { useEffect, useRef, useState, type ReactNode, type Ref } from "react";
import { VideoPlayer } from "../../services/VideoPlayer";
import type { CameraFeed } from "../../types/drone";
import styles from "./VideoFeed.module.css";

interface VideoFeedProps {
  feed: CameraFeed;
  go2rtcUrl: string;
  isMain: boolean;
  rootRef?: Ref<HTMLDivElement>;
  onPlayingChange: (stream: string, playing: boolean) => void;
  onSelect: (stream: string) => void;
  /** The picture's own size in pixels, while it plays. */
  onVideoSize?: (stream: string, width: number, height: number) => void;
  /** Drawn over the main feed, inside it (so it stays in full screen). */
  overlay?: ReactNode;
}

/**
 * One camera feed. The main and inset positions are purely CSS, so swapping
 * feeds never moves the iframe in the DOM (which would restart the stream).
 */
export function VideoFeed({ feed, go2rtcUrl, isMain, rootRef, onPlayingChange, onSelect, onVideoSize, overlay }: VideoFeedProps) {
  const frameRef = useRef<HTMLDivElement>(null);
  const [status, setStatus] = useState<string | null>(null);
  const { stream } = feed;
  // Read through a ref: a new callback must not restart the stream.
  const onVideoSizeRef = useRef(onVideoSize);
  useEffect(() => {
    onVideoSizeRef.current = onVideoSize;
  }, [onVideoSize]);

  useEffect(() => {
    const player = new VideoPlayer(frameRef.current!, {
      onStatus: setStatus,
      onPlayingChange: (playing) => onPlayingChange(stream, playing),
      onVideoSize: (width, height) => onVideoSizeRef.current?.(stream, width, height),
    });
    player.connect(go2rtcUrl, stream).catch((error) => console.warn(`Video failed for ${stream}:`, error));
    return () => player.dispose();
  }, [go2rtcUrl, stream, onPlayingChange]);

  return (
    <div ref={rootRef} className={isMain ? styles.main : styles.inset}>
      <div ref={frameRef} className={`${styles.frame} ${feed.thermal ? styles.thermal : ""}`} />
      {isMain && overlay}
      {status && <div className={styles.status}>{status}</div>}
      {!isMain && (
        <button
          type="button"
          className={styles.swap}
          aria-label={`Switch to ${feed.label}`}
          title={`Switch to ${feed.label}`}
          onClick={() => onSelect(stream)}
        />
      )}
    </div>
  );
}
