/** A rectangle in CSS pixels, relative to its container. */
export interface Rect {
  left: number;
  top: number;
  width: number;
  height: number;
}

/**
 * Where a picture of videoWidth × videoHeight is drawn inside a box of
 * boxWidth × boxHeight with object-fit: contain (centred, letterboxed or
 * pillarboxed). Without the picture's size, the whole box.
 */
export function containRect(boxWidth: number, boxHeight: number, videoWidth?: number, videoHeight?: number): Rect {
  if (!videoWidth || !videoHeight || boxWidth <= 0 || boxHeight <= 0) {
    return { left: 0, top: 0, width: Math.max(0, boxWidth), height: Math.max(0, boxHeight) };
  }
  const scale = Math.min(boxWidth / videoWidth, boxHeight / videoHeight);
  const width = videoWidth * scale;
  const height = videoHeight * scale;
  return { left: (boxWidth - width) / 2, top: (boxHeight - height) / 2, width, height };
}

const clamp01 = (v: number) => Math.min(1, Math.max(0, v));

/**
 * The point (px, py) of the box as fractions (0..1) of the picture, from its
 * top-left corner. Null outside the picture (on the black bars), unless
 * clamp is set, which pulls the point onto the picture's edge (for drags).
 */
export function toImage(rect: Rect, px: number, py: number, clamp = false): { x: number; y: number } | null {
  if (rect.width <= 0 || rect.height <= 0) return null;
  const x = (px - rect.left) / rect.width;
  const y = (py - rect.top) / rect.height;
  if (clamp) return { x: clamp01(x), y: clamp01(y) };
  return x < 0 || x > 1 || y < 0 || y > 1 ? null : { x, y };
}

/** A fraction of the picture back to box pixels. */
export function fromImage(rect: Rect, x: number, y: number): { left: number; top: number } {
  return { left: rect.left + x * rect.width, top: rect.top + y * rect.height };
}

/**
 * A picture of aspect width:height drawn centred inside rect (as a thermal
 * camera pillarboxes its 640x512 image in a 16:9 stream), in the same
 * coordinates as rect.
 */
export function innerRect(rect: Rect, width: number, height: number): Rect {
  const inner = containRect(rect.width, rect.height, width, height);
  return { ...inner, left: rect.left + inner.left, top: rect.top + inner.top };
}

/** True when (px, py) is on rect, edges included. */
export function contains(rect: Rect, px: number, py: number): boolean {
  return px >= rect.left && px <= rect.left + rect.width && py >= rect.top && py <= rect.top + rect.height;
}

/** (px, py) pulled onto rect's nearest edge when outside it. */
export function clampTo(rect: Rect, px: number, py: number): { px: number; py: number } {
  return {
    px: Math.min(rect.left + rect.width, Math.max(rect.left, px)),
    py: Math.min(rect.top + rect.height, Math.max(rect.top, py)),
  };
}
