"""Viewer configuration from .env"""

from __future__ import annotations

from pathlib import Path

from pydantic import AliasChoices, Field, field_validator
from pydantic_settings import BaseSettings, SettingsConfigDict

from urllib.parse import urlparse

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


def build_rtsp_urls(drone_name: str, cfg: ViewerSettings) -> dict[str, str]:
    """Build RTSP stream URLs for both visible and thermal feeds of a drone."""
    clean_name = drone_name.strip().strip("'\"")
    rtsp_base = cfg.get_rtsp_base_url()
    return {
        "visible_rtsp_url": f"{rtsp_base}/{clean_name}_camera",
        "thermal_rtsp_url": f"{rtsp_base}/{clean_name}_thermal",
    }

