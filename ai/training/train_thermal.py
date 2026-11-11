"""Trains the thermal detector on HIT-UAV and exports it to ONNX.

    python -m venv venv && venv/bin/pip install ultralytics onnx onnxslim
    venv/bin/python train_thermal.py --work /tmp/hituav --device mps   # or cuda / cpu

Steps (each skipped if already done):
1. Pulls only the images and VOC labels out of the 812 MB release zip, using
   HTTP range requests (about 210 MB is downloaded).
2. Converts the labels to YOLO format: person, car, bicycle, other_vehicle
   ("DontCare" boxes are dropped).
3. Fine-tunes YOLO11n from COCO weights. Hue/saturation augmentation is off:
   thermal frames have no colour.
4. Validates on the held-out test split and exports thermal.onnx (640, opset 17).

HIT-UAV: https://github.com/suojiashun/HIT-UAV-Infrared-Thermal-Dataset (CC BY 4.0).
"""

from __future__ import annotations

import argparse
import io
import os
import shutil
import urllib.request
import xml.etree.ElementTree as ET
import zipfile

URL = "https://github.com/suojiashun/HIT-UAV-Infrared-Thermal-Dataset/releases/download/v1.2.1/HIT-UAV.zip"
CLASSES = ["person", "car", "bicycle", "other_vehicle"]
MAP = {"Person": 0, "Car": 1, "Bicycle": 2, "OtherVehicle": 3}


class RemoteFile(io.RawIOBase):
    """Seekable read-only view of a URL, so zipfile can read single members."""

    def __init__(self, url: str):
        r = urllib.request.urlopen(urllib.request.Request(url, method="HEAD"))
        self.url, self.size, self.pos = r.geturl(), int(r.headers["Content-Length"]), 0

    def seekable(self): return True
    def readable(self): return True
    def tell(self): return self.pos

    def seek(self, off, whence=0):
        self.pos = off if whence == 0 else self.pos + off if whence == 1 else self.size + off
        return self.pos

    def readinto(self, b):
        n = min(len(b), self.size - self.pos)
        if n <= 0:
            return 0
        req = urllib.request.Request(self.url, headers={"Range": f"bytes={self.pos}-{self.pos + n - 1}"})
        data = urllib.request.urlopen(req).read()
        b[:len(data)] = data
        self.pos += len(data)
        return len(data)


def download(raw: str) -> None:
    if os.path.isdir(os.path.join(raw, "Annotations")):
        return
    z = zipfile.ZipFile(io.BufferedReader(RemoteFile(URL), buffer_size=1 << 20))
    prefix = "HIT-UAV/normal_xml/"
    members = [i for i in z.infolist() if i.filename.startswith(prefix) and not i.is_dir() and ".DS_Store" not in i.filename]
    for n, i in enumerate(members, 1):
        dst = os.path.join(raw, i.filename[len(prefix):])
        os.makedirs(os.path.dirname(dst), exist_ok=True)
        with z.open(i) as src, open(dst, "wb") as f:
            shutil.copyfileobj(src, f)
        if n % 500 == 0:
            print(f"extracted {n}/{len(members)}")


def convert(raw: str, out: str) -> str:
    for split in ("train", "val", "test"):
        os.makedirs(f"{out}/images/{split}", exist_ok=True)
        os.makedirs(f"{out}/labels/{split}", exist_ok=True)
        for stem in (l.strip() for l in open(f"{raw}/ImageSets/Main/{split}.txt") if l.strip()):
            stem = os.path.splitext(stem)[0]
            r = ET.parse(f"{raw}/Annotations/{stem}.xml").getroot()
            W, H = float(r.find("size/width").text), float(r.find("size/height").text)
            lines = []
            for o in r.findall("object"):
                name = o.find("name").text
                if name not in MAP:
                    continue
                b = o.find("bndbox")
                x1, y1, x2, y2 = (float(b.find(k).text) for k in ("xmin", "ymin", "xmax", "ymax"))
                lines.append(f"{MAP[name]} {(x1 + x2) / 2 / W:.6f} {(y1 + y2) / 2 / H:.6f} {(x2 - x1) / W:.6f} {(y2 - y1) / H:.6f}")
            dst = f"{out}/images/{split}/{stem}.jpg"
            if not os.path.exists(dst):
                os.symlink(os.path.abspath(f"{raw}/JPEGImages/{stem}.jpg"), dst)
            with open(f"{out}/labels/{split}/{stem}.txt", "w") as f:
                f.write("\n".join(lines))
    data = os.path.abspath(f"{out}/data.yaml")
    with open(data, "w") as f:
        f.write(f"path: {os.path.abspath(out)}\ntrain: images/train\nval: images/val\ntest: images/test\n"
                f"names: {dict(enumerate(CLASSES))}\n")
    return data


def main() -> None:
    p = argparse.ArgumentParser()
    p.add_argument("--work", default="hituav-work")
    p.add_argument("--device", default="mps")
    p.add_argument("--epochs", type=int, default=25)
    p.add_argument("--weights", default="yolo11n.pt", help="starting weights (COCO, or a previous run to continue)")
    a = p.parse_args()

    from ultralytics import YOLO

    raw, ds = os.path.join(a.work, "raw"), os.path.join(a.work, "yolo")
    download(raw)
    data = convert(raw, ds)
    model = YOLO(a.weights)
    model.train(data=data, imgsz=640, epochs=a.epochs, patience=8, batch=16, device=a.device, workers=2,
                hsv_h=0.0, hsv_s=0.0, project=a.work, name="run", exist_ok=True, plots=False)
    best = YOLO(os.path.join(a.work, "run", "weights", "best.pt"))
    metrics = best.val(data=data, split="test", imgsz=640, device=a.device, plots=False)
    print(f"test mAP50={metrics.box.map50:.3f} mAP50-95={metrics.box.map:.3f}")
    for i, name in enumerate(CLASSES):
        print(f"  {name:14s} AP50={metrics.box.ap50[i]:.3f}")
    onnx = best.export(format="onnx", imgsz=640, opset=17, simplify=True, dynamic=False)
    shutil.copy(onnx, os.path.join(a.work, "thermal.onnx"))
    print("exported", os.path.join(a.work, "thermal.onnx"))


if __name__ == "__main__":
    main()
