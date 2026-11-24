"""Precision and recall per class at several confidence thresholds, measured
through the detector's own pipeline (ai/detector), on images with Pascal VOC
labels. Use it to pick ai.min_confidence / ai.min_confidence_thermal.

    cd ai/detector
    PYTHONPATH=. python ../training/eval_thresholds.py --model models/thermal.onnx \
        --images <dir with .jpg> --labels <dir with .xml> \
        --map Person=person,Car=car,Bicycle=bike,OtherVehicle=vehicle
"""

from __future__ import annotations

import argparse
import glob
import os
import xml.etree.ElementTree as ET

import cv2

from detector.model import YoloOnnx


def iou(a, b) -> float:
    ix = max(0.0, min(a[2], b[2]) - max(a[0], b[0]))
    iy = max(0.0, min(a[3], b[3]) - max(a[1], b[1]))
    inter = ix * iy
    return inter / ((a[2] - a[0]) * (a[3] - a[1]) + (b[2] - b[0]) * (b[3] - b[1]) - inter + 1e-9)


def main() -> None:
    p = argparse.ArgumentParser()
    p.add_argument("--model", required=True)
    p.add_argument("--images", required=True)
    p.add_argument("--labels", required=True)
    p.add_argument("--map", required=True, help="label-file name=detector label, comma separated")
    p.add_argument("--tiles", type=int, default=1)
    a = p.parse_args()
    name_map = dict(kv.split("=") for kv in a.map.split(","))
    model = YoloOnnx(a.model, threads=4)
    thresholds = [0.15, 0.2, 0.25, 0.3, 0.35, 0.45]
    scored: dict[str, list[tuple[float, bool]]] = {}
    truth: dict[str, int] = {}
    for img_path in sorted(glob.glob(os.path.join(a.images, "*.jpg"))):
        stem = os.path.splitext(os.path.basename(img_path))[0]
        root = ET.parse(os.path.join(a.labels, stem + ".xml")).getroot()
        gt: dict[str, list] = {}
        for o in root.findall("object"):
            label = name_map.get(o.find("name").text)
            if label:
                b = o.find("bndbox")
                gt.setdefault(label, []).append([float(b.find(k).text) for k in ("xmin", "ymin", "xmax", "ymax")])
        img = cv2.imread(img_path)
        h, w = img.shape[:2]
        dets, _ = model.detect_tiled(img, a.tiles, conf=min(thresholds))
        for label in set(gt) | {d.label for d in dets}:
            boxes = gt.get(label, [])
            truth[label] = truth.get(label, 0) + len(boxes)
            used: set[int] = set()
            for d in sorted((d for d in dets if d.label == label), key=lambda d: -d.confidence):
                x, y, bw, bh = d.box
                pb = [x * w, y * h, (x + bw) * w, (y + bh) * h]
                best, bi = 0.0, -1
                for i, g in enumerate(boxes):
                    if i not in used and (v := iou(pb, g)) > best:
                        best, bi = v, i
                hit = best >= 0.5
                if hit:
                    used.add(bi)
                scored.setdefault(label, []).append((d.confidence, hit))
    for label in sorted(truth):
        if not truth[label]:
            continue
        print(f"{label} ({truth[label]} labelled)")
        for t in thresholds:
            sel = [hit for c, hit in scored.get(label, []) if c >= t]
            tp = sum(sel)
            print(f"  conf >= {t:.2f}: precision {tp / max(1, len(sel)):.2f}  recall {tp / truth[label]:.2f}")


if __name__ == "__main__":
    main()
