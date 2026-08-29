"""Viewer configuration from .env"""

from __future__ import annotations

from pathlib import Path
from typing import Any

from pydantic import AliasChoices, Field, field_validator
from pydantic_settings import BaseSettings, SettingsConfigDict

from urllib.parse import quote, urlparse

ROOT = Path(__file__).resolve().parent


class ViewerSettings(BaseSettings):
    model_config = SettingsConfigDict(
        env_file=ROOT / ".env",
        env_file_encoding="utf-8",
        extra="ignore",
    )

    agent_port: int = 8080
    go2rtc_url: str = "https://cams.drone.starwatch.bb7fab11.sslip.io"
    go2rtc_rtsp_url: str = ""
    go2rtc_ingest_mode: str = "rtsp"
    recording_path: Path = Field(
        default_factory=lambda: ROOT / "recording",
        validation_alias="RECORDING_PATH",
    )



    viewer_host: str = "0.0.0.0"
    viewer_port: int = 8091

    def __init__(self, **data: Any):
        if "recordings_dir" in data and "recording_path" not in data:
            data["recording_path"] = data.pop("recordings_dir")
        super().__init__(**data)



    @field_validator("recording_path", mode="before")
    @classmethod
    def clean_recording_path(cls, v: str | Path | None) -> Path:
        if v is None or v == "":
            return ROOT / "recording"
        p = Path(str(v).strip().strip("'\""))
        if not p.is_absolute():
            return (ROOT / p).resolve()
        return p.resolve()

    @property
    def recordings_dir(self) -> Path:
        """Alias property for recording_path."""
        return self.recording_path

    @field_validator("go2rtc_url", "go2rtc_rtsp_url", mode="before")
    @classmethod
    def clean_urls(cls, v: str) -> str:
        if isinstance(v, str):
            return v.strip().strip("'\"").rstrip("/")
        return v


    @field_validator("go2rtc_ingest_mode", mode="before")
    @classmethod
    def clean_ingest_mode(cls, v: str) -> str:
        mode = str(v or "rtsp").strip().lower()
        if mode not in ("rtsp", "mp4", "hls"):
            raise ValueError("go2rtc_ingest_mode must be one of: rtsp, mp4, hls")
        return mode

    def get_rtsp_base_url(self) -> str:
        """Return base RTSP URL (e.g. rtsp://host:8554) for stream ingestion."""
        if self.go2rtc_rtsp_url:
            return self.go2rtc_rtsp_url.rstrip("/")
        parsed = urlparse(self.go2rtc_url)
        host = parsed.hostname or "127.0.0.1"
        return f"rtsp://{host}:8554"

    def public_config(self) -> dict:
        """Return non-sensitive, drone-agnostic settings for the frontend."""
        return {
            "agent_port": self.agent_port,
            "go2rtc_url": self.go2rtc_url.strip().strip("'\"").rstrip("/"),
            "go2rtc_rtsp_url": self.get_rtsp_base_url(),
        }


def build_drone_urls(host: str, cfg: ViewerSettings) -> dict:
    """Build per-request upstream URLs from a drone host IP and shared config."""
    clean_host = host.strip().strip("'\"").rstrip("/")
    return {
        "agent_url": f"http://{clean_host}:{cfg.agent_port}",
        "mavlink_ws_url": f"ws://{clean_host}:{cfg.agent_port}/api/mavlink/ws",
    }


def build_rtsp_urls(drone_name: str, cfg: ViewerSettings, stream_names: list[str] | None = None) -> dict[str, str]:
    """Build RTSP stream URLs for a drone, keyed by full stream name.

    When stream_names is omitted, falls back to the conventional
    {name}_camera / {name}_thermal pair.
    """
    clean_name = drone_name.strip().strip("'\"")
    rtsp_base = cfg.get_rtsp_base_url()
    if stream_names is None:
        stream_names = [f"{clean_name}_camera", f"{clean_name}_thermal"]
    return {name: f"{rtsp_base}/{name}" for name in stream_names}


def build_stream_sources(cfg: ViewerSettings, stream_names: list[str]) -> dict[str, str]:
    """Build recorder ingest URLs (name → URL) per the configured ingest mode.

    - rtsp: rtsp://host:8554/{name}          (default; needs the RTSP port reachable)
    - mp4:  {go2rtc_url}/api/stream.mp4      (fragmented MP4 over HTTP(S) — same path the browser uses)
    - hls:  {go2rtc_url}/api/stream.m3u8     (HLS over HTTP(S); higher latency, fine for recording)
    """
    mode = cfg.go2rtc_ingest_mode
    if mode == "rtsp":
        rtsp_base = cfg.get_rtsp_base_url()
        return {name: f"{rtsp_base}/{name}" for name in stream_names}
    base = cfg.go2rtc_url.strip().strip("'\"").rstrip("/")
    kind = "mp4" if mode == "mp4" else "m3u8"
    return {name: f"{base}/api/stream.{kind}?src={quote(name, safe='')}" for name in stream_names}

