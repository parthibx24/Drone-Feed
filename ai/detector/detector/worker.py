"""One worker per camera stream: a reader thread keeps only the newest frame,
and the worker runs the model on it a few times a second."""

from __future__ import annotations

import datetime as dt
import logging
import threading
import time

import cv2
import numpy as np

from .client import GatewayClient
from .model import YoloOnnx
from .tracker import Tracker

log = logging.getLogger("detector")

GROUP_COLOURS = {"person": (60, 200, 255), "vehicle": (255, 170, 60), "other": (200, 200, 200)}  # BGR


class FrameReader(threading.Thread):
    """Reads a stream continuously so the model always sees the newest frame
    (reading only when needed would let decoder buffers fall behind)."""

    def __init__(self, url: str, stop: threading.Event):
        super().__init__(daemon=True)
        self.url, self.stop = url, stop
        self.lock = threading.Lock()
        self.frame: np.ndarray | None = None
        self.seq = 0
        self.connected = False

    def latest(self) -> tuple[np.ndarray | None, int]:
        with self.lock:
            return self.frame, self.seq

    def run(self) -> None:
        backoff = 2.0
        while not self.stop.is_set():
            cap = cv2.VideoCapture(self.url, cv2.CAP_FFMPEG)
            if not cap.isOpened():
                cap.release()
                log.info("stream not available, retrying in %.0fs: %s", backoff, self.url)
                self.stop.wait(backoff)
                backoff = min(backoff * 2, 30.0)
                continue
            log.info("stream opened: %s", self.url)
            self.connected, backoff = True, 2.0
            failures = 0
            while not self.stop.is_set():
                ok, frame = cap.read()
                if not ok or frame is None:
                    failures += 1
                    if failures > 50:
                        break
                    time.sleep(0.02)
                    continue
                failures = 0
                with self.lock:
                    self.frame, self.seq = frame, self.seq + 1
            cap.release()
            self.connected = False
            if not self.stop.is_set():
                log.info("stream ended, reconnecting: %s", self.url)
                self.stop.wait(1.0)


def annotate(frame: np.ndarray, boxes, highlight_id: int, width: int = 960) -> bytes:
    """JPEG of the frame with all boxes drawn and one highlighted, scaled to `width`."""
    h, w = frame.shape[:2]
    scale = min(1.0, width / w)
    img = cv2.resize(frame, (round(w * scale), round(h * scale))) if scale < 1 else frame.copy()
    ih, iw = img.shape[:2]
    for track_id, label, group, conf, (x, y, bw, bh) in boxes:
        p1 = (int(x * iw), int(y * ih))
        p2 = (int((x + bw) * iw), int((y + bh) * ih))
        main = track_id == highlight_id
        colour = (40, 40, 255) if main else GROUP_COLOURS.get(group, (200, 200, 200))
        if main:  # pad tiny boxes so the object stays visible in the snapshot
            p1 = (max(0, p1[0] - 6), max(0, p1[1] - 6))
            p2 = (min(iw - 1, p2[0] + 6), min(ih - 1, p2[1] + 6))
        cv2.rectangle(img, p1, p2, colour, 3 if main else 1)
        if main:
            text = f"{label} {conf * 100:.0f}%"
            cv2.putText(img, text, (p1[0], max(14, p1[1] - 6)), cv2.FONT_HERSHEY_SIMPLEX, 0.55, (0, 0, 0), 3, cv2.LINE_AA)
            cv2.putText(img, text, (p1[0], max(14, p1[1] - 6)), cv2.FONT_HERSHEY_SIMPLEX, 0.55, colour, 1, cv2.LINE_AA)
    for track_id, _, _, _, box in boxes:
        if track_id == highlight_id:
            _inset(img, frame, box)
    ok, buf = cv2.imencode(".jpg", img, [cv2.IMWRITE_JPEG_QUALITY, 82])
    return buf.tobytes() if ok else b""


def _inset(img: np.ndarray, frame: np.ndarray, box) -> None:
    """Pastes a magnified crop of the object into the top-left corner, because
    objects seen from a drone are often only a few pixels in the snapshot."""
    fh, fw = frame.shape[:2]
    x, y, bw, bh = box
    cx, cy = (x + bw / 2) * fw, (y + bh / 2) * fh
    side = max(64.0, 3 * max(bw * fw, bh * fh))  # some context around it
    x0, y0 = int(max(0, cx - side / 2)), int(max(0, cy - side / 2))
    x1, y1 = int(min(fw, cx + side / 2)), int(min(fh, cy + side / 2))
    crop = frame[y0:y1, x0:x1]
    if crop.size == 0:
        return
    ih, iw = img.shape[:2]
    target = int(min(iw, ih) * 0.32)
    scale = target / max(crop.shape[:2])
    crop = cv2.resize(crop, (max(1, int(crop.shape[1] * scale)), max(1, int(crop.shape[0] * scale))),
                      interpolation=cv2.INTER_CUBIC)
    ch, cw = crop.shape[:2]
    m = 8
    if ch + 2 * m > ih or cw + 2 * m > iw:
        return
    img[m:m + ch, m:m + cw] = crop
    cv2.rectangle(img, (m - 2, m - 2), (m + cw + 1, m + ch + 1), (40, 40, 255), 2)
    # outline the object inside the magnified view too
    ox1, oy1 = int(m + (x * fw - x0) * scale), int(m + (y * fh - y0) * scale)
    ox2, oy2 = int(m + ((x + bw) * fw - x0) * scale), int(m + ((y + bh) * fh - y0) * scale)
    cv2.rectangle(img, (ox1 - 3, oy1 - 3), (ox2 + 3, oy2 + 3), (40, 40, 255), 2)


class StreamWorker(threading.Thread):
    def __init__(self, drone_id: str, stream: str, kind: str, url: str, model: YoloOnnx,
                 client: GatewayClient, settings: dict):
        super().__init__(daemon=True, name=f"{drone_id}/{stream}")
        self.drone_id, self.stream, self.kind = drone_id, stream, kind
        self.model, self.client, self.settings = model, client, settings
        self.stop_event = threading.Event()
        self.reader = FrameReader(url, self.stop_event)
        self.tracker = Tracker(min_hits=settings.get("min_hits", 3), max_age_s=settings.get("max_age_s", 2.0))
        self.stats = {"frames": 0, "infer_ms": 0.0, "events": 0}

    def stop(self) -> None:
        self.stop_event.set()

    def run(self) -> None:
        self.reader.start()
        last_seq = 0
        while not self.stop_event.is_set():
            period = 1.0 / max(0.2, float(self.settings.get("fps", 3)))
            started = time.monotonic()
            frame, seq = self.reader.latest()
            if frame is not None and seq != last_seq:
                last_seq = seq
                try:
                    self.process(frame)
                except Exception:  # keep the worker alive; the next frame may work
                    log.exception("processing failed for %s", self.name)
            self.stop_event.wait(max(0.0, period - (time.monotonic() - started)))
        self.reader.join(timeout=5)

    def process(self, frame: np.ndarray) -> None:
        conf = float(self.settings.get("min_confidence", 0.35))
        tiles = int(self.settings.get("tiles", 1)) if self.kind == "visible" else 1
        dets, ms = self.model.detect_tiled(frame, tiles, conf=conf)
        now = time.monotonic()
        pairs, confirmed = self.tracker.update(dets, now)
        ts = dt.datetime.now(dt.timezone.utc).isoformat().replace("+00:00", "Z")
        h, w = frame.shape[:2]
        payload = {
            "drone_id": self.drone_id, "stream": self.stream, "kind": self.kind, "ts": ts,
            "width": w, "height": h, "model": self.model.name, "infer_ms": round(ms, 1),
            "detections": [{"track_id": t.id, "label": d.label, "group": d.group,
                            "confidence": round(d.confidence, 3), "box": [round(v, 5) for v in d.box]}
                           for t, d in pairs],
        }
        self.stats["frames"] += 1
        self.stats["infer_ms"] = 0.9 * self.stats["infer_ms"] + 0.1 * ms if self.stats["infer_ms"] else ms
        try:
            self.client.detections(payload)
        except Exception as e:
            log.warning("posting detections failed: %s", e)
            return
        if not confirmed:
            return
        boxes = [(t.id, d.label, d.group, d.confidence, d.box) for t, d in pairs]
        for t in confirmed:
            event = {"drone_id": self.drone_id, "stream": self.stream, "kind": self.kind, "ts": ts,
                     "track_id": t.id, "label": t.label, "group": t.group,
                     "confidence": round(t.confidence, 3), "box": [round(v, 5) for v in t.box]}
            try:
                self.client.event(event, annotate(frame, boxes, t.id, int(self.settings.get("snapshot_width", 960))))
                self.stats["events"] += 1
            except Exception as e:
                log.warning("posting event failed: %s", e)
