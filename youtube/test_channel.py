"""Latest channel releases stay bounded, ordered and metadata-only."""
from pathlib import Path
import unittest
from unittest.mock import patch

import main
from test_credentials import cookies


class ChannelTest(unittest.IsolatedAsyncioTestCase):
    async def call(self, **inputs):
        return await main.p.dispatch_buffered({"operation": "channel_latest", "input": inputs,
                                              "credential": {"cookies": cookies(), "proxy": "http://proxy.invalid"}})

    async def test_latest_preserves_order_and_does_not_download(self):
        opts_seen, urls, paths = [], [], []
        owner = self
        class YDL:
            def __init__(self, opts):
                opts_seen.append(opts)
                path = Path(opts["cookiefile"])
                paths.append(path)
                owner.assertEqual(path.read_text(), cookies())
            def __enter__(self):
                return self
            def __exit__(self, *args):
                pass
            def extract_info(self, url, download=False):
                owner.assertFalse(download)
                urls.append(url)
                return {"_type": "playlist", "channel": "Example", "channel_id": "UCexample", "entries": [
                    {"id": "abcdefghijk", "title": "Newest", "duration": 12.5, "view_count": 25,
                     "thumbnails": [{"url": "https://example.invalid/thumb.jpg"}]},
                    {"id": "12345678901", "title": "Previous", "upload_date": "20261020"},
                    {"id": "abcdefghij2", "title": "Beyond limit"},
                ]}
        with patch.object(main.yt_dlp, "YoutubeDL", YDL):
            out = await self.call(channel="@example", limit=2)
        self.assertEqual(urls, ["https://www.youtube.com/@example/videos"])
        self.assertEqual(opts_seen[0]["playlistend"], 2)
        self.assertEqual(opts_seen[0]["extract_flat"], "in_playlist")
        self.assertEqual(opts_seen[0]["proxy"], "http://proxy.invalid")
        self.assertEqual(out["channel_name"], "Example")
        self.assertEqual(out["count"], 2)
        self.assertEqual([v["title"] for v in out["videos"]], ["Newest", "Previous"])
        self.assertEqual(out["videos"][0]["url"], "https://www.youtube.com/watch?v=abcdefghijk")
        self.assertEqual(out["videos"][0]["upload_date"], "")
        self.assertEqual(out["videos"][1]["upload_date"], "2026-10-20")
        self.assertTrue(all(not path.exists() for path in paths))

    async def test_channel_tabs_and_empty_result(self):
        for channel, tab, expected in [
            ("https://youtube.com/@example?view=0", "shorts", "https://www.youtube.com/@example/shorts"),
            ("https://www.youtube.com/channel/UCexample/videos", "streams", "https://www.youtube.com/channel/UCexample/streams"),
            ("https://www.youtube.com/c/example/", "videos", "https://www.youtube.com/c/example/videos"),
        ]:
            with self.subTest(tab=tab), patch.object(main.yt_dlp, "YoutubeDL") as ydl:
                ydl.return_value.__enter__.return_value.extract_info.return_value = {"_type": "playlist", "entries": []}
                out = await self.call(channel=channel, content_type=tab)
                self.assertEqual(out["videos"], [])
                self.assertEqual(out["count"], 0)
                self.assertEqual(ydl.call_args.args[0]["playlistend"], 10)
                self.assertEqual(ydl.return_value.__enter__.return_value.extract_info.call_args.args, (expected,))

    async def test_rejects_non_channel_inputs_and_unbounded_limits(self):
        for inputs in ({"channel": "https://evil.invalid/@example"}, {"channel": "https://www.youtube.com/watch?v=abcdefghijk"},
                       {"channel": ""}, {"channel": "@example", "limit": 0}, {"channel": "@example", "limit": 51},
                       {"channel": "https://youtube.com.evil.invalid/@example"}):
            with self.subTest(inputs=inputs), patch.object(main.yt_dlp, "YoutubeDL") as ydl:
                with self.assertRaises(ValueError):
                    await self.call(**inputs)
                ydl.assert_not_called()

    async def test_unavailable_entries_are_not_fake_videos(self):
        with patch.object(main.yt_dlp, "YoutubeDL") as ydl:
            ydl.return_value.__enter__.return_value.extract_info.return_value = {
                "_type": "playlist", "entries": [None, {"_type": "unavailable"}, {"id": "abcdefghijk", "title": "Available"}]}
            out = await self.call(channel="@example")
        self.assertEqual(out["count"], 1)
        self.assertEqual(out["videos"][0]["title"], "Available")
