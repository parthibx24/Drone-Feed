"""Viewer configuration from .env"""

from __future__ import annotations

from pathlib import Path

from pydantic_settings import BaseSettings, SettingsConfigDict

ROOT = Path(__file__).resolve().parent


class ViewerSettings(BaseSettings):
    model_config = SettingsConfigDict(
        env_file=ROOT / ".env",
        env_file_encoding="utf-8",
        extra="ignore",
    )

    agent_port: int = 8080
    go2rtc_url: str = "https://cams.drone.starwatch.bb7fab11.sslip.io"

    viewer_host: str = "0.0.0.0"
    viewer_port: int = 8091

    def public_config(self) -> dict:
        """Return non-sensitive, drone-agnostic settings for the frontend."""
        return {
            "agent_port": self.agent_port,
            "go2rtc_url": self.go2rtc_url.rstrip("/"),
        }


def build_drone_urls(host: str, cfg: ViewerSettings) -> dict:
    """Build per-request upstream URLs from a drone host IP and shared config."""
    return {
        "agent_url": f"http://{host}:{cfg.agent_port}",
        "mavlink_ws_url": f"ws://{host}:{cfg.agent_port}/api/mavlink/ws",
    }
