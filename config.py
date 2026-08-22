"""Viewer configuration from .env"""

from __future__ import annotations

from pathlib import Path

from pydantic import computed_field
from pydantic_settings import BaseSettings, SettingsConfigDict

ROOT = Path(__file__).resolve().parent


class ViewerSettings(BaseSettings):
    model_config = SettingsConfigDict(
        env_file=ROOT / ".env",
        env_file_encoding="utf-8",
        extra="ignore",
    )

    drone_host: str
    drone_name: str = "Drone"

    agent_port: int = 8080
    go2rtc_port: int = 1984
    stream: str = "skydroid_camera"
    thermal_stream: str = "skydroid_thermal"

    viewer_host: str = "0.0.0.0"
    viewer_port: int = 8091

    @computed_field  # type: ignore[prop-decorator]
    @property
    def agent_url(self) -> str:
        return f"http://{self.drone_host}:{self.agent_port}"

    @computed_field  # type: ignore[prop-decorator]
    @property
    def go2rtc_url(self) -> str:
        return f"http://{self.drone_host}:{self.go2rtc_port}"

    @computed_field  # type: ignore[prop-decorator]
    @property
    def mavlink_ws_url(self) -> str:
        return f"ws://{self.drone_host}:{self.agent_port}/api/mavlink/ws"

    def public_config(self) -> dict:
        return {
            "name": self.drone_name,
            "host": self.drone_host,
            "go2rtc_url": self.go2rtc_url,
            "stream": self.stream,
            "thermal_stream": self.thermal_stream,
        }
