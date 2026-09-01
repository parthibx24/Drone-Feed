"""Unit and integration tests for server-side dual-stream FFmpeg recording."""

import asyncio
import os
import shutil
import tempfile
import unittest
from pathlib import Path
from unittest.mock import AsyncMock, MagicMock, patch

from fastapi.testclient import TestClient

from app import create_app
from config import ViewerSettings, build_rtsp_urls, build_stream_sources
from recorder import (
    INPUT_TIMEOUT_US,
    PROBE_INPUT_TIMEOUT_US,
    RECORD_INPUT_TIMEOUT_US,
    RecordingManager,
    format_bytes,
    format_duration,
    parse_fps,
)


class TestConfigAndHelpers(unittest.TestCase):
    def test_rtsp_url_builder(self):
        cfg = ViewerSettings(
            go2rtc_url="https://cams.drone.starwatch.bb7fab11.sslip.io",
            go2rtc_rtsp_url="",
        )
        self.assertEqual(
            cfg.get_rtsp_base_url(),
            "rtsp://cams.drone.starwatch.bb7fab11.sslip.io:8554",
        )
        # Default: legacy visible/thermal pair
        urls = build_rtsp_urls("kutwali", cfg)
        self.assertEqual(
            urls,
            {
                "kutwali_camera": "rtsp://cams.drone.starwatch.bb7fab11.sslip.io:8554/kutwali_camera",
                "kutwali_thermal": "rtsp://cams.drone.starwatch.bb7fab11.sslip.io:8554/kutwali_thermal",
            },
        )
        # Explicit stream names (any number of cameras)
        urls = build_rtsp_urls("drone1", cfg, ["drone1_main", "drone1_rear", "drone1_thermal"])
        self.assertEqual(
            urls,
            {
                "drone1_main": "rtsp://cams.drone.starwatch.bb7fab11.sslip.io:8554/drone1_main",
                "drone1_rear": "rtsp://cams.drone.starwatch.bb7fab11.sslip.io:8554/drone1_rear",
                "drone1_thermal": "rtsp://cams.drone.starwatch.bb7fab11.sslip.io:8554/drone1_thermal",
            },
        )

    def test_custom_rtsp_url(self):
        cfg = ViewerSettings(
            go2rtc_url="https://example.com",
            go2rtc_rtsp_url="rtsp://10.0.0.5:8554",
        )
        self.assertEqual(cfg.get_rtsp_base_url(), "rtsp://10.0.0.5:8554")
        urls = build_rtsp_urls("drone1", cfg, ["drone1_camera", "drone1_thermal"])
        self.assertEqual(
            urls,
            {
                "drone1_camera": "rtsp://10.0.0.5:8554/drone1_camera",
                "drone1_thermal": "rtsp://10.0.0.5:8554/drone1_thermal",
            },
        )

    def test_recording_path_configuration(self):
        # Default should be ROOT / "recording"
        cfg_default = ViewerSettings()
        self.assertTrue(str(cfg_default.recording_path).endswith("recording"))
        self.assertEqual(cfg_default.recordings_dir, cfg_default.recording_path)

        # Custom relative path
        cfg_rel = ViewerSettings(recording_path="my_custom_records")
        self.assertTrue(str(cfg_rel.recording_path).endswith("my_custom_records"))

        # Custom alias RECORDING_PATH
        cfg_alias = ViewerSettings(RECORDING_PATH="custom_folder")
        self.assertTrue(str(cfg_alias.recording_path).endswith("custom_folder"))

    def test_format_helpers(self):
        self.assertEqual(format_bytes(500), "500 B")
        self.assertEqual(format_bytes(2048), "2.0 KB")
        self.assertEqual(format_bytes(10485760), "10.0 MB")
        self.assertEqual(format_duration(45), "00:45")
        self.assertEqual(format_duration(125), "02:05")
        self.assertEqual(format_duration(3665), "01:01:05")
        self.assertEqual(parse_fps("30/1"), 30.0)
        self.assertEqual(parse_fps("29.97"), 29.97)
        self.assertIsNone(parse_fps("0/0"))
        self.assertIsNone(parse_fps(None))

    def test_timeout_constants(self):
        # Probe timeout must be fast (15s = 15,000,000 us)
        self.assertEqual(PROBE_INPUT_TIMEOUT_US, "15000000")
        # Recording timeout must be 5 minutes (300s = 300,000,000 us)
        self.assertEqual(RECORD_INPUT_TIMEOUT_US, "300000000")
        self.assertEqual(INPUT_TIMEOUT_US, RECORD_INPUT_TIMEOUT_US)



class TestRecordingManager(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.temp_dir = tempfile.mkdtemp()
        self.manager = RecordingManager(Path(self.temp_dir))

    async def asyncTearDown(self):
        await self.manager.cleanup_all()
        shutil.rmtree(self.temp_dir, ignore_errors=True)

    async def test_initial_state(self):
        status = self.manager.get_status("test_drone")
        self.assertFalse(status["is_recording"])
        self.assertEqual(status["name"], "test_drone")
        self.assertEqual(status["drone_name"], "test_drone")

    async def test_path_traversal_prevention(self):
        # Attempt to access outside file
        resolved = self.manager.get_recording_path("../../etc/passwd")
        self.assertIsNone(resolved)
        resolved_nested = self.manager.get_recording_path("sub/../../secret.txt")
        self.assertIsNone(resolved_nested)

    async def test_list_recordings_and_sorting(self):
        # Create dummy sample recording files in subfolders and root
        sub_a = Path(self.temp_dir) / "droneA"
        sub_b = Path(self.temp_dir) / "droneB"
        sub_a.mkdir(parents=True, exist_ok=True)
        sub_b.mkdir(parents=True, exist_ok=True)

        file1 = sub_a / "droneA_20260824_100000.mkv"
        file2 = sub_a / "droneA_20260824_110000.mkv"
        file3 = sub_b / "droneB_20260824_120000.mkv"

        file1.write_bytes(b"A" * 100)
        file2.write_bytes(b"B" * 300)
        file3.write_bytes(b"C" * 200)

        # Set specific mtimes
        os.utime(file1, (1000, 1000))
        os.utime(file2, (2000, 2000))
        os.utime(file3, (3000, 3000))

        # List all sorted by time desc (default)
        records = await self.manager.list_recordings()
        self.assertEqual(len(records), 3)
        self.assertEqual(records[0]["filename"], file3.name)
        self.assertEqual(records[1]["filename"], file2.name)
        self.assertEqual(records[2]["filename"], file1.name)

        # Filter by drone name
        drone_a_records = await self.manager.list_recordings(drone_name="droneA")
        self.assertEqual(len(drone_a_records), 2)
        self.assertTrue(all(r["name"] == "droneA" and r["drone_name"] == "droneA" for r in drone_a_records))

        # Sort by size desc
        by_size = await self.manager.list_recordings(sort_by="size", order="desc")
        self.assertEqual(by_size[0]["filename"], file2.name)
        self.assertEqual(by_size[1]["filename"], file3.name)
        self.assertEqual(by_size[2]["filename"], file1.name)

    async def test_delete_recording_in_subfolder(self):
        sub = Path(self.temp_dir) / "kutwali"
        sub.mkdir(parents=True, exist_ok=True)
        test_file = sub / "kutwali_20260824_150000.mkv"
        test_file.write_bytes(b"sample video bytes")
        self.assertTrue(test_file.exists())

        # Test resolving path both ways
        resolved_rel = self.manager.get_recording_path("kutwali/kutwali_20260824_150000.mkv")
        self.assertIsNotNone(resolved_rel)
        resolved_base = self.manager.get_recording_path("kutwali_20260824_150000.mkv")
        self.assertIsNotNone(resolved_base)

        success = await self.manager.delete_recording("kutwali_20260824_150000.mkv")
        self.assertTrue(success)
        self.assertFalse(test_file.exists())

    async def test_start_and_cancel_while_waiting(self):
        # Start recording (enters waiting state)
        result = await self.manager.start_recording(
            drone_name="kutwali",
            stream_urls={
                "kutwali_camera": "rtsp://localhost:8554/kutwali_camera",
                "kutwali_thermal": "rtsp://localhost:8554/kutwali_thermal",
            },
            wait_timeout=10.0,
        )
        self.assertEqual(result["status"], "waiting_for_stream")
        self.assertEqual(result["state"], "waiting")
        self.assertEqual(result["drone_name"], "kutwali")

        # Verify target filepath is inside the drone subfolder
        rec = self.manager._active["kutwali"]
        self.assertEqual(rec.filepath.parent, Path(self.temp_dir) / "kutwali")
        self.assertTrue(rec.filepath.parent.exists())

        # Status check
        status = self.manager.get_status("kutwali")
        self.assertTrue(status["is_recording"])
        self.assertEqual(status["state"], "waiting")

        # Cancel while waiting
        stop_result = await self.manager.stop_recording("kutwali")
        self.assertEqual(stop_result["status"], "recording_cancelled")
        self.assertEqual(stop_result["state"], "idle")

        # Post status check
        post_status = self.manager.get_status("kutwali")
        self.assertFalse(post_status["is_recording"])

    @patch.object(RecordingManager, "probe_stream_active", AsyncMock(return_value=True))
    @patch("asyncio.create_subprocess_exec")
    async def test_start_and_stop_recording(self, mock_subprocess):
        # Create a mock subprocess
        mock_proc = AsyncMock()
        mock_proc.returncode = None
        mock_proc.stdin = MagicMock()
        mock_proc.stdin.is_closing.return_value = False
        mock_proc.stdin.write = MagicMock()
        mock_proc.stdin.drain = AsyncMock()
        mock_proc.stdin.close = MagicMock()
        mock_proc.wait = AsyncMock(return_value=0)
        mock_proc.stderr = AsyncMock()
        mock_proc.stderr.readline = AsyncMock(return_value=b"")

        mock_subprocess.return_value = mock_proc

        # Start recording
        result = await self.manager.start_recording(
            drone_name="kutwali",
            stream_urls={
                "kutwali_camera": "rtsp://localhost:8554/kutwali_camera",
                "kutwali_thermal": "rtsp://localhost:8554/kutwali_thermal",
            },
        )
        self.assertEqual(result["status"], "waiting_for_stream")

        # Wait a brief moment for probe loop to detect active stream and start ffmpeg
        await asyncio.sleep(0.2)

        # Status check - should have transitioned to recording
        status = self.manager.get_status("kutwali")
        self.assertTrue(status["is_recording"])
        self.assertEqual(status["state"], "recording")
        self.assertEqual(status["drone_name"], "kutwali")

        # Stop recording
        stop_result = await self.manager.stop_recording("kutwali")
        self.assertEqual(stop_result["status"], "recording_stopped")
        self.assertIn("kutwali", stop_result["download_url"])
        mock_proc.stdin.write.assert_called_with(b"q\n")

        # Status check after stop
        post_status = self.manager.get_status("kutwali")
        self.assertFalse(post_status["is_recording"])

    @patch.object(RecordingManager, "is_ffmpeg_available", return_value=True)
    @patch("asyncio.create_subprocess_exec")
    async def test_probe_requires_actual_frames(self, mock_subprocess, mock_avail):
        mock_proc = AsyncMock()
        mock_proc.returncode = 0
        mock_proc.communicate = AsyncMock(return_value=(b"", b""))
        mock_proc.kill = MagicMock()
        mock_proc.wait = AsyncMock(return_value=0)
        mock_subprocess.return_value = mock_proc

        # Streams count as active only when a real video frame can be read
        active = await self.manager.probe_stream_active("", "cam", "rtsp://localhost:8554/cam")
        self.assertTrue(active)

        cmd = mock_subprocess.call_args.args
        self.assertEqual(cmd[0], "ffmpeg")
        self.assertIn("rtsp://localhost:8554/cam", cmd)
        self.assertIn("-frames:v", cmd)
        self.assertIn("1", cmd)
        self.assertIn("-f", cmd)
        self.assertIn("-timeout", cmd)  # RTSP inputs get a socket I/O timeout
        self.assertEqual(cmd[cmd.index("-timeout") + 1], PROBE_INPUT_TIMEOUT_US)

        # Non-zero exit (connected but no frames / unreachable) must NOT be active
        mock_subprocess.reset_mock()
        mock_proc.returncode = 1
        inactive = await self.manager.probe_stream_active("", "cam", "rtsp://localhost:8554/cam")
        self.assertFalse(inactive)

        # Non-RTSP sources use -rw_timeout instead
        mock_subprocess.reset_mock()
        mock_proc.returncode = 0
        mp4_active = await self.manager.probe_stream_active(
            "", "cam", "https://cams.example.com/api/stream.mp4?src=cam"
        )
        self.assertTrue(mp4_active)
        mp4_cmd = mock_subprocess.call_args.args
        self.assertIn("-rw_timeout", mp4_cmd)
        self.assertEqual(mp4_cmd[mp4_cmd.index("-rw_timeout") + 1], PROBE_INPUT_TIMEOUT_US)

    @patch.object(RecordingManager, "probe_stream_active", new=AsyncMock(return_value=False))
    @patch.object(RecordingManager, "is_ffmpeg_available", return_value=True)
    @patch("asyncio.create_subprocess_exec")
    async def test_wait_timeout_fails_without_frames(self, mock_subprocess, mock_avail):
        result = await self.manager.start_recording(
            drone_name="kutwali",
            stream_urls={"kutwali_camera": "rtsp://localhost:8554/kutwali_camera"},
            wait_timeout=1.5,
        )
        self.assertEqual(result["state"], "waiting")

        await asyncio.sleep(2.2)

        # Never went into recording mode; failed with a clear error and no file
        status = self.manager.get_status("kutwali")
        self.assertFalse(status["is_recording"])
        self.assertEqual(status["state"], "idle")
        self.assertIn("frames", status["error"].lower())
        mock_subprocess.assert_not_called()

    @patch.object(RecordingManager, "_wait_for_output_data", new_callable=AsyncMock)
    @patch.object(RecordingManager, "_terminate_ffmpeg", new_callable=AsyncMock)
    @patch.object(RecordingManager, "_discard_output_file")
    @patch.object(RecordingManager, "probe_stream_active", new=AsyncMock(return_value=True))
    @patch.object(RecordingManager, "is_ffmpeg_available", return_value=True)
    @patch("asyncio.create_subprocess_exec")
    async def test_recording_restarts_when_no_data_flows(
        self, mock_subprocess, mock_avail, mock_discard, mock_terminate, mock_wait_data
    ):
        mock_proc = AsyncMock()
        mock_proc.returncode = None
        mock_proc.stdin = MagicMock()
        mock_proc.stdin.is_closing.return_value = False
        mock_proc.wait = AsyncMock(return_value=0)
        mock_proc.stderr = AsyncMock()
        mock_proc.stderr.readline = AsyncMock(return_value=b"")
        mock_subprocess.return_value = mock_proc

        # First FFmpeg attempt writes no data; second attempt succeeds
        mock_wait_data.side_effect = [False, True]

        await self.manager.start_recording(
            drone_name="kutwali",
            stream_urls={"kutwali_camera": "rtsp://localhost:8554/kutwali_camera"},
            wait_timeout=10.0,
        )
        await asyncio.sleep(1.5)

        status = self.manager.get_status("kutwali")
        self.assertTrue(status["is_recording"])
        self.assertEqual(status["state"], "recording")
        self.assertEqual(mock_subprocess.call_count, 2)
        self.assertEqual(mock_terminate.await_count, 1)
        mock_discard.assert_called_once()

        await self.manager.stop_recording("kutwali")

    async def test_start_recording_empty_streams_raises(self):
        with self.assertRaises(ValueError):
            await self.manager.start_recording(
                drone_name="kutwali",
                stream_urls={},
                wait_timeout=10.0,
            )

    @patch.object(RecordingManager, "probe_stream_active", AsyncMock(return_value=True))
    @patch("recorder.discover_streams", new_callable=AsyncMock)
    @patch("asyncio.create_subprocess_exec")
    async def test_start_recording_multi_stream(self, mock_subprocess, mock_discover):
        mock_proc = AsyncMock()
        mock_proc.returncode = None
        mock_proc.stdin = MagicMock()
        mock_proc.stdin.is_closing.return_value = False
        mock_proc.stdin.write = MagicMock()
        mock_proc.stdin.drain = AsyncMock()
        mock_proc.stdin.close = MagicMock()
        mock_proc.wait = AsyncMock(return_value=0)
        mock_proc.stderr = AsyncMock()
        mock_proc.stderr.readline = AsyncMock(return_value=b"")
        mock_subprocess.return_value = mock_proc

        result = await self.manager.start_recording(
            drone_name="kutwali",
            stream_urls={
                "kutwali_main": "rtsp://localhost:8554/kutwali_main",
                "kutwali_rear": "https://cams.example.com/api/stream.mp4?src=kutwali_rear",
                "kutwali_thermal": "rtsp://localhost:8554/kutwali_thermal",
            },
            go2rtc_url="https://go2rtc.example.com",
        )
        self.assertEqual(result["status"], "waiting_for_stream")

        await asyncio.sleep(0.2)

        status = self.manager.get_status("kutwali")
        self.assertTrue(status["is_recording"])
        self.assertEqual(status["state"], "recording")

        rec = self.manager._active["kutwali"]
        self.assertEqual(list(rec.stream_urls), ["kutwali_main", "kutwali_rear", "kutwali_thermal"])

        # The wait loop must NOT re-enumerate — page-load discovery is authoritative
        mock_discover.assert_not_called()

        # FFmpeg command: one input + one map per camera, with an I/O timeout
        # on every input (rtsp uses -timeout, http uses -rw_timeout)
        cmd = mock_subprocess.call_args.args
        self.assertEqual(cmd.count("-i"), 3)
        self.assertIn("rtsp://localhost:8554/kutwali_main", cmd)
        self.assertIn("https://cams.example.com/api/stream.mp4?src=kutwali_rear", cmd)
        self.assertIn("rtsp://localhost:8554/kutwali_thermal", cmd)
        for mapping in ("0:v", "1:v", "2:v"):
            self.assertIn(mapping, cmd)
        self.assertEqual(cmd.count("-timeout"), 2)
        self.assertEqual(cmd.count("-rw_timeout"), 1)
        self.assertEqual(cmd.count(RECORD_INPUT_TIMEOUT_US), 3)
        self.assertEqual(cmd.count(INPUT_TIMEOUT_US), 3)

        await self.manager.stop_recording("kutwali")




class TestFastAPIEndpoints(unittest.TestCase):
    def setUp(self):
        self.temp_dir = tempfile.mkdtemp()
        self.settings = ViewerSettings(
            recording_path=Path(self.temp_dir),
            go2rtc_url="https://cams.example.com",
            go2rtc_ingest_mode="rtsp",
            viewer_port=8091,
        )
        self.app = create_app(self.settings)
        self.client = TestClient(self.app)

    def tearDown(self):
        shutil.rmtree(self.temp_dir, ignore_errors=True)

    def test_config_endpoint(self):
        resp = self.client.get("/api/config")
        self.assertEqual(resp.status_code, 200)
        data = resp.json()
        self.assertIn("go2rtc_url", data)
        self.assertIn("go2rtc_rtsp_url", data)

    def test_go2rtc_streams_proxy_endpoint(self):
        with patch("app.proxy_get", new_callable=AsyncMock) as mock_proxy:
            from fastapi import Response
            mock_proxy.return_value = Response(content=b'{"kutwali_camera": {"producers": []}}', media_type="application/json")
            resp = self.client.get("/api/go2rtc/streams?src=kutwali_camera")
            self.assertEqual(resp.status_code, 200)
            mock_proxy.assert_called_once()

    def test_cameras_endpoint(self):
        with patch("app.discover_streams", new_callable=AsyncMock) as mock_disc:
            mock_disc.return_value = ["kutwali_camera", "kutwali_thermal"]
            resp = self.client.get("/api/cameras?name=kutwali")
            self.assertEqual(resp.status_code, 200)
            data = resp.json()
            self.assertEqual(data["name"], "kutwali")
            self.assertEqual(data["cameras"], ["kutwali_camera", "kutwali_thermal"])
            mock_disc.assert_called_once_with("https://cams.example.com", "kutwali_")

    def test_cameras_endpoint_host_fallback(self):
        # When name is not supplied, the host value is used as the name
        with patch("app.discover_streams", new_callable=AsyncMock) as mock_disc:
            mock_disc.return_value = []
            resp = self.client.get("/api/cameras?host=100.91.79.23")
            self.assertEqual(resp.status_code, 200)
            data = resp.json()
            self.assertEqual(data["name"], "100.91.79.23")
            self.assertEqual(data["cameras"], [])
            mock_disc.assert_called_once_with("https://cams.example.com", "100.91.79.23_")

    def test_cameras_endpoint_missing_params(self):
        resp = self.client.get("/api/cameras")
        self.assertEqual(resp.status_code, 400)

    def test_stream_source_builder_modes(self):
        cfg_rtsp = ViewerSettings(go2rtc_url="https://cams.example.com", go2rtc_ingest_mode="rtsp")
        self.assertEqual(
            build_stream_sources(cfg_rtsp, ["drone1_camera"]),
            {"drone1_camera": "rtsp://cams.example.com:8554/drone1_camera"},
        )

        cfg_mp4 = ViewerSettings(go2rtc_url="https://cams.example.com", go2rtc_ingest_mode="mp4")
        self.assertEqual(
            build_stream_sources(cfg_mp4, ["drone1_camera", "drone1_rear"]),
            {
                "drone1_camera": "https://cams.example.com/api/stream.mp4?src=drone1_camera",
                "drone1_rear": "https://cams.example.com/api/stream.mp4?src=drone1_rear",
            },
        )

        cfg_hls = ViewerSettings(go2rtc_url="https://cams.example.com", go2rtc_ingest_mode="hls")
        self.assertEqual(
            build_stream_sources(cfg_hls, ["drone1_camera"]),
            {"drone1_camera": "https://cams.example.com/api/stream.m3u8?src=drone1_camera"},
        )

        with self.assertRaises(Exception):
            ViewerSettings(go2rtc_url="https://cams.example.com", go2rtc_ingest_mode="webrtc")

    def test_recording_start_discovers_streams(self):
        mgr = self.app.state.recorder
        with patch("app.discover_streams", new_callable=AsyncMock) as mock_disc, \
             patch.object(mgr, "start_recording", new_callable=AsyncMock) as mock_start:
            mock_disc.return_value = ["kutwali_main", "kutwali_thermal"]
            mock_start.return_value = {"status": "waiting_for_stream", "state": "waiting"}
            resp = self.client.post("/api/recording/start?name=kutwali")
            self.assertEqual(resp.status_code, 200)
            mock_disc.assert_called_once_with("https://cams.example.com", "kutwali_")
            mock_start.assert_called_once_with(
                drone_name="kutwali",
                stream_urls={
                    "kutwali_main": "rtsp://cams.example.com:8554/kutwali_main",
                    "kutwali_thermal": "rtsp://cams.example.com:8554/kutwali_thermal",
                },
                go2rtc_url="https://cams.example.com",
            )

    def test_recording_start_no_streams_discovered(self):
        mgr = self.app.state.recorder
        with patch("app.discover_streams", new_callable=AsyncMock) as mock_disc, \
             patch.object(mgr, "start_recording", new_callable=AsyncMock) as mock_start:
            mock_disc.return_value = []  # go2rtc unreachable / no streams
            mock_start.return_value = {"status": "waiting_for_stream", "state": "waiting"}
            resp = self.client.post("/api/recording/start?name=solo")
            self.assertEqual(resp.status_code, 200)
            # Empty discovery → conventional {name}_camera/_thermal pair fallback
            mock_start.assert_called_once_with(
                drone_name="solo",
                stream_urls={
                    "solo_camera": "rtsp://cams.example.com:8554/solo_camera",
                    "solo_thermal": "rtsp://cams.example.com:8554/solo_thermal",
                },
                go2rtc_url="https://cams.example.com",
            )

    def test_recording_status_endpoint(self):
        # Parameterless call should return all active recordings overview
        resp_all = self.client.get("/api/recording/status")
        self.assertEqual(resp_all.status_code, 200)
        data_all = resp_all.json()
        self.assertEqual(data_all["active_count"], 0)
        self.assertEqual(data_all["recordings"], [])
        self.assertEqual(data_all["by_drone"], {})

        # Filtered call with name
        resp = self.client.get("/api/recording/status?name=kutwali")
        self.assertEqual(resp.status_code, 200)
        data = resp.json()
        self.assertFalse(data["is_recording"])
        self.assertEqual(data["name"], "kutwali")
        self.assertEqual(data["drone_name"], "kutwali")


    def test_recordings_list_download_and_delete_subfolder(self):
        # Create a mock recording file in the drone subfolder
        sub = Path(self.temp_dir) / "kutwali"
        sub.mkdir(parents=True, exist_ok=True)
        file_path = sub / "kutwali_20260824_120000.mkv"
        file_path.write_bytes(b"\x1a\x45\xdf\xa3" + b"Mock MKV Content")

        # GET /api/recordings
        resp = self.client.get("/api/recordings?name=kutwali")
        self.assertEqual(resp.status_code, 200)
        items = resp.json()
        self.assertEqual(len(items), 1)
        self.assertEqual(items[0]["filename"], "kutwali_20260824_120000.mkv")
        self.assertEqual(items[0]["name"], "kutwali")
        self.assertEqual(items[0]["drone_name"], "kutwali")
        self.assertIn("download_url", items[0])

        # GET /api/recordings/download/{relative_path}
        dl_resp1 = self.client.get(f"/api/recordings/download/kutwali/{file_path.name}")
        self.assertEqual(dl_resp1.status_code, 200)
        self.assertEqual(dl_resp1.content, b"\x1a\x45\xdf\xa3Mock MKV Content")

        # GET /api/recordings/download/{filename}
        dl_resp2 = self.client.get(f"/api/recordings/download/{file_path.name}")
        self.assertEqual(dl_resp2.status_code, 200)
        self.assertEqual(dl_resp2.content, b"\x1a\x45\xdf\xa3Mock MKV Content")

        # DELETE /api/recordings/{filename}
        del_resp = self.client.delete(f"/api/recordings/{file_path.name}")
        self.assertEqual(del_resp.status_code, 200)
        self.assertTrue(del_resp.json()["deleted"])

        # Subsequent GET should be empty
        resp2 = self.client.get("/api/recordings")
        self.assertEqual(len(resp2.json()), 0)

    def test_not_found_recording(self):
        resp = self.client.get("/api/recordings/download/missing_file.mkv")
        self.assertEqual(resp.status_code, 404)

        del_resp = self.client.delete("/api/recordings/missing_file.mkv")
        self.assertEqual(del_resp.status_code, 404)


if __name__ == "__main__":
    unittest.main()
