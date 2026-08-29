"""Single-drone lightweight viewer — video + MAVLink telemetry."""

from __future__ import annotations

import logging
from contextlib import asynccontextmanager
from pathlib import Path
from typing import AsyncIterator

from fastapi import FastAPI, HTTPException, Query, Request, WebSocket
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import FileResponse, JSONResponse
from fastapi.staticfiles import StaticFiles

from config import ViewerSettings, build_drone_urls, build_stream_sources
from proxy import close_client, proxy_get, proxy_request, proxy_websocket
from recorder import RecordingManager, discover_streams

logger = logging.getLogger(__name__)
STATIC = Path(__file__).resolve().parent / "static"


def create_app(settings: ViewerSettings | None = None) -> FastAPI:
    cfg = settings or ViewerSettings()
    recorder = RecordingManager(cfg.recordings_dir)

    @asynccontextmanager
    async def lifespan(_app: FastAPI) -> AsyncIterator[None]:
        logging.basicConfig(level=logging.INFO)
        logger.info(
            "Drone View — viewer :%s (drone host/name supplied via URL query params)",
            cfg.viewer_port,
        )
        yield
        await recorder.cleanup_all()
        await close_client()

    app = FastAPI(title="Drone View", version="1.0.0", lifespan=lifespan)
    app.state.settings = cfg
    app.state.recorder = recorder

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

    @app.get("/api/go2rtc/streams")
    async def go2rtc_streams(src: str | None = Query(None, description="Optional stream name")):
        clean_base = cfg.go2rtc_url.strip().strip("'\"").rstrip("/")
        path = f"/api/streams?src={src}" if src else "/api/streams"
        return await proxy_get(clean_base, path)

    @app.get("/api/cameras")
    async def cameras(
        name: str | None = Query(None, description="Drone name prefix (matches '{name}_*' streams)"),
        host: str | None = Query(None, description="Drone host; used as name when name is not supplied"),
    ):
        """Enumerate all go2rtc cameras whose stream name starts with '{name}_'.

        Called once at dashboard page load. Falls back to the host value
        when name is not supplied.
        """
        resolved = (name or host or "").strip().strip("'\"")
        if not resolved:
            raise HTTPException(status_code=400, detail="Provide ?name= or ?host=")
        found = await discover_streams(cfg.go2rtc_url, f"{resolved}_")
        return {"name": resolved, "cameras": found}

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

    # ---- Server-side Recording Endpoints ----

    @app.post("/api/recording/start")
    async def start_recording(
        name: str = Query(..., description="Drone name prefix for stream ingestion"),
    ):
        clean_name = name.strip().strip("'\"")
        # Discover cameras now so streams registered after page load are captured;
        # fall back to the conventional pair when go2rtc can't be reached.
        stream_names = await discover_streams(cfg.go2rtc_url, f"{clean_name}_")
        if not stream_names:
            stream_names = [f"{clean_name}_camera", f"{clean_name}_thermal"]
        stream_urls = build_stream_sources(cfg, stream_names)
        try:
            result = await recorder.start_recording(
                drone_name=clean_name,
                stream_urls=stream_urls,
                go2rtc_url=cfg.go2rtc_url,
            )
            return result
        except RuntimeError as exc:
            raise HTTPException(status_code=500, detail=str(exc)) from exc
        except ValueError as exc:
            raise HTTPException(status_code=400, detail=str(exc)) from exc

    @app.post("/api/recording/stop")
    async def stop_recording(
        name: str = Query(..., description="Drone name prefix"),
    ):
        return await recorder.stop_recording(name)

    @app.get("/api/recording/status")
    async def recording_status(
        name: str | None = Query(None, description="Optional drone name prefix"),
    ):
        return recorder.get_status(name)


    @app.get("/api/recordings")
    async def list_recordings(
        name: str | None = Query(None, description="Filter recordings by drone name"),
        sort_by: str = Query("time", description="Sort by 'time', 'duration', 'size', or 'name'"),
        order: str = Query("desc", description="Order 'asc' or 'desc'"),
    ):
        return await recorder.list_recordings(drone_name=name, sort_by=sort_by, order=order)

    @app.api_route("/api/recordings/download/{filename:path}", methods=["GET", "HEAD"])
    async def download_recording(filename: str):
        filepath = recorder.get_recording_path(filename)
        if not filepath:
            raise HTTPException(status_code=404, detail="Recording file not found")
        media_type = "video/x-matroska" if filename.endswith(".mkv") else "video/mp4"
        return FileResponse(
            path=filepath,
            filename=filepath.name,
            media_type=media_type,
        )

    @app.delete("/api/recordings/{filename:path}")
    async def delete_recording(filename: str):
        try:
            success = await recorder.delete_recording(filename)
            if not success:
                raise HTTPException(status_code=404, detail="Recording file not found")
            return {"deleted": True, "filename": filename}
        except RuntimeError as exc:
            raise HTTPException(status_code=400, detail=str(exc)) from exc

    @app.get("/")
    async def index():
        return FileResponse(STATIC / "index.html")

    app.mount("/static", StaticFiles(directory=STATIC), name="static")
    return app

