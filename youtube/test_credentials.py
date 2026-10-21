"""Credential behavior through the real SDK dispatch, without upstream requests."""
import asyncio
from pathlib import Path
import os
import threading
import unittest
from unittest.mock import patch

import main
from sokel_gen import CONTRACT
from test_main import Files


def cookies(value="account-a"):
    return "# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t2147483647\tSID\t" + value + "\n"


class CredentialTest(unittest.IsolatedAsyncioTestCase):
    async def call(self, operation="video_info", credential=None):
        return await main.p.dispatch_buffered({
            "operation": operation, "input": {"url": "video", "lang": "en"},
            "credential": credential or {},
        }, files=Files())

    def test_contract_advertises_optional_secrets(self):
        fields = {f["name"]: f for f in CONTRACT.get("credential_schema", [])}
        self.assertIn("cookies", fields)
        self.assertIn("proxy", fields)
        for field in fields.values():
            self.assertFalse(field.get("required", False))
            self.assertEqual(field["type"], "secret")

    async def test_every_operation_receives_credentials_and_cleans_on_error(self):
        for operation in ("video_info", "download", "download_audio", "transcript"):
            with self.subTest(operation=operation):
                paths = []
                def inspect(opts):
                    self.assertEqual(opts.get("proxy"), "http://credential-proxy.invalid")
                    path = Path(opts.get("cookiefile", "/missing"))
                    self.assertTrue(path.is_file(), "credential cookies never reached the extractor")
                    self.assertEqual(path.read_text(), cookies())
                    self.assertEqual(path.stat().st_mode & 0o777, 0o600)
                    paths.append(path)
                    raise RuntimeError("upstream unavailable")
                with patch.dict(os.environ, {"YTDLP_PROXY": "http://environment.invalid", "YTDLP_COOKIES": "/old"}), \
                        patch.object(main.yt_dlp, "YoutubeDL", side_effect=inspect):
                    with self.assertRaisesRegex(RuntimeError, "upstream unavailable"):
                        await self.call(operation, {"cookies": cookies(), "proxy": "http://credential-proxy.invalid"})
                self.assertEqual(len(paths), 1)
                self.assertFalse(paths[0].exists())

    async def test_concurrent_credentials_do_not_share_files(self):
        barrier = threading.Barrier(2)
        paths = []
        class YDL:
            def __init__(self, opts):
                self.path = Path(opts.get("cookiefile", "/missing"))
                paths.append(self.path)
            def __enter__(self):
                return self
            def __exit__(self, *args):
                self.path.write_text(cookies("refreshed"))
            def extract_info(self, url, download=False):
                barrier.wait(timeout=5)
                return {"id": self.path.read_text().splitlines()[-1].split("\t")[-1]}
        with patch.dict(os.environ, {}, clear=True), patch.object(main.yt_dlp, "YoutubeDL", YDL):
            results = await asyncio.gather(self.call(credential={"cookies": cookies("a")}),
                                           self.call(credential={"cookies": cookies("b")}))
        self.assertEqual([r["id"] for r in results], ["a", "b"])
        self.assertNotEqual(paths[0], paths[1])
        self.assertTrue(all(not p.exists() for p in paths))

    async def test_invalid_cookies_fail_without_disclosing_content(self):
        for content in ("SID=private-secret", "# Netscape HTTP Cookie File\nmalformed-private-secret\n"):
            with self.subTest(content=content), patch.object(main.yt_dlp, "YoutubeDL") as ydl:
                with self.assertRaisesRegex(ValueError, "Netscape") as err:
                    await self.call(credential={"cookies": content})
                self.assertNotIn("private-secret", str(err.exception))
                ydl.assert_not_called()

    async def test_anonymous_calls_still_work(self):
        with patch.dict(os.environ, {}, clear=True), patch.object(main.yt_dlp, "YoutubeDL") as ydl:
            ydl.return_value.__enter__.return_value.extract_info.return_value = {"id": "anonymous"}
            self.assertEqual((await self.call())["id"], "anonymous")
            self.assertNotIn("cookiefile", ydl.call_args.args[0])

    async def test_download_and_subtitle_second_stage_keep_credentials(self):
        for operation in ("download", "download_audio", "transcript"):
            with self.subTest(operation=operation):
                paths = []
                owner = self
                class YDL:
                    def __init__(self, opts):
                        self.opts = opts
                        path = Path(opts["cookiefile"])
                        owner.assertEqual(path.read_text(), cookies())
                        paths.append(path)
                    def __enter__(self):
                        return self
                    def __exit__(self, *args):
                        pass
                    def extract_info(self, url, download=False):
                        if download:
                            directory = Path(self.opts["outtmpl"]).parent
                            if operation == "transcript":
                                (directory / "sub.en.vtt").write_text("WEBVTT\n\nHello\n")
                            else:
                                (directory / "video.webm").write_bytes(b"media")
                        return {"id": "video", "subtitles": {"en": []}, "ext": "webm"}
                with patch.object(main.yt_dlp, "YoutubeDL", YDL):
                    await self.call(operation, {"cookies": cookies()})
                self.assertEqual(len(paths), 2)
                self.assertTrue(all(not p.exists() for p in paths))

    async def test_real_extractor_loads_cookie_jar_and_saves_privately(self):
        paths = []
        def inspect(ydl, *args, **kwargs):
            path = Path(ydl.params["cookiefile"])
            paths.append(path)
            self.assertEqual(next(iter(ydl.cookiejar)).value, "account-a")
            return {"id": "video"}
        with patch.object(main.yt_dlp.YoutubeDL, "extract_info", inspect):
            self.assertEqual((await self.call(credential={"cookies": cookies()}))["id"], "video")
        self.assertFalse(paths[0].exists())

    async def test_cookie_file_preserves_empty_last_value(self):
        content = cookies() + ".youtube.com\tTRUE\t/\tTRUE\t2147483647\tEMPTY\t\n"
        def inspect(ydl, *args, **kwargs):
            self.assertEqual(Path(ydl.params["cookiefile"]).read_text(), content)
            return {"id": "video"}
        with patch.object(main.yt_dlp.YoutubeDL, "extract_info", inspect):
            self.assertEqual((await self.call(credential={"cookies": content}))["id"], "video")

    async def test_health_uses_credential_proxy_without_exposing_secrets(self):
        with patch.object(main.shutil, "which", return_value="ffmpeg"), \
                patch.object(main, "probe_network", side_effect=OSError("http://user:private-secret@proxy.invalid")) as probe:
            out = await self.call("health_check", {"proxy": "http://user:private-secret@proxy.invalid"})
        self.assertFalse(out["ok"])
        self.assertNotIn("private-secret", out["message"])
        self.assertEqual(probe.call_args.args, ("http://user:private-secret@proxy.invalid",))

    async def test_upstream_diagnostics_do_not_expose_credentials(self):
        private_url = "http://user:private-secret@proxy.invalid"
        def fail(opts):
            opts["logger"].error(private_url)
            raise main.yt_dlp.DownloadError(private_url)
        with patch.object(main.yt_dlp, "YoutubeDL", side_effect=fail), self.assertNoLogs("yt_dlp"):
            with self.assertRaises(Exception) as err:
                await self.call(credential={"proxy": private_url})
            self.assertNotIn("private-secret", str(err.exception))
            self.assertIn("cookies", str(err.exception))

    async def test_health_rejects_invalid_cookies_before_network(self):
        with patch.object(main.shutil, "which", return_value="ffmpeg"), patch.object(main, "probe_network") as probe:
            out = await self.call("health_check", {"cookies": "invalid-secret"})
        self.assertFalse(out["ok"])
        self.assertIn("Netscape", out["message"])
        self.assertNotIn("invalid-secret", out["message"])
        probe.assert_not_called()
