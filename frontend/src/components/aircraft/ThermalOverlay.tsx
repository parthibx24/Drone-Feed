import { useEffect, useLayoutEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from "react";
import type { ThermalArea, ThermalMeasure, ThermalSpot } from "../../types/gateway";
import { clampTo, containRect, contains, fromImage, innerRect, toImage, type Rect } from "../../utils/imageRect";
import { formatTemp } from "./thermal";
import styles from "./ThermalOverlay.module.css";

interface ThermalOverlayProps {
  /** The picture's own size, when the player has reported it. */
  videoSize: { width: number; height: number } | null;
  measure: ThermalMeasure;
  spot: ThermalSpot | null;
  area: ThermalArea | null;
  /** True when a click or drag measures (operator, aircraft online, thermal lens). */
  interactive: boolean;
  onSpot: (x: number, y: number) => void;
  onArea: (x: number, y: number, width: number, height: number) => void;
}

type Box = { x: number; y: number; width: number; height: number };

interface MarkProps {
  /** The frame, and the thermal image inside it (overlay pixels). */
  rect: Rect;
  image: Rect;
  /** Where the image stops being covered by the video's top controls. */
  clearTop: number;
  /** One rem in pixels: the dashboard scales with the window. */
  rem: number;
  waiting: boolean;
}

/** The lens selector, status chips and record button float over the video's top this far (rem). */
const TOP_CONTROLS_REM = 6.25;

/** A drag shorter than this (fraction of the picture) is a click, not an area. */
const MIN_AREA = 0.02;
/** How long a new measurement shows where it was asked for before the aircraft's answer replaces it. */
const PENDING_MS = 4000;
/**
 * The thermal sensor's picture (640x512), drawn centred in the stream's frame:
 * the Mavic 3T sends a 16:9 frame with the image pillarboxed in its middle.
 */
const IR_WIDTH = 640;
const IR_HEIGHT = 512;

const boxOf = (a: { x: number; y: number }, b: { x: number; y: number }): Box => ({
  x: Math.min(a.x, b.x),
  y: Math.min(a.y, b.y),
  width: Math.abs(a.x - b.x),
  height: Math.abs(a.y - b.y),
});

const near = (a: number, b: number) => Math.abs(a - b) < 0.01;

/**
 * Spot and area temperatures over the thermal picture. Clicks and drags are
 * sent as fractions of the stream's frame (the player letterboxes it with
 * object-fit: contain). Only the thermal image inside that frame can be
 * measured: on the black bars around it the cursor shows "not allowed",
 * clicks do nothing and drags are held to the image's edge.
 */
export function ThermalOverlay({ videoSize, measure, spot, area, interactive, onSpot, onArea }: ThermalOverlayProps) {
  const rootRef = useRef<HTMLDivElement>(null);
  const [box, setBox] = useState({ width: 0, height: 0, rem: 16 });
  const [drag, setDrag] = useState<{ from: { x: number; y: number }; to: { x: number; y: number } } | null>(null);
  const [pendingSpot, setPendingSpot] = useState<{ x: number; y: number; at: number } | null>(null);
  const [pendingArea, setPendingArea] = useState<(Box & { at: number }) | null>(null);
  const down = useRef<{ x: number; y: number; px: number; py: number } | null>(null);

  useLayoutEffect(() => {
    const el = rootRef.current;
    if (!el) return;
    const measureBox = () =>
      setBox({
        width: el.clientWidth,
        height: el.clientHeight,
        rem: parseFloat(getComputedStyle(document.documentElement).fontSize) || 16,
      });
    measureBox();
    const observer = new ResizeObserver(measureBox);
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  // The aircraft's answer replaces the pending mark once it is at the same place, or after a while.
  useEffect(() => {
    if (pendingSpot && spot && near(spot.x, pendingSpot.x) && near(spot.y, pendingSpot.y)) setPendingSpot(null);
  }, [spot, pendingSpot]);
  useEffect(() => {
    if (pendingArea && area && near(area.x, pendingArea.x) && near(area.y, pendingArea.y)) setPendingArea(null);
  }, [area, pendingArea]);
  useEffect(() => {
    if (!pendingSpot && !pendingArea) return;
    const timer = window.setTimeout(() => {
      setPendingSpot(null);
      setPendingArea(null);
    }, PENDING_MS);
    return () => window.clearTimeout(timer);
  }, [pendingSpot, pendingArea]);

  // Escape abandons a drag.
  useEffect(() => {
    if (!drag) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        down.current = null;
        setDrag(null);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [drag]);

  // The frame in the box, and the thermal image in the frame.
  const rect: Rect = containRect(box.width, box.height, videoSize?.width, videoSize?.height);
  const image: Rect = innerRect(rect, IR_WIDTH, IR_HEIGHT);
  const marks = { rect, image, clearTop: Math.max(image.top, TOP_CONTROLS_REM * box.rem), rem: box.rem };
  const local = (event: ReactPointerEvent) => {
    const r = rootRef.current!.getBoundingClientRect();
    return { px: event.clientX - r.left, py: event.clientY - r.top };
  };
  /** A pointer position held to the image, as a fraction of the frame. */
  const onImage = (event: ReactPointerEvent) => {
    const { px, py } = local(event);
    const held = clampTo(image, px, py);
    return toImage(rect, held.px, held.py, true)!;
  };

  const onPointerDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (!interactive || event.button !== 0) return;
    const { px, py } = local(event);
    if (!contains(image, px, py)) return; // on the black bars
    const at = toImage(rect, px, py, true)!;
    event.currentTarget.setPointerCapture(event.pointerId);
    down.current = { ...at, px, py };
  };

  const onPointerMove = (event: ReactPointerEvent<HTMLDivElement>) => {
    const start = down.current;
    if (!start || measure !== "area") return;
    const { px, py } = local(event);
    if (!drag && Math.hypot(px - start.px, py - start.py) < 6) return;
    setDrag({ from: start, to: onImage(event) });
  };

  const onPointerUp = (event: ReactPointerEvent<HTMLDivElement>) => {
    const start = down.current;
    down.current = null;
    if (!start) return;
    if (measure === "spot") {
      setPendingSpot({ x: start.x, y: start.y, at: Date.now() });
      onSpot(start.x, start.y);
    } else if (measure === "area" && drag) {
      const b = boxOf(drag.from, onImage(event));
      setDrag(null);
      if (b.width >= MIN_AREA && b.height >= MIN_AREA) {
        setPendingArea({ ...b, at: Date.now() });
        onArea(b.x, b.y, b.width, b.height);
      }
    }
  };

  const onPointerCancel = () => {
    down.current = null;
    setDrag(null);
  };

  // What to draw: a drag in progress, else what was just asked for, else the aircraft's reading.
  const shownSpot = measure === "spot" ? (pendingSpot ? { ...pendingSpot, temp_c: null } : spot) : null;
  const dragBox = drag ? boxOf(drag.from, drag.to) : null;
  const shownArea =
    measure === "area" ? (dragBox ?? (pendingArea ? { ...pendingArea, avg_c: null, min_c: null, max_c: null } : area)) : null;
  const waiting = Boolean(pendingSpot || pendingArea);

  return (
    <div
      ref={rootRef}
      className={`${styles.overlay} ${interactive ? styles.interactive : ""}`}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onPointerCancel={onPointerCancel}
      onContextMenu={(e) => interactive && e.preventDefault()}
    >
      {interactive && (
        <div
          className={styles.target}
          style={{ left: image.left, top: image.top, width: image.width, height: image.height }}
          aria-hidden
        />
      )}
      {shownSpot && <SpotMark {...marks} spot={shownSpot} waiting={waiting} />}
      {shownArea && <AreaMark {...marks} area={shownArea} drawing={Boolean(dragBox)} waiting={waiting} />}
      {interactive && !shownSpot && !shownArea && (
        <div className={styles.hint} role="status">
          {measure === "spot" ? "Click the image to read a point" : "Drag on the image to read an area"}
        </div>
      )}
    </div>
  );
}

function SpotMark({ rect, image, clearTop, rem, spot, waiting }: MarkProps & { spot: ThermalSpot }) {
  const { left, top } = fromImage(rect, spot.x, spot.y);
  // The readout sits up and to the right of the reticle. Near the image's right
  // edge it goes to the left; where it would be under the video's top controls,
  // it goes below the reticle, far enough down to be seen.
  const flipX = left > image.left + image.width * 0.75;
  const gap = 0.75 * rem;
  const readoutHeight = 2.3 * rem;
  const above = top - gap - readoutHeight >= clearTop;
  const reading = !waiting && spot.temp_c != null;
  return (
    <div className={styles.spot} style={{ left, top }}>
      <span className={styles.reticle} aria-hidden />
      <span
        className={`${styles.readout} ${flipX ? styles.flipX : ""} ${reading ? "" : styles.pending}`}
        style={above ? undefined : { top: Math.max(gap, clearTop - top + gap / 2), bottom: "auto" }}
        aria-live="polite"
      >
        {reading ? formatTemp(spot.temp_c) : "Measuring…"}
      </span>
    </div>
  );
}

function AreaMark({
  rect,
  image,
  clearTop,
  rem,
  area,
  drawing,
  waiting,
}: MarkProps & {
  area: Box & Partial<Pick<ThermalArea, "avg_c" | "min_c" | "max_c">>;
  drawing: boolean;
}) {
  const a = fromImage(rect, area.x, area.y);
  const width = area.width * rect.width;
  const height = area.height * rect.height;
  // The readout goes under the box, else above it, else inside it (below the
  // video's top controls); it lines up with the box's right edge when the box
  // is on the right of the image.
  const gap = 0.5 * rem;
  const readoutHeight = 3.3 * rem;
  const below = a.top + height + gap + readoutHeight <= image.top + image.height;
  const above = a.top - gap - readoutHeight >= clearTop;
  const alignRight = a.left + width / 2 > image.left + image.width * 0.6;
  const ready = !drawing && !waiting && area.avg_c != null;
  return (
    <div className={`${styles.area} ${drawing ? styles.drawing : ""}`} style={{ left: a.left, top: a.top, width, height }}>
      {!drawing && (
        <div
          className={`${styles.areaReadout} ${below ? styles.below : above ? styles.above : styles.inside} ${alignRight ? styles.alignRight : ""}`}
          style={below || above ? undefined : { top: Math.max(gap, clearTop - a.top + gap) }}
          aria-live="polite"
        >
          {ready ? (
            <>
              <Stat label="Avg" value={area.avg_c!} />
              <Stat label="Min" value={area.min_c ?? null} />
              <Stat label="Max" value={area.max_c ?? null} />
            </>
          ) : (
            <span className={styles.pending}>Measuring…</span>
          )}
        </div>
      )}
    </div>
  );
}

function Stat({ label, value }: { label: string; value: number | null }) {
  return (
    <span className={styles.stat}>
      <span className={styles.statLabel}>{label}</span>
      {formatTemp(value)}
    </span>
  );
}
