"""Server-side zero-encoding FFmpeg dual-stream recording manager with stream probing."""

from __future__ import annotations

import asyncio
import json
import logging
import os
import re
import shutil
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

import httpx

logger = logging.getLogger(__name__)


def format_bytes(size: int) -> str:
    """Format bytes into human-readable string."""
    if size < 1024:
        return f"{size} B"
    elif size < 1024 * 1024:
        return f"{size / 1024:.1f} KB"
    elif size < 1024 * 1024 * 1024:
        return f"{size / (1024 * 1024):.1f} MB"
    else:
        return f"{size / (1024 * 1024 * 1024):.2f} GB"


def format_duration(seconds: float) -> str:
    """Format seconds into HH:MM:SS string."""
    total = int(max(0, seconds))
    hrs = total // 3600
    mins = (total % 3600) // 60
    secs = total % 60
    if hrs > 0:
        return f"{hrs:02d}:{mins:02d}:{secs:02d}"
    return f"{mins:02d}:{secs:02d}"


def parse_fps(rate_str: str | None) -> float | None:
    """Parse FFprobe fractional fps string (e.g. '30/1' or '29.97')."""
    if not rate_str or rate_str == "0/0":
        return None
    try:
        if "/" in rate_str:
            num, den = rate_str.split("/", 1)
            den_val = float(den)
            return round(float(num) / den_val, 2) if den_val != 0 else None
        return round(float(rate_str), 2)
    except Exception:
        return None


class ActiveRecording:
    def __init__(
        self,
        drone_name: str,
        filename: str,
        filepath: Path,
        visible_rtsp_url: str,
        thermal_rtsp_url: str,
        go2rtc_url: str = "",
    ):
        self.drone_name = drone_name
        self.filename = filename
        self.filepath = filepath
        self.visible_rtsp_url = visible_rtsp_url
        self.thermal_rtsp_url = thermal_rtsp_url
        self.go2rtc_url = go2rtc_url

        self.state: str = "waiting"  # "waiting" | "recording" | "stopping" | "failed"
        self.init_time = time.time()
        self.start_time: float | None = None
        self.error_message: str | None = None

        self.process: asyncio.subprocess.Process | None = None
        self.stderr_lines: list[str] = []
        self._reader_task: asyncio.Task | None = None
        self._wait_task: asyncio.Task | None = None

    def start_reader(self) -> None:
        async def _read_stderr():
            if not self.process or not self.process.stderr:
                return
            try:
                while True:
                    line = await self.process.stderr.readline()
                    if not line:
                        break
                    decoded = line.decode("utf-8", errors="replace").strip()
                    if decoded:
                        self.stderr_lines.append(decoded)
                        if len(self.stderr_lines) > 50:
                            self.stderr_lines.pop(0)
            except Exception as exc:
                logger.debug("Stderr reader finished: %s", exc)

        self._reader_task = asyncio.create_task(_read_stderr())

    def stop_reader(self) -> None:
        if self._reader_task and not self._reader_task.done():
            self._reader_task.cancel()

    def cancel_wait_task(self) -> None:
        if self._wait_task and not self._wait_task.done():
            self._wait_task.cancel()


class RecordingManager:
    def __init__(self, recordings_dir: Path):
        self.recordings_dir = Path(recordings_dir).resolve()
        self.ensure_dir()
        self._active: dict[str, ActiveRecording] = {}
        self._probe_cache: dict[str, tuple[float, dict[str, Any]]] = {}

    def ensure_dir(self) -> None:
        self.recordings_dir.mkdir(parents=True, exist_ok=True)

    def is_ffmpeg_available(self) -> bool:
        return shutil.which("ffmpeg") is not None

    def is_ffprobe_available(self) -> bool:
        return shutil.which("ffprobe") is not None

    async def probe_stream_active(self, go2rtc_url: str, stream_name: str, rtsp_url: str) -> bool:
        """Check if go2rtc has active producers for stream, or probe via ffprobe/socket."""
        # 1. First probe go2rtc REST API if URL provided
        if go2rtc_url:
            clean_base = go2rtc_url.strip().strip("'\"").rstrip("/")
            try:
                async with httpx.AsyncClient(timeout=2.0) as client:
                    resp = await client.get(f"{clean_base}/api/streams?src={stream_name}")
                    if resp.status_code == 200:
                        data = resp.json()
                        # If single stream returned
                        if isinstance(data, dict):
                            stream_data = data.get(stream_name) if stream_name in data else data
                            producers = stream_data.get("producers", [])
                            if producers:
                                return True
            except Exception as exc:
                logger.debug("go2rtc API probe for '%s' failed: %s", stream_name, exc)

        # 2. Fallback: lightweight RTSP probe via ffprobe with short timeout
        if self.is_ffprobe_available():
            try:
                cmd = [
                    "ffprobe",
                    "-v", "error",
                    "-rtsp_transport", "tcp",
                    "-show_entries", "stream=codec_type",
                    "-of", "default=noprint_wrappers=1:nokey=1",
                    "-timeout", "1500000",  # 1.5s in microseconds
                    rtsp_url,
                ]
                proc = await asyncio.create_subprocess_exec(
                    *cmd,
                    stdout=asyncio.subprocess.PIPE,
                    stderr=asyncio.subprocess.PIPE,
                )
                stdout, _ = await asyncio.wait_for(proc.communicate(), timeout=3.0)
                if proc.returncode == 0 and b"video" in stdout:
                    return True
            except Exception as exc:
                logger.debug("ffprobe RTSP probe for '%s' failed: %s", stream_name, exc)

        return False

    async def start_recording(
        self,
        drone_name: str,
        visible_rtsp_url: str,
        thermal_rtsp_url: str,
        go2rtc_url: str = "",
        wait_timeout: float = 45.0,
    ) -> dict[str, Any]:
        """Initiate recording by entering a waiting state until camera feeds are active."""
        self.ensure_dir()
        clean_name = drone_name.strip().strip("'\"")
        if not clean_name:
            raise ValueError("Drone name cannot be empty")

        # Check if already active
        if clean_name in self._active:
            active = self._active[clean_name]
            if active.state == "waiting":
                return {
                    "status": "already_waiting",
                    "state": "waiting",
                    "name": clean_name,
                    "drone_name": clean_name,
                    "message": "Already waiting for active camera stream",
                    "elapsed_waiting_sec": round(time.time() - active.init_time, 1),
                }
            elif active.state == "recording" and active.process and active.process.returncode is None:
                return {
                    "status": "already_recording",
                    "state": "recording",
                    "name": clean_name,
                    "drone_name": clean_name,
                    "filename": active.filename,
                    "start_time": active.start_time,
                    "duration_sec": round(time.time() - (active.start_time or active.init_time), 1),
                }
            else:
                # Clean up finished/failed instance
                active.stop_reader()
                active.cancel_wait_task()
                del self._active[clean_name]

        if not self.is_ffmpeg_available():
            raise RuntimeError("ffmpeg binary not found on system PATH")

        timestamp_str = datetime.now(timezone.utc).strftime("%Y%m%d_%H%M%S")
        safe_prefix = re.sub(r"[^\w\-]", "_", clean_name)
        drone_dir = self.recordings_dir / safe_prefix
        drone_dir.mkdir(parents=True, exist_ok=True)
        filename = f"{safe_prefix}_{timestamp_str}.mkv"
        filepath = drone_dir / filename

        rec = ActiveRecording(
            drone_name=clean_name,
            filename=filename,
            filepath=filepath,
            visible_rtsp_url=visible_rtsp_url,
            thermal_rtsp_url=thermal_rtsp_url,
            go2rtc_url=go2rtc_url,
        )
        self._active[clean_name] = rec

        # Launch background waiting and capture task
        rec._wait_task = asyncio.create_task(
            self._wait_and_start_capture(rec, wait_timeout=wait_timeout)
        )

        return {
            "status": "waiting_for_stream",
            "state": "waiting",
            "name": clean_name,
            "drone_name": clean_name,
            "filename": filename,
            "message": "Waiting for active camera feed from go2rtc…",
            "init_time": rec.init_time,
            "created_at": datetime.fromtimestamp(rec.init_time, tz=timezone.utc).isoformat(),
        }

    async def _wait_and_start_capture(self, rec: ActiveRecording, wait_timeout: float = 45.0) -> None:
        """Polls go2rtc until camera streams are ready, then spawns FFmpeg."""
        drone_name = rec.drone_name
        vis_stream = f"{drone_name}_camera"
        th_stream = f"{drone_name}_thermal"

        logger.info("Drone '%s': waiting for active camera feeds...", drone_name)
        start_wait = time.time()

        # Probing loop
        while time.time() - start_wait < wait_timeout:
            try:
                # Probe visible stream
                vis_ok = await self.probe_stream_active(rec.go2rtc_url, vis_stream, rec.visible_rtsp_url)
                # Probe thermal stream
                th_ok = await self.probe_stream_active(rec.go2rtc_url, th_stream, rec.thermal_rtsp_url)

                if vis_ok or th_ok:
                    logger.info("Drone '%s': active camera feeds detected (vis: %s, th: %s). Starting FFmpeg...", drone_name, vis_ok, th_ok)
                    break
            except Exception as exc:
                logger.debug("Probe error for '%s': %s", drone_name, exc)

            await asyncio.sleep(1.0)

        # Build FFmpeg command for dual-stream copy
        cmd = [
            "ffmpeg",
            "-y",
            "-hide_banner",
            "-loglevel", "warning",
            "-rtsp_transport", "tcp",
            "-i", rec.visible_rtsp_url,
            "-rtsp_transport", "tcp",
            "-i", rec.thermal_rtsp_url,
            "-map", "0:v",
            "-map", "1:v",
            "-c", "copy",
            str(rec.filepath),
        ]

        try:
            process = await asyncio.create_subprocess_exec(
                *cmd,
                stdin=asyncio.subprocess.PIPE,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
            )
            rec.process = process
            rec.state = "recording"
            rec.start_time = time.time()
            rec.start_reader()
            logger.info("FFmpeg recording started for drone '%s': %s", drone_name, rec.filename)

            # Check for immediate process crash
            await asyncio.sleep(0.3)
            if process.returncode is not None:
                err_msg = "\n".join(rec.stderr_lines) or f"Process exited immediately with code {process.returncode}"
                rec.state = "failed"
                rec.error_message = err_msg
                logger.error("FFmpeg exited immediately for '%s': %s", drone_name, err_msg)
        except asyncio.CancelledError:
            logger.info("Recording wait task for '%s' was cancelled", drone_name)
            rec.state = "failed"
            raise
        except Exception as exc:
            logger.error("Failed to start FFmpeg for '%s': %s", drone_name, exc)
            rec.state = "failed"
            rec.error_message = str(exc)

    async def stop_recording(self, drone_name: str) -> dict[str, Any]:
        """Gracefully stop active recording or cancel waiting state."""
        clean_name = drone_name.strip().strip("'\"")
        if clean_name not in self._active:
            return {
                "status": "not_recording",
                "state": "idle",
                "name": clean_name,
                "drone_name": clean_name,
                "message": f"No active recording for drone '{clean_name}'",
            }

        rec = self._active[clean_name]

        # Case 1: Cancel while still waiting for stream
        if rec.state == "waiting":
            rec.cancel_wait_task()
            del self._active[clean_name]
            logger.info("Cancelled waiting to record for drone '%s'", clean_name)
            return {
                "status": "recording_cancelled",
                "state": "idle",
                "name": clean_name,
                "drone_name": clean_name,
                "message": "Recording cancelled while waiting for stream",
            }

        # Case 2: Stop running FFmpeg process
        rec.state = "stopping"
        rec.cancel_wait_task()
        duration_sec = max(0.0, time.time() - (rec.start_time or rec.init_time))
        process = rec.process

        logger.info("Stopping recording for drone '%s' (filename: %s)", clean_name, rec.filename)

        if process and process.returncode is None:
            # Send 'q' to stdin for clean MKV header & index finalization
            if process.stdin and not process.stdin.is_closing():
                try:
                    process.stdin.write(b"q\n")
                    await process.stdin.drain()
                    process.stdin.close()
                except Exception as exc:
                    logger.debug("Failed writing 'q' to ffmpeg stdin: %s", exc)

            # Wait for process to exit cleanly up to 5 seconds
            try:
                await asyncio.wait_for(process.wait(), timeout=5.0)
            except asyncio.TimeoutError:
                logger.warning("FFmpeg did not exit after 'q', sending terminate")
                try:
                    process.terminate()
                    await asyncio.wait_for(process.wait(), timeout=3.0)
                except Exception:
                    process.kill()
                    await process.wait()

        rec.stop_reader()
        del self._active[clean_name]

        file_size = rec.filepath.stat().st_size if rec.filepath.exists() else 0

        # Probe file for final metadata
        details = await self.probe_file(rec.filepath)

        rel_path = rec.filepath.relative_to(self.recordings_dir).as_posix() if rec.filepath.exists() else rec.filename

        return {
            "status": "recording_stopped",
            "state": "idle",
            "name": clean_name,
            "drone_name": clean_name,
            "filename": rec.filename,
            "relative_path": rel_path,
            "duration_sec": round(duration_sec, 2),
            "duration_formatted": format_duration(duration_sec),
            "file_size_bytes": file_size,
            "file_size_human": format_bytes(file_size),
            "download_url": f"/api/recordings/download/{rel_path}",
            "details": details,
        }

    def get_status(self, drone_name: str | None = None) -> dict[str, Any]:
        """Get recording status for a specific drone, or all active recordings if drone_name is None."""
        if drone_name:
            clean_name = drone_name.strip().strip("'\"")
            rec = self._active.get(clean_name)
            if not rec:
                return {
                    "is_recording": False,
                    "state": "idle",
                    "name": clean_name,
                    "drone_name": clean_name,
                }
            return self._build_drone_status(clean_name, rec)

        # Return all active recordings
        active_list = []
        by_drone = {}
        for name in list(self._active.keys()):
            rec = self._active.get(name)
            if rec:
                stat = self._build_drone_status(name, rec)
                if stat.get("is_recording"):
                    active_list.append(stat)
                    by_drone[name] = stat

        return {
            "active_count": len(active_list),
            "recordings": active_list,
            "by_drone": by_drone,
        }

    def _build_drone_status(self, clean_name: str, rec: ActiveRecording) -> dict[str, Any]:
        if rec.state == "waiting":
            elapsed_wait = time.time() - rec.init_time
            return {
                "is_recording": True,
                "state": "waiting",
                "name": clean_name,
                "drone_name": clean_name,
                "filename": rec.filename,
                "init_time": rec.init_time,
                "elapsed_waiting_sec": round(elapsed_wait, 1),
                "message": "Waiting for active camera feed from go2rtc…",
            }

        if rec.state == "recording":
            if rec.process and rec.process.returncode is not None:
                rec.stop_reader()
                del self._active[clean_name]
                return {
                    "is_recording": False,
                    "state": "idle",
                    "name": clean_name,
                    "drone_name": clean_name,
                }

            start_t = rec.start_time or rec.init_time
            elapsed = time.time() - start_t
            file_size = rec.filepath.stat().st_size if rec.filepath.exists() else 0
            return {
                "is_recording": True,
                "state": "recording",
                "name": clean_name,
                "drone_name": clean_name,
                "filename": rec.filename,
                "start_time": start_t,
                "created_at": datetime.fromtimestamp(start_t, tz=timezone.utc).isoformat(),
                "duration_sec": round(elapsed, 1),
                "duration_formatted": format_duration(elapsed),
                "file_size_bytes": file_size,
                "file_size_human": format_bytes(file_size),
            }

        # Failed or other state
        return {
            "is_recording": False,
            "state": "idle",
            "name": clean_name,
            "drone_name": clean_name,
            "error": rec.error_message,
        }


    async def probe_file(self, filepath: Path) -> dict[str, Any]:
        """Extract stream and container metadata using ffprobe with caching."""
        if not filepath.exists():
            return {}

        mtime = filepath.stat().st_mtime
        cache_key = filepath.name
        if cache_key in self._probe_cache:
            cached_mtime, cached_data = self._probe_cache[cache_key]
            if cached_mtime == mtime:
                return cached_data

        if not self.is_ffprobe_available():
            fallback = {
                "duration_sec": None,
                "streams": [],
                "format_name": "matroska",
            }
            return fallback

        try:
            cmd = [
                "ffprobe",
                "-v", "quiet",
                "-print_format", "json",
                "-show_format",
                "-show_streams",
                str(filepath),
            ]
            proc = await asyncio.create_subprocess_exec(
                *cmd,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
            )
            stdout, _ = await proc.communicate()
            if proc.returncode != 0 or not stdout:
                return {}

            data = json.loads(stdout.decode("utf-8", errors="replace"))
            fmt = data.get("format", {})
            raw_streams = data.get("streams", [])

            streams_info = []
            for idx, s in enumerate(raw_streams):
                if s.get("codec_type") == "video":
                    fps = parse_fps(s.get("r_frame_rate") or s.get("avg_frame_rate"))
                    stream_role = "visible" if idx == 0 else "thermal" if idx == 1 else f"track_{idx}"
                    streams_info.append({
                        "index": s.get("index", idx),
                        "role": stream_role,
                        "codec_name": s.get("codec_name"),
                        "codec_long_name": s.get("codec_long_name"),
                        "width": s.get("width"),
                        "height": s.get("height"),
                        "resolution": f"{s.get('width')}x{s.get('height')}" if s.get("width") and s.get("height") else None,
                        "fps": fps,
                        "bit_rate": int(s.get("bit_rate")) if s.get("bit_rate") and s.get("bit_rate").isdigit() else None,
                    })

            duration_raw = fmt.get("duration")
            duration_sec = float(duration_raw) if duration_raw else None
            bitrate_raw = fmt.get("bit_rate")
            bitrate = int(bitrate_raw) if bitrate_raw and bitrate_raw.isdigit() else None

            probed_result = {
                "format_name": fmt.get("format_name", "matroska"),
                "duration_sec": round(duration_sec, 2) if duration_sec is not None else None,
                "duration_formatted": format_duration(duration_sec) if duration_sec is not None else None,
                "bit_rate": bitrate,
                "streams": streams_info,
                "stream_count": len(streams_info),
            }

            self._probe_cache[cache_key] = (mtime, probed_result)
            return probed_result
        except Exception as exc:
            logger.warning("ffprobe failed on %s: %s", filepath.name, exc)
            return {}

    async def list_recordings(
        self,
        drone_name: str | None = None,
        sort_by: str = "time",
        order: str = "desc",
    ) -> list[dict[str, Any]]:
        """List all recordings with full metadata, sorted and filtered."""
        self.ensure_dir()
        filter_name = drone_name.strip().strip("'\"").lower() if drone_name else None

        files = list(self.recordings_dir.rglob("*.mkv")) + list(self.recordings_dir.rglob("*.mp4"))
        recordings: list[dict[str, Any]] = []

        for p in files:
            try:
                st = p.stat()
            except OSError:
                continue

            # Determine drone name from parent subfolder or filename prefix
            parts = p.stem.rsplit("_", 2)
            parsed_stem_drone = parts[0] if len(parts) >= 3 else ""

            if p.parent != self.recordings_dir:
                parsed_drone = p.parent.name
            else:
                parsed_drone = parsed_stem_drone or "unknown"

            if filter_name:
                matched = (
                    parsed_drone.lower() == filter_name
                    or (parsed_stem_drone and parsed_stem_drone.lower() == filter_name)
                )
                if not matched:
                    continue

            # Check if this file is currently being written by active recording
            is_active = any(
                rec.filepath.resolve() == p.resolve() and rec.state == "recording"
                for rec in self._active.values()
            )

            # Probe details (ffprobe)
            details = await self.probe_file(p)

            # Determine duration
            if is_active:
                active_rec = next((r for r in self._active.values() if r.filepath.resolve() == p.resolve()), None)
                start_t = active_rec.start_time if active_rec and active_rec.start_time else st.st_mtime
                duration_sec = round(time.time() - start_t, 2)
                duration_fmt = format_duration(duration_sec)
            else:
                duration_sec = details.get("duration_sec")
                duration_fmt = details.get("duration_formatted")

            # Creation time
            created_dt = datetime.fromtimestamp(st.st_mtime, tz=timezone.utc)
            rel_path = p.relative_to(self.recordings_dir).as_posix()

            recordings.append({
                "filename": p.name,
                "relative_path": rel_path,
                "name": parsed_drone,
                "drone_name": parsed_drone,
                "created_at": created_dt.isoformat(),
                "created_timestamp": st.st_mtime,
                "file_size_bytes": st.st_size,
                "file_size_human": format_bytes(st.st_size),
                "duration_sec": duration_sec,
                "duration_formatted": duration_fmt or "--:--",
                "is_active": is_active,
                "streams": details.get("streams", []),
                "stream_count": details.get("stream_count", len(details.get("streams", []))),
                "format_name": details.get("format_name", "matroska"),
                "download_url": f"/api/recordings/download/{rel_path}",
            })

        # Sorting
        reverse = (order.lower() != "asc")
        if sort_by == "duration":
            recordings.sort(key=lambda r: (r["duration_sec"] or 0), reverse=reverse)
        elif sort_by == "size":
            recordings.sort(key=lambda r: r["file_size_bytes"], reverse=reverse)
        elif sort_by == "name":
            recordings.sort(key=lambda r: r["filename"].lower(), reverse=reverse)
        else:  # default 'time'
            recordings.sort(key=lambda r: r["created_timestamp"], reverse=reverse)

        return recordings

    def get_recording_path(self, filename: str) -> Path | None:
        """Safely resolve a recording file path preventing path traversal."""
        # 1. Attempt direct resolution (handles "subfolder/file.mkv" or "file.mkv" in root)
        try:
            target = (self.recordings_dir / filename).resolve()
            target.relative_to(self.recordings_dir)
            if target.exists() and target.is_file():
                return target
        except (ValueError, OSError):
            return None

        # 2. Fallback: search for safe basename across subdirectories
        safe_name = Path(filename).name
        if not safe_name or safe_name in (".", ".."):
            return None

        for p in self.recordings_dir.rglob(safe_name):
            try:
                p_resolved = p.resolve()
                p_resolved.relative_to(self.recordings_dir)
                if p_resolved.is_file():
                    return p_resolved
            except (ValueError, OSError):
                continue

        return None

    async def delete_recording(self, filename: str) -> bool:
        """Delete a recording file if not currently active."""
        filepath = self.get_recording_path(filename)
        if not filepath:
            return False

        # Prevent deletion of actively recording file
        for rec in self._active.values():
            if rec.filepath.resolve() == filepath.resolve() and rec.state in ("waiting", "recording"):
                raise RuntimeError(f"Cannot delete '{filepath.name}': recording is currently active")

        try:
            filepath.unlink()
            self._probe_cache.pop(filepath.name, None)
            return True
        except Exception as exc:
            logger.error("Failed to delete recording %s: %s", filepath.name, exc)
            raise

    async def cleanup_all(self) -> None:
        """Gracefully stop all active recordings upon server shutdown."""
        active_names = list(self._active.keys())
        for name in active_names:
            try:
                await self.stop_recording(name)
            except Exception as exc:
                logger.warning("Error stopping recording for %s during cleanup: %s", name, exc)
