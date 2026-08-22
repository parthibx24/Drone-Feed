"""Run: python -m Drone_View   (from Star Technologies/) or  python __main__.py  (from Drone_View/)"""

from __future__ import annotations

import sys
from pathlib import Path

import uvicorn

ROOT = Path(__file__).resolve().parent
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))


def main() -> None:
    from config import ViewerSettings
    from app import create_app

    cfg = ViewerSettings()
    app = create_app(cfg)
    uvicorn.run(app, host=cfg.viewer_host, port=cfg.viewer_port, log_level="info")


if __name__ == "__main__":
    main()
