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

# FFmpeg socket I/O timeouts in microseconds:
# - Probe timeout (15s): Fast timeout for single-frame probes so unreachable
#   sources fail quickly during startup checks.
PROBE_INPUT_TIMEOUT_US = "15000000"
# Outer cap for a single frame probe. Must exceed PROBE_INPUT_TIMEOUT_US so the
# socket timeout — not the task timeout — is what bounds a slow-but-live source.
PROBE_TIMEOUT_SEC = 25.0

# - Record timeout (5 mins / 300s): Max wait time when a source disconnects during
#   recording. FFmpeg waits up to 5 minutes before stopping the recording.
RECORD_INPUT_TIMEOUT_US = "300000000"  # 300s in microseconds
INPUT_TIMEOUT_US = RECORD_INPUT_TIMEOUT_US


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


async def discover_streams(go2rtc_url: str, prefix: str) -> list[str]:
    """Return sorted go2rtc stream names starting with prefix (empty list on failure)."""
    if not go2rtc_url or not prefix:
        return []
    clean_base = go2rtc_url.strip().strip("'\"").rstrip("/")
    try:
        async with httpx.AsyncClient(timeout=2.0) as client:
            resp = await client.get(f"{clean_base}/api/streams")
        if resp.status_code != 200:
            return []
        data = resp.json()
    except Exception as exc:
        logger.debug("go2rtc stream discovery failed: %s", exc)
        return []
    if not isinstance(data, dict):
        return []
    return sorted(k for k in data if isinstance(k, str) and k.startswith(prefix))


class ActiveRecording:
    def __init__(
        self,
        drone_name: str,
        filename: str,
        filepath: Path,
        stream_urls: dict[str, str],
        go2rtc_url: str = "",
    ):
        self.drone_name = drone_name
        self.filename = filename
        self.filepath = filepath
        self.stream_urls = dict(stream_urls)
        self.go2rtc_url = go2rtc_url

        self.state: str = "waiting"  # "waiting" | "recording" | "stopping" | "failed"
        self.init_time = time.time()
        self.start_time: float | None = None
        self.error_message: str | None = None
        self.pending_streams: list[str] = []

        self.process: asyncio.subprocess.Process | None = None
        self.stderr_lines: list[str] = []
        self._reader_task: asyncio.Task | None = None
        self._wait_task: asyncio.Task | None = None

    @property
    def streams(self) -> list[dict[str, str]]:
        """Current streams as [{name, url}] pairs."""
        return [{"name": n, "url": u} for n, u in self.stream_urls.items()]

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
        """Verify the stream actually delivers video frames end-to-end.

        A go2rtc producer registration only proves the source is connected —
        not that frames are flowing. The authoritative check reads one real
        video packet from the exact URL FFmpeg will record from.
        """
        if not self.is_ffmpeg_available():
            return False

        # Socket I/O timeout for fast stream probe checks (15s)
        if rtsp_url.startswith(("rtsp://", "rtsps://")):
            input_opts = ["-rtsp_transport", "tcp", "-timeout", PROBE_INPUT_TIMEOUT_US]
        else:
            input_opts = ["-rw_timeout", PROBE_INPUT_TIMEOUT_US]

        cmd = [
            "ffmpeg",
            "-v", "error",
            "-nostdin",
            *input_opts,
            "-i", rtsp_url,
            "-map", "0:v:0",
            "-c", "copy",
            "-frames:v", "1",
            "-f", "null",
            "-",
        ]
        proc: asyncio.subprocess.Process | None = None
        try:
            proc = await asyncio.create_subprocess_exec(
                *cmd,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
            )
            _stdout, stderr = await asyncio.wait_for(proc.communicate(), timeout=PROBE_TIMEOUT_SEC)
            if proc.returncode == 0:
                return True
            logger.info(
                "Frame probe for stream '%s' failed (rc=%s): %s",
                stream_name,
                proc.returncode,
                (stderr or b"").decode("utf-8", errors="replace").strip()[-300:],
            )
            return False
        except asyncio.CancelledError:
            raise
        except Exception as exc:
            logger.info("Frame probe for stream '%s' failed: %s", stream_name, exc)
            return False
        finally:
            # Never leak the probe process if it timed out or was cancelled
            if proc is not None and proc.returncode is None:
                try:
                    proc.kill()
                    await proc.wait()
                except Exception:
                    pass

    async def start_recording(
        self,
        drone_name: str,
        stream_urls: dict[str, str] | None = None,
        go2rtc_url: str = "",
        wait_timeout: float = 90.0,
    ) -> dict[str, Any]:
        """Initiate recording by entering a waiting state until camera feeds are active.

        The default deadline (90s) accommodates slow drone cold starts: each
        probe allows up to 15s for the first frame, and streams are probed
        once per second until frames flow or the deadline expires.
        """
        self.ensure_dir()
        clean_name = drone_name.strip().strip("'\"")
        if not clean_name:
            raise ValueError("Drone name cannot be empty")

        clean_urls = {
            n.strip().strip("'\""): u.strip().strip("'\"")
            for n, u in (stream_urls or {}).items()
            if n and n.strip() and u and u.strip()
        }
        if not clean_urls:
            raise ValueError("No stream URLs to record")

        # Check if already active
        if clean_name in self._active:
            active = self._active[clean_name]
            if active.state == "waiting":
                return {
                    "status": "already_waiting",
                    "state": "waiting",
                    "name": clean_name,
                    "drone_name": clean_name,
                    "message": "Already waiting for camera streams to deliver frames",
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
            stream_urls=clean_urls,
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
            "streams": rec.streams,
            "message": "Waiting for camera streams to deliver video frames…",
            "init_time": rec.init_time,
            "created_at": datetime.fromtimestamp(rec.init_time, tz=timezone.utc).isoformat(),
        }

    async def _wait_and_start_capture(self, rec: ActiveRecording, wait_timeout: float = 45.0) -> None:
        """Wait until every camera stream delivers real video frames, then record.

        A go2rtc producer registration alone is not trusted: FFmpeg is only
        spawned after a probe read confirms frames flow end-to-end. If FFmpeg
        starts but the output file stays empty (frames stopped again), it is
        torn down and the wait resumes until the deadline. On timeout the
        recording fails with a clear error instead of producing a 0-byte file.
        """
        drone_name = rec.drone_name
        deadline = time.time() + wait_timeout
        next_status_log = 0.0

        logger.info(
            "Drone '%s': waiting for camera feeds to deliver frames (%s)...",
            drone_name,
            ", ".join(rec.stream_urls),
        )

        while True:
            # Phase 1: every mapped stream must deliver video frames.
            # Streams are probed in parallel — a multi-camera drone's cold
            # start would otherwise stack probe latencies sequentially.
            streams_ready = False
            while time.time() < deadline:
                try:
                    results = await asyncio.gather(
                        *(
                            self.probe_stream_active(rec.go2rtc_url, stream["name"], stream["url"])
                            for stream in rec.streams
                        ),
                        return_exceptions=True,
                    )
                except asyncio.CancelledError:
                    rec.state = "failed"
                    raise

                pending = [
                    stream["name"]
                    for stream, result in zip(rec.streams, results)
                    if result is not True
                ]
                for stream, result in zip(rec.streams, results):
                    if isinstance(result, BaseException):
                        logger.warning("Probe error for '%s' stream '%s': %s", drone_name, stream["name"], result)

                rec.pending_streams = pending
                if not pending:
                    rec.pending_streams = []
                    streams_ready = True
                    break

                if time.time() >= next_status_log:
                    logger.info(
                        "Drone '%s': still waiting for video frames from: %s",
                        drone_name,
                        ", ".join(pending),
                    )
                    next_status_log = time.time() + 5.0
                await asyncio.sleep(1.0)

            if not streams_ready:
                rec.state = "failed"
                rec.error_message = (
                    f"No video frames received from streams ({', '.join(rec.stream_urls)}) "
                    f"within {int(wait_timeout)}s"
                )
                logger.error("Drone '%s': %s", drone_name, rec.error_message)
                return

            logger.info("Drone '%s': all camera feeds deliver frames. Starting FFmpeg...", drone_name)

            # Phase 2: launch FFmpeg
            try:
                await self._spawn_ffmpeg(rec)
            except asyncio.CancelledError:
                rec.state = "failed"
                raise
            except Exception as exc:
                logger.error("Failed to start FFmpeg for '%s': %s", drone_name, exc)
                rec.state = "failed"
                rec.error_message = str(exc)
                return

            logger.info("FFmpeg recording started for drone '%s': %s", drone_name, rec.filename)

            # Phase 3: confirm frames actually land on disk
            if await self._wait_for_output_data(rec, grace=6.0):
                return

            # No data (or early crash): tear down and wait again while time remains
            error = rec.error_message or "FFmpeg produced no data (stream delivered no frames)"
            logger.warning("Drone '%s': %s — restarting stream wait", drone_name, error)
            await self._terminate_ffmpeg(rec)
            self._discard_output_file(rec)
            if time.time() >= deadline:
                rec.state = "failed"
                rec.error_message = error
                return
            rec.state = "waiting"
            rec.start_time = None
            rec.error_message = None
            await asyncio.sleep(1.0)

    async def _spawn_ffmpeg(self, rec: ActiveRecording) -> None:
        """Build and launch the multi-stream copy FFmpeg process.

        Each input gets an I/O timeout of 5 minutes (300s) so transient
        source disconnects wait before stopping the recording.
        """
        streams = rec.streams
        cmd = ["ffmpeg", "-y", "-hide_banner", "-loglevel", "warning"]
        for stream in streams:
            url = stream["url"]
            if url.startswith(("rtsp://", "rtsps://")):
                cmd += ["-rtsp_transport", "tcp", "-timeout", RECORD_INPUT_TIMEOUT_US, "-i", url]
            else:
                cmd += ["-rw_timeout", RECORD_INPUT_TIMEOUT_US, "-i", url]
        for i in range(len(streams)):
            cmd += ["-map", f"{i}:v"]
        cmd += ["-c", "copy", str(rec.filepath)]

        process = await asyncio.create_subprocess_exec(
            *cmd,
            stdin=asyncio.subprocess.PIPE,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
        )
        rec.stderr_lines = []
        rec.error_message = None
        rec.process = process
        rec.state = "recording"
        rec.start_time = time.time()
        rec.start_reader()

    async def _wait_for_output_data(self, rec: ActiveRecording, grace: float = 6.0) -> bool:
        """Return True once the output file has bytes on disk.

        Returns False if FFmpeg dies early or no data arrives within `grace`
        seconds — the caller should tear down and retry the stream wait.
        """
        deadline = time.time() + grace
        while time.time() < deadline:
            process = rec.process
            if process is None:
                rec.error_message = "FFmpeg process disappeared before writing data"
                return False
            if process.returncode is not None:
                rec.error_message = (
                    "\n".join(rec.stderr_lines[-10:])
                    or f"FFmpeg exited early with code {process.returncode}"
                )
                logger.error("FFmpeg exited early for drone '%s': %s", rec.drone_name, rec.error_message)
                return False
            try:
                if rec.filepath.exists() and rec.filepath.stat().st_size > 0:
                    return True
            except OSError:
                pass
            await asyncio.sleep(0.5)

        rec.error_message = "FFmpeg started but no data was written (stream delivered no frames)"
        return False

    async def _terminate_ffmpeg(self, rec: ActiveRecording) -> None:
        """Gracefully stop the FFmpeg process ('q' → terminate → kill)."""
        process = rec.process
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
        rec.process = None

    def _discard_output_file(self, rec: ActiveRecording) -> None:
        """Remove a failed attempt's empty/partial file so it never shows up in listings."""
        try:
            if rec.filepath.exists():
                rec.filepath.unlink()
        except OSError as exc:
            logger.debug("Could not remove failed output %s: %s", rec.filename, exc)

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

        logger.info("Stopping recording for drone '%s' (filename: %s)", clean_name, rec.filename)

        await self._terminate_ffmpeg(rec)
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
                "pending_streams": list(rec.pending_streams),
                "message": "Waiting for camera streams to deliver video frames…",
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
