"""Runtime health and useful, secret-safe extraction failures."""
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import main
from test_credentials import cookies


class RuntimeTest(unittest.IsolatedAsyncioTestCase):
    async def health(self):
        return await main.p.dispatch_buffered({"operation": "health_check", "input": {}})

    async def test_health_reports_missing_javascript_runtime(self):
        with patch.object(main.shutil, "which", side_effect=lambda name: "/bin/ffmpeg" if name == "ffmpeg" else None), \
                patch.object(main, "probe_network") as probe:
            out = await self.health()
        self.assertFalse(out["ok"])
        self.assertIn("Deno", out["message"])
        probe.assert_not_called()

    async def test_health_reports_missing_ejs(self):
        with patch.object(main.shutil, "which", return_value="runtime"), \
                patch("importlib.util.find_spec", return_value=None), patch.object(main, "probe_network") as probe:
            out = await self.health()
        self.assertFalse(out["ok"])
        self.assertIn("yt-dlp-ejs", out["message"])
        probe.assert_not_called()

    async def test_failure_preserves_reason_but_removes_cookie_and_proxy_secrets(self):
        secret = "session-private-value"
        raw = "Sign in to confirm you are not a bot. Cookie SID=" + secret + " proxy=http://alice:p%40ss@proxy.invalid:8080 token=p@ss https://video.invalid/watch?token=private-query"
        with patch.object(main.yt_dlp, "YoutubeDL", side_effect=main.yt_dlp.DownloadError(raw)):
            with self.assertRaises(RuntimeError) as err:
                await main.p.dispatch_buffered({"operation":"video_info", "input":{"url":"abcdefghijk"},
                    "credential":{"cookies":cookies(secret), "proxy":"http://alice:p%40ss@proxy.invalid:8080"}})
        message = str(err.exception)
        self.assertIn("Sign in to confirm", message)
        for private in (secret, "p%40ss", "p@ss", "alice", "private-query"):
            self.assertNotIn(private, message)

    async def test_environment_cookie_secrets_are_also_redacted(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / "cookies.txt"
            path.write_text(cookies("environment-private-value"))
            with patch.dict(os.environ, {"YTDLP_COOKIES":str(path)}), \
                    patch.object(main.yt_dlp, "YoutubeDL", side_effect=main.yt_dlp.DownloadError("Video unavailable: environment-private-value")):
                with self.assertRaises(RuntimeError) as err:
                    await main.p.dispatch_buffered({"operation":"video_info", "input":{"url":"abcdefghijk"}})
        self.assertIn("Video unavailable", str(err.exception))
        self.assertNotIn("environment-private-value", str(err.exception))

    async def test_environment_proxy_password_is_redacted_outside_url(self):
        with patch.dict(os.environ, {"HTTPS_PROXY":"http://proxy-user:proxy-private-password@proxy.invalid"}), \
                patch.object(main.yt_dlp, "YoutubeDL", side_effect=main.yt_dlp.DownloadError("Proxy authentication failed for proxy-user:proxy-private-password")):
            with self.assertRaises(RuntimeError) as err:
                await main.p.dispatch_buffered({"operation":"video_info", "input":{"url":"abcdefghijk"}})
        self.assertIn("Proxy authentication failed", str(err.exception))
        self.assertNotIn("proxy-private-password", str(err.exception))
        self.assertNotIn("proxy-user", str(err.exception))
