"""HTTP / WebSocket proxy to the drone agent over Tailscale."""

from __future__ import annotations

import asyncio
import logging

import httpx
import websockets
from fastapi import HTTPException, Response, WebSocket, WebSocketDisconnect

logger = logging.getLogger(__name__)

# ---- Shared persistent HTTP client (connection pooling) ----
_client: httpx.AsyncClient | None = None


def _get_client() -> httpx.AsyncClient:
    """Return a long-lived AsyncClient with connection pooling."""
    global _client
    if _client is None or _client.is_closed:
        _client = httpx.AsyncClient(
            timeout=5.0,
            limits=httpx.Limits(
                max_connections=20,
                max_keepalive_connections=10,
                keepalive_expiry=30,
            ),
        )
    return _client


async def close_client() -> None:
    """Shutdown the shared client (call during app lifespan teardown)."""
    global _client
    if _client and not _client.is_closed:
        await _client.aclose()
        _client = None


async def proxy_get(agent_url: str, path: str, *, timeout: float = 5.0) -> Response:
    url = f"{agent_url.rstrip('/')}/{path.lstrip('/')}"
    try:
        resp = await _get_client().get(url, timeout=timeout)
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
        resp = await _get_client().request(method, url, content=body, headers=headers, timeout=timeout)
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
