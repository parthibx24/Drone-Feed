import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import type { LiveDetections } from "../../context/DroneContext";
import type { Detection } from "../../types/ai";
import { DETECTIONS_FRESH_MS, FOLLOW_COLOR, groupColor, labelName, percent } from "../../utils/ai";
import styles from "./DetectionOverlay.module.css";

interface DetectionOverlayProps {
  live: LiveDetections | undefined;
  /** Track being followed on this stream, highlighted. */
  followTrackId?: number | null;
  /** Small inset feed: boxes only, no labels, not clickable. */
  compact?: boolean;
  /** When set, boxes are clickable (operators, main feed). */
  onSelect?: (detection: Detection) => void;
}

interface Size {
  width: number;
  height: number;
}

/**
 * Detector boxes drawn over a video feed. The iframe letterboxes the video
 * (object-fit: contain), so boxes are placed inside the same fitted rectangle.
 */
export function DetectionOverlay({ live, followTrackId = null, compact = false, onSelect }: DetectionOverlayProps) {
  const rootRef = useRef<HTMLDivElement>(null);
  const [size, setSize] = useState<Size>({ width: 0, height: 0 });
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    const element = rootRef.current;
    if (!element) return;
    const observer = new ResizeObserver(([entry]) => {
      const { width, height } = entry.contentRect;
      setSize((prev) => (prev.width === width && prev.height === height ? prev : { width, height }));
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, []);

  // Re-check freshness so boxes disappear when frames stop arriving.
  useEffect(() => {
    if (!live) return;
    const timer = setInterval(() => setNow(Date.now()), 500);
    return () => clearInterval(timer);
  }, [live]);

  const fresh = live && now - live.receivedAt < DETECTIONS_FRESH_MS;
  const frame = fresh ? live.frame : null;

  let boxes: { detection: Detection; x: number; y: number; w: number; h: number }[] = [];
  if (frame && frame.width > 0 && frame.height > 0 && size.width > 0 && size.height > 0) {
    const scale = Math.min(size.width / frame.width, size.height / frame.height);
    const videoW = frame.width * scale;
    const videoH = frame.height * scale;
    const offsetX = (size.width - videoW) / 2;
    const offsetY = (size.height - videoH) / 2;
    boxes = frame.detections.map((detection) => {
      const [bx, by, bw, bh] = detection.box;
      return {
        detection,
        x: offsetX + bx * videoW,
        y: offsetY + by * videoH,
        w: Math.max(2, bw * videoW),
        h: Math.max(2, bh * videoH),
      };
    });
  }

  const select = (detection: Detection) => onSelect?.(detection);
  const onKey = (event: KeyboardEvent, detection: Detection) => {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      select(detection);
    }
  };

  return (
    <div ref={rootRef} className={styles.root} aria-hidden={boxes.length === 0}>
      {boxes.length > 0 && (
        <svg className={styles.svg} width={size.width} height={size.height}>
          {boxes.map(({ detection, x, y, w, h }) => {
            const followed = followTrackId !== null && detection.track_id === followTrackId;
            const color = followed ? FOLLOW_COLOR : groupColor(detection.group);
            const text = `${labelName(detection.label)} ${percent(detection.confidence)}${followed ? " · following" : ""}`;
            const labelW = text.length * 7 + 10;
            const labelY = y > 20 ? y - 20 : y + h;
            const clickable = Boolean(onSelect) && !compact;
            return (
              <g key={`${detection.track_id}-${detection.label}`}>
                <rect
                  className={clickable ? styles.clickable : undefined}
                  x={x}
                  y={y}
                  width={w}
                  height={h}
                  fill={followed ? "rgba(255, 59, 59, 0.12)" : "transparent"}
                  stroke={color}
                  strokeWidth={followed ? 3 : compact ? 1.5 : 2}
                  rx={2}
                  {...(clickable
                    ? {
                        role: "button",
                        tabIndex: 0,
                        "aria-label": `Follow ${labelName(detection.label)} #${detection.track_id}`,
                        onClick: () => select(detection),
                        onKeyDown: (event: KeyboardEvent) => onKey(event, detection),
                      }
                    : {})}
                >
                  {clickable && <title>{`Follow ${labelName(detection.label)} #${detection.track_id}`}</title>}
                </rect>
                {!compact && (
                  <g className={styles.label} transform={`translate(${x}, ${labelY})`}>
                    <rect width={labelW} height={20} rx={3} fill={color} />
                    <text x={5} y={14} className={styles.labelText}>
                      {text}
                    </text>
                  </g>
                )}
              </g>
            );
          })}
        </svg>
      )}
    </div>
  );
}
