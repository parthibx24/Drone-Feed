import { useEffect, useLayoutEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from "react";
import type { ThermalArea, ThermalMeasure, ThermalSpot } from "../../types/gateway";
import { containRect, fromImage, toImage, type Rect } from "../../utils/imageRect";
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

/** A drag shorter than this (fraction of the picture) is a click, not an area. */
const MIN_AREA = 0.02;
/** How long a new measurement shows where it was asked for before the aircraft's answer replaces it. */
const PENDING_MS = 4000;

const boxOf = (a: { x: number; y: number }, b: { x: number; y: number }): Box => ({
  x: Math.min(a.x, b.x),
  y: Math.min(a.y, b.y),
  width: Math.abs(a.x - b.x),
  height: Math.abs(a.y - b.y),
});

const near = (a: number, b: number) => Math.abs(a - b) < 0.01;

/**
 * Spot and area temperatures over the thermal picture. Clicks and drags are
 * turned into fractions of the picture itself, not of the frame around it:
 * the player letterboxes (object-fit: contain), so the black bars measure
 * nothing.
 */
export function ThermalOverlay({ videoSize, measure, spot, area, interactive, onSpot, onArea }: ThermalOverlayProps) {
  const rootRef = useRef<HTMLDivElement>(null);
  const [box, setBox] = useState({ width: 0, height: 0 });
  const [drag, setDrag] = useState<{ from: { x: number; y: number }; to: { x: number; y: number } } | null>(null);
  const [pendingSpot, setPendingSpot] = useState<{ x: number; y: number; at: number } | null>(null);
  const [pendingArea, setPendingArea] = useState<(Box & { at: number }) | null>(null);
  const down = useRef<{ x: number; y: number; px: number; py: number } | null>(null);

  useLayoutEffect(() => {
    const el = rootRef.current;
    if (!el) return;
    const measureBox = () => setBox({ width: el.clientWidth, height: el.clientHeight });
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

  const rect: Rect = containRect(box.width, box.height, videoSize?.width, videoSize?.height);
  const local = (event: ReactPointerEvent) => {
    const r = rootRef.current!.getBoundingClientRect();
    return { px: event.clientX - r.left, py: event.clientY - r.top };
  };

  const onPointerDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (!interactive || event.button !== 0) return;
    const { px, py } = local(event);
    const at = toImage(rect, px, py);
    if (!at) return; // on the black bars
    event.currentTarget.setPointerCapture(event.pointerId);
    down.current = { ...at, px, py };
  };

  const onPointerMove = (event: ReactPointerEvent<HTMLDivElement>) => {
    const start = down.current;
    if (!start || measure !== "area") return;
    const { px, py } = local(event);
    if (!drag && Math.hypot(px - start.px, py - start.py) < 6) return;
    setDrag({ from: start, to: toImage(rect, px, py, true)! });
  };

  const onPointerUp = (event: ReactPointerEvent<HTMLDivElement>) => {
    const start = down.current;
    down.current = null;
    if (!start) return;
    const { px, py } = local(event);
    if (measure === "spot") {
      setPendingSpot({ x: start.x, y: start.y, at: Date.now() });
      onSpot(start.x, start.y);
    } else if (measure === "area" && drag) {
      const b = boxOf(drag.from, toImage(rect, px, py, true)!);
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
      {shownSpot && <SpotMark rect={rect} spot={shownSpot} waiting={waiting} />}
      {shownArea && <AreaMark rect={rect} area={shownArea} drawing={Boolean(dragBox)} waiting={waiting} />}
      {interactive && !shownSpot && !shownArea && (
        <div className={styles.hint} role="status">
          {measure === "spot" ? "Click the thermal image to read a point" : "Drag a box on the thermal image to read an area"}
        </div>
      )}
    </div>
  );
}

function SpotMark({ rect, spot, waiting }: { rect: Rect; spot: ThermalSpot; waiting: boolean }) {
  const { left, top } = fromImage(rect, spot.x, spot.y);
  const flip = spot.x > 0.8; // keep the readout on the picture near the right edge
  return (
    <div className={styles.spot} style={{ left, top }}>
      <span className={styles.reticle} aria-hidden />
      <span className={`${styles.readout} ${flip ? styles.flip : ""}`} aria-live="polite">
        {waiting || spot.temp_c == null ? "Measuring…" : formatTemp(spot.temp_c)}
      </span>
    </div>
  );
}

function AreaMark({
  rect,
  area,
  drawing,
  waiting,
}: {
  rect: Rect;
  area: Box & Partial<Pick<ThermalArea, "avg_c" | "min_c" | "max_c">>;
  drawing: boolean;
  waiting: boolean;
}) {
  const a = fromImage(rect, area.x, area.y);
  const width = area.width * rect.width;
  const height = area.height * rect.height;
  const below = area.y + area.height < 0.8; // readout under the box, or above it near the bottom
  const ready = !drawing && !waiting && area.avg_c != null;
  return (
    <div className={`${styles.area} ${drawing ? styles.drawing : ""}`} style={{ left: a.left, top: a.top, width, height }}>
      {!drawing && (
        <div className={`${styles.areaReadout} ${below ? styles.below : styles.above}`} aria-live="polite">
          {ready ? (
            <>
              <span className={styles.avg}>
                {formatTemp(area.avg_c!)} <span className={styles.avgLabel}>average</span>
              </span>
              <span className={styles.range}>
                <span>Min {formatTemp(area.min_c ?? null)}</span>
                <span>Max {formatTemp(area.max_c ?? null)}</span>
              </span>
            </>
          ) : (
            <span className={styles.avg}>Measuring…</span>
          )}
        </div>
      )}
    </div>
  );
}
