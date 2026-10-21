"""Discovery exposes uncertainty and filters only positively identified access."""
import unittest
from unittest.mock import patch
from urllib.parse import parse_qs, urlsplit

import main
from yt_dlp.extractor.youtube import YoutubeTabIE


class DiscoveryTest(unittest.IsolatedAsyncioTestCase):
    async def call(self, operation="channel_latest", entries=None, **inputs):
        with patch.object(main.yt_dlp, "YoutubeDL") as ydl:
            ydl.return_value.__enter__.return_value.extract_info.return_value = {
                "_type": "playlist", "channel": "Example", "channel_id": "UCexample",
                "entries": entries or [],
            }
            result = await main.p.dispatch_buffered({"operation": operation, "input": inputs})
            self.options = ydl.call_args.args[0]
            self.request = ydl.return_value.__enter__.return_value.extract_info.call_args
            return result

    async def test_access_time_and_missing_numeric_metadata(self):
        out = await self.call(channel="@example", entries=[
            {"id": "abcdefghijk", "availability": "subscriber_only", "timestamp": 1792497600,
             "upload_date": "20261020", "release_timestamp": 1792584000, "duration": 0, "view_count": 0,
             "description": "Summary", "channel": "Creator", "channel_id": "UCcreator"},
            {"id": "12345678901"},
        ])
        known, missing = out["videos"]
        self.assertEqual(known.get("availability"), "subscriber_only")
        self.assertEqual(known["published_at"], "2026-10-20T12:00:00Z")
        self.assertEqual(known["published_time_precision"], "approximate")
        self.assertEqual(known["release_at"], "2026-10-21T12:00:00Z")
        self.assertEqual(known["description"], "Summary")
        self.assertEqual(known["channel_name"], "Creator")
        self.assertTrue(known["duration_known"])
        self.assertTrue(known["view_count_known"])
        self.assertEqual(missing["availability"], "unknown")
        self.assertEqual(missing["published_time_precision"], "unknown")
        self.assertEqual(missing["published_at"], "")
        self.assertEqual(missing["channel_name"], "Example")
        self.assertFalse(missing["duration_known"])
        self.assertFalse(missing["view_count_known"])
        self.assertEqual(out["unknown_access_count"], 1)
        self.assertEqual(self.options["extractor_args"]["youtubetab"]["approximate_date"], ["true"])

    async def test_strict_access_filters_scan_beyond_return_limit(self):
        entries = [{"id": "00000000001"},
                   {"id": "00000000002", "availability": "subscriber_only"},
                   {"id": "00000000003", "availability": "public"},
                   {"id": "00000000004", "availability": "unlisted"}]
        for access, expected in [("members_only", "00000000002"), ("public", "00000000003")]:
            with self.subTest(access=access):
                out = await self.call(channel="@example", access_filter=access, limit=1, scan_limit=4, entries=entries)
                self.assertEqual([v["id"] for v in out["videos"]], [expected])
                self.assertEqual(out["scanned_count"], 4)
                self.assertEqual(out["unknown_access_count"], 1)
                self.assertEqual(self.options["playlistend"], 4)

    async def test_unknown_is_not_public_and_empty_is_valid(self):
        out = await self.call(channel="@example", access_filter="public", entries=[{"id": "abcdefghijk"}])
        self.assertEqual(out["videos"], [])
        self.assertEqual(out["unknown_access_count"], 1)

    async def test_upstream_members_badge_reaches_filter(self):
        with main.yt_dlp.YoutubeDL({"quiet": True}) as ydl:
            entry = YoutubeTabIE(ydl)._extract_video({
                "videoId": "jEDgu8GtgzI", "title": {"simpleText": "Members fixture"},
                "badges": [{"metadataBadgeRenderer": {"style": "BADGE_STYLE_TYPE_MEMBERS_ONLY"}}],
            })
        out = await self.call(channel="@example", access_filter="members_only", entries=[entry])
        self.assertEqual(out["count"], 1)
        self.assertEqual(out["videos"][0]["availability"], "subscriber_only")

    async def test_scan_bound_and_return_bound_are_independent(self):
        out = await self.call(channel="@example", access_filter="public", limit=1, scan_limit=3, entries=[
            {"id": "00000000001", "availability": "public", "timestamp": float("inf"), "upload_date": "bad"},
            {"id": "00000000002", "availability": "public"},
            {"id": "00000000003", "availability": "future_status"},
            {"id": "00000000004", "availability": "public"},
        ])
        self.assertEqual(out["count"], 1)
        self.assertEqual(out["scanned_count"], 3)
        self.assertEqual(out["unknown_access_count"], 1)
        self.assertEqual(out["videos"][0]["published_at"], "")
        self.assertEqual(out["videos"][0]["upload_date"], "")

    async def test_global_search_is_registered(self):
        self.assertIn("search", main.p._ops)

    async def test_date_sort_orders_the_scan_window_before_return_limit(self):
        out = await self.call(operation="search", query="test", sort="date", limit=2, scan_limit=5, entries=[
            {"id": "00000000001"},
            {"id": "00000000002", "upload_date": "20261019"},
            {"id": "00000000003", "timestamp": 1792497600},
            {"id": "00000000004", "upload_date": "20261018"},
        ])
        self.assertEqual([v["id"] for v in out["videos"]], ["00000000003", "00000000002"])
        self.assertEqual(self.options["playlistend"], 5)
        self.assertEqual(out["scanned_count"], 4)

    async def test_global_search_query_is_literal_and_video_only(self):
        query = "猫 & sp=evil # https://example.invalid/"
        for sort, params in [("relevance", "EgIQAfABAQ=="), ("date", "CAISAhAB")]:
            with self.subTest(sort=sort):
                out = await self.call(operation="search", query=query, sort=sort, limit=5, entries=[
                    {"id": "UCnotavideo", "ie_key": "YoutubeTab"},
                    {"id": "abcdefghijk", "channel": "Creator", "channel_id": "UCcreator", "availability": "public"},
                    {"id": "abcdefghijk", "title": "Duplicate"},
                    {"id": "12345678901", "upload_date": "20261020"},
                ])
                qs = parse_qs(urlsplit(self.request.args[0]).query)
                self.assertEqual(qs, {"search_query": [query], "sp": [params]})
                self.assertFalse(self.request.kwargs["download"])
                self.assertEqual(self.options["extract_flat"], "in_playlist")
                self.assertEqual(out["query"], query)
                by_id = {video["id"]: video for video in out["videos"]}
                self.assertEqual(by_id["abcdefghijk"]["channel_name"], "Creator")
                self.assertEqual(out["count"], 2)
                self.assertEqual(by_id["12345678901"]["published_time_precision"], "date")

    async def test_invalid_inputs_do_not_extract(self):
        for operation, inputs in [
            ("search", {"query": " "}), ("search", {"query": "a" * 501}),
            ("search", {"query": "test", "sort": "bad"}),
            ("search", {"query": "test", "limit": 51}),
            ("channel_latest", {"channel": "@test", "scan_limit": 51}),
            ("channel_latest", {"channel": "@test", "access_filter": "bad"}),
            ("channel_latest", {"channel": "@test", "access_filter": "public", "limit": 10, "scan_limit": 2}),
        ]:
            with self.subTest(inputs=inputs), patch.object(main.yt_dlp, "YoutubeDL") as ydl:
                with self.assertRaises(ValueError):
                    await main.p.dispatch_buffered({"operation": operation, "input": inputs})
                ydl.assert_not_called()
