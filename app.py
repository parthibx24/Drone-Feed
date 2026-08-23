"""Single-drone lightweight viewer — video + MAVLink telemetry."""

from __future__ import annotations

import logging
from contextlib import asynccontextmanager
from pathlib import Path
from typing import AsyncIterator

from fastapi import FastAPI, Query, Request, WebSocket
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import FileResponse
from fastapi.staticfiles import StaticFiles

from config import ViewerSettings, build_drone_urls
from proxy import close_client, proxy_get, proxy_request, proxy_websocket

logger = logging.getLogger(__name__)
STATIC = Path(__file__).resolve().parent / "static"


def create_app(settings: ViewerSettings | None = None) -> FastAPI:
    cfg = settings or ViewerSettings()

    @asynccontextmanager
    async def lifespan(_app: FastAPI) -> AsyncIterator[None]:
        logging.basicConfig(level=logging.INFO)
        logger.info(
            "Drone View — viewer :%s (drone host/name supplied via URL query params)",
            cfg.viewer_port,
        )
        yield
        await close_client()

    app = FastAPI(title="Drone View", version="1.0.0", lifespan=lifespan)
    app.state.settings = cfg

    app.add_middleware(
        CORSMiddleware,
        allow_origins=["*"],
        allow_credentials=True,
        allow_methods=["*"],
        allow_headers=["*"],
    )

    @app.get("/api/config")
    async def get_config() -> dict:
        return cfg.public_config()

    @app.get("/api/health")
    async def health(host: str = Query(..., description="Drone Tailscale IP")):
        urls = build_drone_urls(host, cfg)
        return await proxy_get(urls["agent_url"], "/api/health")

    @app.get("/api/telemetry")
    async def telemetry(host: str = Query(..., description="Drone Tailscale IP")):
        urls = build_drone_urls(host, cfg)
        return await proxy_get(urls["agent_url"], "/api/telemetry")

    @app.api_route("/api/gimbal/{path:path}", methods=["GET", "POST"])
    async def gimbal(
        path: str,
        request: Request,
        host: str = Query(..., description="Drone Tailscale IP"),
    ):
        urls = build_drone_urls(host, cfg)
        body = await request.body()
        return await proxy_request(
            urls["agent_url"],
            f"/api/gimbal/{path}",
            request.method,
            body=body or None,
            content_type=request.headers.get("content-type"),
        )

    @app.websocket("/ws/mavlink")
    async def ws_mavlink(
        websocket: WebSocket,
        host: str = Query(..., description="Drone Tailscale IP"),
    ):
        urls = build_drone_urls(host, cfg)
        await proxy_websocket(websocket, urls["mavlink_ws_url"])

    @app.get("/")
    async def index():
        return FileResponse(STATIC / "index.html")

    app.mount("/static", StaticFiles(directory=STATIC), name="static")
    return app
