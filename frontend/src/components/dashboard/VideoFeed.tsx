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
  /** Drawn over the video (e.g. detection boxes), outside the thermal filter. */
  overlay?: ReactNode;
}

/**
 * One camera feed. The main and inset positions are purely CSS, so swapping
 * feeds never moves the iframe in the DOM (which would restart the stream).
 */
export function VideoFeed({ feed, go2rtcUrl, isMain, rootRef, onPlayingChange, onSelect, overlay }: VideoFeedProps) {
  const frameRef = useRef<HTMLDivElement>(null);
  const [status, setStatus] = useState<string | null>(null);
  const { stream } = feed;

  useEffect(() => {
    const player = new VideoPlayer(frameRef.current!, {
      onStatus: setStatus,
      onPlayingChange: (playing) => onPlayingChange(stream, playing),
    });
    player.connect(go2rtcUrl, stream).catch((error) => console.warn(`Video failed for ${stream}:`, error));
    return () => player.dispose();
  }, [go2rtcUrl, stream, onPlayingChange]);

  return (
    <div ref={rootRef} className={isMain ? styles.main : styles.inset}>
      <div ref={frameRef} className={`${styles.frame} ${feed.thermal ? styles.thermal : ""}`} />
      {overlay}
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
