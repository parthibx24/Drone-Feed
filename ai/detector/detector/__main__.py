"""StarWatch detector service.

Asks the gateway which camera streams to process, runs the visible or thermal
model on each a few times a second, and reports boxes and new objects back.

Configuration (environment):
  AI_INGEST_TOKEN   required; same value as ai.ingest_token in gateway.yaml
  GATEWAY_URL       default http://127.0.0.1:8091
  SOURCE_TEMPLATE   default rtsp://127.0.0.1:8554/{stream}
  MODEL_VISIBLE     default /app/models/visible.onnx
  MODEL_THERMAL     default /app/models/thermal.onnx
  THREADS           ONNX threads per model, default 1
  POLL_S            how often to ask for targets, default 2
  MIN_HITS          frames an object must appear in before it is an event, default 3
  TILES             visible camera only: 1 (default), 2 (halves) or 4 (quadrants); finds
                    smaller objects (people far below) at about 3x or 5x the CPU
  LOG_LEVEL         default INFO

Offline use (no gateway): python -m detector file <image|video> --kind visible|thermal --out <path>
"""

from __future__ import annotations

import argparse
import logging
import os
import signal
import sys
import threading
import time

# Must be set before OpenCV opens any stream: RTSP over TCP, 5 s socket timeout.
os.environ.setdefault("OPENCV_FFMPEG_CAPTURE_OPTIONS", "rtsp_transport;tcp|stimeout;5000000|timeout;5000000")

from .client import GatewayClient  # noqa: E402
from .model import YoloOnnx  # noqa: E402
from .worker import StreamWorker  # noqa: E402

log = logging.getLogger("detector")


def load_models(threads: int) -> dict[str, YoloOnnx]:
    models = {}
    for kind, env, default in (("visible", "MODEL_VISIBLE", "/app/models/visible.onnx"),
                               ("thermal", "MODEL_THERMAL", "/app/models/thermal.onnx")):
        path = os.environ.get(env, default)
        if os.path.exists(path):
            models[kind] = YoloOnnx(path, threads=threads, name=f"{kind}:{os.path.basename(path)}")
            log.info("loaded %s model %s: %d classes (%s)", kind, path, len(models[kind].names),
                     ", ".join(models[kind].names))
        else:
            log.warning("no %s model at %s", kind, path)
    if not models:
        sys.exit("no models found; set MODEL_VISIBLE / MODEL_THERMAL")
    return models


def serve() -> None:
    token = os.environ.get("AI_INGEST_TOKEN", "")
    if len(token) < 16:
        sys.exit("AI_INGEST_TOKEN must be set (at least 16 characters, same as ai.ingest_token)")
    client = GatewayClient(os.environ.get("GATEWAY_URL", "http://127.0.0.1:8091"), token)
    template = os.environ.get("SOURCE_TEMPLATE", "rtsp://127.0.0.1:8554/{stream}")
    models = load_models(int(os.environ.get("THREADS", "1")))
    poll = float(os.environ.get("POLL_S", "2"))
    settings = {"fps": 3, "min_confidence": 0.35, "min_hits": int(os.environ.get("MIN_HITS", "3")),
                "tiles": int(os.environ.get("TILES", "1"))}

    workers: dict[tuple[str, str], StreamWorker] = {}
    stopping = threading.Event()
    signal.signal(signal.SIGTERM, lambda *_: stopping.set())
    signal.signal(signal.SIGINT, lambda *_: stopping.set())
    last_report = time.monotonic()
    failures = 0

    while not stopping.is_set():
        try:
            resp = client.targets()
            failures = 0
        except Exception as e:
            failures += 1
            if failures in (1, 10) or failures % 60 == 0:
                log.warning("gateway not reachable (%s); keeping %d workers", e, len(workers))
            stopping.wait(poll)
            continue
        settings["fps"] = float(resp.get("fps", 3))
        settings["min_confidence"] = float(resp.get("min_confidence", 0.35))
        wanted = {}
        for t in resp.get("targets", []):
            kind = t.get("kind", "visible")
            model = models.get(kind) or models.get("visible") or next(iter(models.values()))
            wanted[(t["drone_id"], t["stream"])] = (kind, model)

        for key in list(workers):
            if key not in wanted:
                log.info("stopping %s/%s (no longer needed)", *key)
                workers.pop(key).stop()
        for key, (kind, model) in wanted.items():
            w = workers.get(key)
            if w is None or not w.is_alive():
                url = template.format(stream=key[1], drone=key[0])
                log.info("starting %s/%s (%s) from %s", key[0], key[1], kind, url)
                w = StreamWorker(key[0], key[1], kind, url, model, client, settings)
                workers[key] = w
                w.start()

        if time.monotonic() - last_report > 60 and workers:
            last_report = time.monotonic()
            for (d, s), w in workers.items():
                log.info("%s/%s: connected=%s frames=%d infer=%.0fms events=%d", d, s, w.reader.connected,
                         w.stats["frames"], w.stats["infer_ms"], w.stats["events"])
        stopping.wait(poll)

    for w in workers.values():
        w.stop()
    log.info("stopped")


def offline(args: argparse.Namespace) -> None:
    """Runs a model over an image or video and writes an annotated copy."""
    import cv2

    from .tracker import Tracker
    from .worker import GROUP_COLOURS

    model = YoloOnnx(args.model or os.environ.get(
        "MODEL_THERMAL" if args.kind == "thermal" else "MODEL_VISIBLE",
        f"/app/models/{args.kind}.onnx"), threads=int(os.environ.get("THREADS", "2")))

    def draw(img, pairs):
        for t, d in pairs:
            x, y, w, h = d.box
            H, W = img.shape[:2]
            c = GROUP_COLOURS.get(d.group, (200, 200, 200))
            cv2.rectangle(img, (int(x * W), int(y * H)), (int((x + w) * W), int((y + h) * H)), c, 2)
            cv2.putText(img, f"{d.label} {d.confidence:.2f}", (int(x * W), max(12, int(y * H) - 4)),
                        cv2.FONT_HERSHEY_SIMPLEX, 0.45, c, 1, cv2.LINE_AA)

    img = cv2.imread(args.input)
    if img is not None:
        dets, ms = model.detect_tiled(img, args.tiles, conf=args.conf)
        draw(img, [(None, d) for d in dets])
        cv2.imwrite(args.out, img)
        counts: dict[str, int] = {}
        for d in dets:
            counts[d.label] = counts.get(d.label, 0) + 1
        print(f"{len(dets)} objects in {ms:.0f} ms: {counts} -> {args.out}")
        return

    cap = cv2.VideoCapture(args.input)
    fps_in = cap.get(cv2.CAP_PROP_FPS) or 25
    step = max(1, round(fps_in / args.fps))
    tracker, writer, n, events = Tracker(), None, 0, 0
    total_ms = 0.0
    while True:
        ok, frame = cap.read()
        if not ok:
            break
        n += 1
        if n % step:
            continue
        dets, ms = model.detect_tiled(frame, args.tiles, conf=args.conf)
        total_ms += ms
        pairs, confirmed = tracker.update(dets, n / fps_in)
        events += len(confirmed)
        draw(frame, pairs)
        if writer is None:
            h, w = frame.shape[:2]
            writer = cv2.VideoWriter(args.out, cv2.VideoWriter_fourcc(*"mp4v"), args.fps, (w, h))
        writer.write(frame)
    if writer:
        writer.release()
    processed = n // step
    print(f"{processed} frames processed, {events} objects confirmed, "
          f"{total_ms / max(1, processed):.0f} ms per frame -> {args.out}")


def main() -> None:
    logging.basicConfig(level=os.environ.get("LOG_LEVEL", "INFO"),
                        format="%(asctime)s %(levelname)s %(name)s: %(message)s")
    p = argparse.ArgumentParser(prog="detector")
    sub = p.add_subparsers(dest="cmd")
    f = sub.add_parser("file", help="annotate an image or video offline")
    f.add_argument("input")
    f.add_argument("--kind", choices=["visible", "thermal"], default="visible")
    f.add_argument("--model")
    f.add_argument("--out", default="annotated.jpg")
    f.add_argument("--conf", type=float, default=0.35)
    f.add_argument("--fps", type=float, default=3)
    f.add_argument("--tiles", type=int, default=1, choices=[1, 2, 4])
    args = p.parse_args()
    if args.cmd == "file":
        offline(args)
    else:
        serve()


if __name__ == "__main__":
    main()
