"""HTTP / WebSocket proxy to the drone agent over Tailscale."""

from __future__ import annotations

import asyncio
import logging

import httpx
import websockets
from fastapi import HTTPException, Response, WebSocket, WebSocketDisconnect

logger = logging.getLogger(__name__)


async def proxy_get(agent_url: str, path: str, *, timeout: float = 5.0) -> Response:
    url = f"{agent_url.rstrip('/')}/{path.lstrip('/')}"
    try:
        async with httpx.AsyncClient(timeout=timeout) as client:
            resp = await client.get(url)
    except httpx.RequestError as exc:
        logger.warning("Proxy GET %s failed: %s", url, exc)
        raise HTTPException(status_code=502, detail=f"Drone unreachable: {exc}") from exc
    return Response(
        content=resp.content,
        status_code=resp.status_code,
        media_type=resp.headers.get("content-type", "application/json"),
    )


async def proxy_request(
    agent_url: str,
    path: str,
    method: str,
    body: bytes | None = None,
    content_type: str | None = None,
    *,
    timeout: float = 5.0,
) -> Response:
    """Forward an arbitrary method + body to the drone agent (used for gimbal control)."""
    url = f"{agent_url.rstrip('/')}/{path.lstrip('/')}"
    headers = {"Content-Type": content_type} if content_type else None
    try:
        async with httpx.AsyncClient(timeout=timeout) as client:
            resp = await client.request(method, url, content=body, headers=headers)
    except httpx.RequestError as exc:
        logger.warning("Proxy %s %s failed: %s", method, url, exc)
        raise HTTPException(status_code=502, detail=f"Drone unreachable: {exc}") from exc
    return Response(
        content=resp.content,
        status_code=resp.status_code,
        media_type=resp.headers.get("content-type", "application/json"),
    )


async def proxy_websocket(client_ws: WebSocket, upstream_url: str) -> None:
    await client_ws.accept()
    try:
        async with websockets.connect(upstream_url, open_timeout=5) as upstream:
            async def upstream_to_client() -> None:
                async for message in upstream:
                    if isinstance(message, bytes):
                        await client_ws.send_bytes(message)
                    else:
                        await client_ws.send_text(message)

            relay = asyncio.create_task(upstream_to_client())
            try:
                while True:
                    msg = await client_ws.receive()
                    if msg["type"] == "websocket.disconnect":
                        break
            except WebSocketDisconnect:
                pass
            finally:
                relay.cancel()
                try:
                    await relay
                except asyncio.CancelledError:
                    pass
    except Exception as exc:
        logger.warning("WebSocket proxy failed: %s", exc)
        try:
            await client_ws.close(code=1011, reason=str(exc))
        except Exception:
            pass
