"""Runs an Ultralytics-format YOLO detector exported to ONNX.

Input: 1x3xSxS RGB float32 in 0-1, letterboxed with grey (114) padding.
Output: 1x(4+nc)xN, rows are centre-x, centre-y, width, height (input pixels)
followed by one score per class. NMS is not in the graph.
"""

from __future__ import annotations

import ast
import time
from dataclasses import dataclass

import cv2
import numpy as np
import onnxruntime as ort

from .labels import normalise


@dataclass
class Detection:
    label: str
    group: str
    confidence: float
    box: tuple[float, float, float, float]  # x, y, w, h normalised to the frame, top-left origin


def parse_names(meta: dict[str, str]) -> list[str]:
    """Class names from Ultralytics export metadata: "{0: 'person', ...}" or "person,car"."""
    raw = meta.get("names", "")
    if not raw:
        return []
    if raw.lstrip().startswith("{"):
        d = ast.literal_eval(raw)
        return [d[k] for k in sorted(d)]
    return [n.strip() for n in raw.split(",")]


def nms(boxes: np.ndarray, scores: np.ndarray, iou: float, smaller: bool = False) -> list[int]:
    """NMS over xyxy boxes; returns kept indices, best first. With smaller=True the
    overlap is measured against the smaller box, which also removes a box cut off
    at a tile edge that lies inside the whole box from another pass."""
    order = scores.argsort()[::-1]
    keep: list[int] = []
    x1, y1, x2, y2 = boxes.T
    areas = (x2 - x1) * (y2 - y1)
    while order.size:
        i = order[0]
        keep.append(int(i))
        xx1 = np.maximum(x1[i], x1[order[1:]])
        yy1 = np.maximum(y1[i], y1[order[1:]])
        xx2 = np.minimum(x2[i], x2[order[1:]])
        yy2 = np.minimum(y2[i], y2[order[1:]])
        inter = np.clip(xx2 - xx1, 0, None) * np.clip(yy2 - yy1, 0, None)
        if smaller:
            ov = inter / (np.minimum(areas[i], areas[order[1:]]) + 1e-9)
        else:
            ov = inter / (areas[i] + areas[order[1:]] - inter + 1e-9)
        order = order[1:][ov <= iou]
    return keep


class YoloOnnx:
    def __init__(self, path: str, threads: int = 1, name: str | None = None):
        opts = ort.SessionOptions()
        opts.intra_op_num_threads = max(1, threads)
        opts.inter_op_num_threads = 1
        opts.graph_optimization_level = ort.GraphOptimizationLevel.ORT_ENABLE_ALL
        self.session = ort.InferenceSession(path, sess_options=opts, providers=["CPUExecutionProvider"])
        inp = self.session.get_inputs()[0]
        self.input_name = inp.name
        shape = inp.shape
        self.size = int(shape[2]) if isinstance(shape[2], int) else 640
        meta = self.session.get_modelmeta().custom_metadata_map
        self.names = parse_names(meta)
        out_shape = self.session.get_outputs()[0].shape
        if not self.names and isinstance(out_shape[1], int):
            self.names = [f"class{i}" for i in range(out_shape[1] - 4)]
        self.labels = [normalise(n) for n in self.names]
        self.name = name or meta.get("description", path)

    def _letterbox(self, img: np.ndarray) -> tuple[np.ndarray, float, int, int]:
        h, w = img.shape[:2]
        r = min(self.size / h, self.size / w)
        nh, nw = round(h * r), round(w * r)
        resized = cv2.resize(img, (nw, nh), interpolation=cv2.INTER_LINEAR) if (nh, nw) != (h, w) else img
        top = (self.size - nh) // 2
        left = (self.size - nw) // 2
        canvas = np.full((self.size, self.size, 3), 114, dtype=np.uint8)
        canvas[top:top + nh, left:left + nw] = resized
        return canvas, r, left, top

    def detect_tiled(self, bgr: np.ndarray, tiles: int, conf: float = 0.35, iou: float = 0.5) -> tuple[list[Detection], float]:
        """Like detect, but also runs the model on `tiles` overlapping parts of the
        frame (2 = left/right halves, 4 = quadrants), so small objects are seen at
        higher resolution. Costs about (tiles + 1) times the CPU."""
        if tiles not in (2, 4):
            return self.detect(bgr, conf, iou)
        h, w = bgr.shape[:2]
        cols, rows = (2, 1) if tiles == 2 else (2, 2)
        ov = 0.15  # overlap so objects on a seam are whole in one tile
        tw, th = int(w / cols * (1 + ov)), int(h / rows * (1 + (ov if rows > 1 else 0)))
        all_dets, total_ms = self.detect(bgr, conf, iou)
        for r in range(rows):
            for c in range(cols):
                x0 = 0 if c == 0 else w - tw
                y0 = 0 if r == 0 else h - th
                dets, ms = self.detect(bgr[y0:y0 + th, x0:x0 + tw], conf, iou)
                total_ms += ms
                for d in dets:
                    x, y, bw, bh = d.box
                    all_dets.append(Detection(d.label, d.group, d.confidence,
                                              ((x0 + x * tw) / w, (y0 + y * th) / h, bw * tw / w, bh * th / h)))
        if not all_dets:
            return [], total_ms
        # merge duplicates from the full frame and the tiles, per label
        names = sorted({d.label for d in all_dets})
        boxes = np.array([[d.box[0], d.box[1], d.box[0] + d.box[2], d.box[1] + d.box[3]] for d in all_dets], dtype=np.float32)
        offs = np.array([names.index(d.label) * 2.0 for d in all_dets], dtype=np.float32)[:, None]
        scores = np.array([d.confidence for d in all_dets], dtype=np.float32)
        keep = nms(boxes + offs, scores, 0.6, smaller=True)
        return [all_dets[i] for i in keep][:300], total_ms

    def detect(self, bgr: np.ndarray, conf: float = 0.35, iou: float = 0.5) -> tuple[list[Detection], float]:
        """Detects objects in a BGR frame. Returns detections and inference time (ms)."""
        if bgr.ndim == 2:
            bgr = cv2.cvtColor(bgr, cv2.COLOR_GRAY2BGR)
        h, w = bgr.shape[:2]
        canvas, r, left, top = self._letterbox(bgr)
        x = cv2.cvtColor(canvas, cv2.COLOR_BGR2RGB).transpose(2, 0, 1)[None].astype(np.float32) / 255.0
        t0 = time.perf_counter()
        out = self.session.run(None, {self.input_name: x})[0][0]  # (4+nc, N)
        ms = (time.perf_counter() - t0) * 1000
        preds = out.T
        scores_all = preds[:, 4:]
        cls = scores_all.argmax(axis=1)
        scores = scores_all[np.arange(len(cls)), cls]
        m = scores >= conf
        if not m.any():
            return [], ms
        preds, cls, scores = preds[m], cls[m], scores[m]
        cx, cy, bw, bh = preds[:, 0], preds[:, 1], preds[:, 2], preds[:, 3]
        x1 = (cx - bw / 2 - left) / r
        y1 = (cy - bh / 2 - top) / r
        x2 = (cx + bw / 2 - left) / r
        y2 = (cy + bh / 2 - top) / r
        boxes = np.stack([np.clip(x1, 0, w), np.clip(y1, 0, h), np.clip(x2, 0, w), np.clip(y2, 0, h)], axis=1)
        # class-aware NMS: offset each class far apart so boxes of different classes never suppress each other
        offs = cls[:, None].astype(np.float32) * (max(w, h) + 1)
        keep = nms(boxes + offs, scores, iou)[:300]
        dets = []
        for i in keep:
            bx1, by1, bx2, by2 = boxes[i]
            if bx2 - bx1 < 1 or by2 - by1 < 1:
                continue
            label, group = self.labels[int(cls[i])] if int(cls[i]) < len(self.labels) else (f"class{int(cls[i])}", "other")
            dets.append(Detection(label, group, float(scores[i]),
                                  (float(bx1 / w), float(by1 / h), float((bx2 - bx1) / w), float((by2 - by1) / h))))
        return dets, ms
