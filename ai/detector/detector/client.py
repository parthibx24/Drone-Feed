"""Talks to the gateway's detector endpoints (docs/09-ai.md)."""

from __future__ import annotations

import json

import requests


class GatewayClient:
    def __init__(self, base_url: str, token: str, timeout: float = 5.0):
        self.base = base_url.rstrip("/")
        self.timeout = timeout
        self.http = requests.Session()
        self.http.headers["Authorization"] = f"Bearer {token}"

    def targets(self) -> dict:
        r = self.http.get(f"{self.base}/api/v1/ai/targets", timeout=self.timeout)
        r.raise_for_status()
        return r.json()

    def detections(self, frame: dict) -> None:
        r = self.http.post(f"{self.base}/api/v1/ai/detections", json=frame, timeout=self.timeout)
        r.raise_for_status()

    def event(self, event: dict, snapshot_jpeg: bytes | None) -> str:
        files = {"event": (None, json.dumps(event), "application/json")}
        if snapshot_jpeg:
            files["snapshot"] = ("snapshot.jpg", snapshot_jpeg, "image/jpeg")
        r = self.http.post(f"{self.base}/api/v1/ai/events", files=files, timeout=max(self.timeout, 15))
        r.raise_for_status()
        return r.json().get("id", "")
