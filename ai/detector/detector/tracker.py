"""Gives detections stable ids across frames and decides when an object is
new enough to report as an event.

Objects seen from a drone are small and the drone moves, so between two
frames (a third of a second apart) a box can move further than its own size.
Matching therefore uses overlap first, then distance between centres.
"""

from __future__ import annotations

from dataclasses import dataclass, field

from .model import Detection


def iou(a, b) -> float:
    ax2, ay2 = a[0] + a[2], a[1] + a[3]
    bx2, by2 = b[0] + b[2], b[1] + b[3]
    iw = max(0.0, min(ax2, bx2) - max(a[0], b[0]))
    ih = max(0.0, min(ay2, by2) - max(a[1], b[1]))
    inter = iw * ih
    union = a[2] * a[3] + b[2] * b[3] - inter
    return inter / union if union > 0 else 0.0


def centre_distance(a, b) -> float:
    """Distance between centres, in units of the larger box's larger side."""
    ax, ay = a[0] + a[2] / 2, a[1] + a[3] / 2
    bx, by = b[0] + b[2] / 2, b[1] + b[3] / 2
    scale = max(a[2], a[3], b[2], b[3], 1e-6)
    return ((ax - bx) ** 2 + (ay - by) ** 2) ** 0.5 / scale


@dataclass
class Track:
    id: int
    label: str
    group: str
    box: tuple[float, float, float, float]
    confidence: float
    hits: int = 1
    last_seen: float = 0.0
    reported: bool = False


@dataclass
class Tracker:
    min_hits: int = 3
    max_age_s: float = 2.0
    iou_match: float = 0.2
    max_centre_distance: float = 2.0
    _next: int = 1
    tracks: list[Track] = field(default_factory=list)

    def update(self, dets: list[Detection], now: float) -> tuple[list[tuple[Track, Detection]], list[Track]]:
        """Matches detections to tracks. Returns (track, detection) pairs for this
        frame and the tracks that just became confirmed (report these once)."""
        pairs: list[tuple[Track, Detection]] = []
        unmatched = set(range(len(dets)))
        free = set(range(len(self.tracks)))

        # Pass 1: overlap. Pass 2: nearby centres. Same group only.
        for score_fn, accept in ((lambda t, d: iou(t.box, d.box), lambda s: s >= self.iou_match),
                                 (lambda t, d: -centre_distance(t.box, d.box), lambda s: -s <= self.max_centre_distance)):
            cands = []
            for ti in free:
                for di in unmatched:
                    t, d = self.tracks[ti], dets[di]
                    if t.group != d.group:
                        continue
                    s = score_fn(t, d)
                    if accept(s):
                        cands.append((s, ti, di))
            cands.sort(reverse=True)
            for _, ti, di in cands:
                if ti in free and di in unmatched:
                    free.discard(ti)
                    unmatched.discard(di)
                    t, d = self.tracks[ti], dets[di]
                    t.box, t.label, t.hits, t.last_seen = d.box, d.label, t.hits + 1, now
                    t.confidence = 0.7 * t.confidence + 0.3 * d.confidence
                    pairs.append((t, d))

        for di in sorted(unmatched):
            d = dets[di]
            t = Track(self._next, d.label, d.group, d.box, d.confidence, last_seen=now)
            self._next += 1
            self.tracks.append(t)
            pairs.append((t, d))

        self.tracks = [t for t in self.tracks if now - t.last_seen <= self.max_age_s]
        confirmed = []
        for t, _ in pairs:
            if not t.reported and t.hits >= self.min_hits:
                t.reported = True
                confirmed.append(t)
        return pairs, confirmed
