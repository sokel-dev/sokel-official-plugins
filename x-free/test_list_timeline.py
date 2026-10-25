"""List timeline contracts exercised at HTTP boundary without network access."""

import json
import threading
import unittest
from unittest.mock import patch
from urllib.parse import parse_qs, urlsplit

import requests

import main
from test_main import credential, response, tweet


def tweet_item(row, **extra):
    return {"itemType": "TimelineTweet", "tweet_results": {"result": row}, **extra}


def tweet_entry(row, **extra):
    return {"entryId": "tweet-" + row["rest_id"], "content": {
        "entryType": "TimelineTimelineItem", "itemContent": tweet_item(row, **extra),
    }}


def cursor_entry(value, kind="Bottom"):
    return {"entryId": "cursor-" + kind.lower(), "content": {
        "entryType": "TimelineTimelineCursor", "cursorType": kind, "value": value,
    }}


def module_item(row, **extra):
    return {"entryId": "module-tweet-" + row["rest_id"],
            "item": {"itemContent": tweet_item(row, **extra)}}


def list_body(rows=(), cursor="", *, instructions=None):
    if instructions is None:
        entries = [tweet_entry(row) for row in rows]
        if cursor:
            entries.append(cursor_entry(cursor))
        instructions = [{"type": "TimelineAddEntries", "entries": entries}]
    return {"data": {"list": {"tweets_timeline": {"timeline": {
        "instructions": instructions,
    }}}}}


class ListTimelineTest(unittest.TestCase):
    def setUp(self):
        self.calls = []
        self.replies = []
        tid_patch = patch.object(main.SessionTweetKit, "_tid",
                                 side_effect=lambda method, path: method + "-transaction")
        send_patch = patch.object(requests.Session, "send", side_effect=self.fake_send)
        self.tid = tid_patch.start()
        self.send = send_patch.start()
        self.addCleanup(tid_patch.stop)
        self.addCleanup(send_patch.stop)

    def fake_send(self, request, **kwargs):
        self.calls.append((request, kwargs))
        if not self.replies:
            raise AssertionError("Unexpected extra HTTP request for a single list page")
        reply = self.replies.pop(0)
        if isinstance(reply, Exception):
            raise reply
        return reply

    def call(self, **inputs):
        return main.execute("list_timeline", {"list_id": "12345", **inputs}, credential())

    def variables(self, call_index=0):
        query = parse_qs(urlsplit(self.calls[call_index][0].url).query)
        return json.loads(query["variables"][0])

    def test_get_single_page_uses_existing_auth_transaction_and_guard(self):
        self.replies = [response(list_body([tweet("100", in_reply_to_status_id_str="42")], "next-page"))]
        result = self.call(limit=7, cursor="input/page+token==")
        self.assertEqual(result["list_id"], "12345")
        self.assertEqual(result["count"], 1)
        self.assertEqual(result["next_cursor"], "next-page")
        self.assertIs(result["has_more"], True)
        self.assertEqual(result["posts"][0]["id"], "100")
        self.assertEqual(result["posts"][0]["author_username"], "reader")
        self.assertEqual(result["posts"][0]["reply_to_id"], "42")
        self.assertEqual(result["posts"][0]["like_count"], 3)
        self.assertEqual(len(self.calls), 1)
        request, options = self.calls[0]
        path = urlsplit(request.url).path
        self.assertEqual(request.method, "GET")
        self.assertRegex(path, r"^/i/api/graphql/[^/]+/ListLatestTweetsTimeline$")
        self.assertEqual(urlsplit(request.url).hostname, "x.com")
        self.assertEqual(self.variables()["listId"], "12345")
        self.assertEqual(self.variables()["count"], 7)
        self.assertEqual(self.variables()["cursor"], "input/page+token==")
        self.assertEqual(request.headers["cookie"], "auth_token=private-token; ct0=private-csrf")
        self.assertEqual(request.headers["x-csrf-token"], "private-csrf")
        self.assertEqual(request.headers["x-client-transaction-id"], "GET-transaction")
        self.tid.assert_called_once_with("GET", path)
        self.assertFalse(options["allow_redirects"])
        self.assertGreater(options["timeout"], 0)
        self.assertLessEqual(options["timeout"], main.REQUEST_TIMEOUT)

    def test_accepts_list_ids_and_x_or_twitter_urls(self):
        for value in ("12345", "https://x.com/i/lists/12345",
                      "https://x.com/i/lists/12345/?s=20",
                      "https://twitter.com/i/lists/12345?ref=profile",
                      "https://twitter.com/i/lists/12345/"):
            with self.subTest(value=value):
                self.calls.clear()
                self.replies = [response(list_body())]
                result = self.call(list_id=value)
                self.assertEqual(result["list_id"], "12345")
                self.assertEqual(self.variables()["listId"], "12345")
                self.assertEqual(len(self.calls), 1)

    def test_default_and_boundary_limits(self):
        for inputs, expected in (({}, 20), ({"limit": 1}, 1), ({"limit": 100}, 100)):
            with self.subTest(inputs=inputs):
                self.calls.clear()
                self.replies = [response(list_body())]
                self.call(**inputs)
                self.assertEqual(self.variables()["count"], expected)

    def test_page_is_never_truncated_to_requested_limit(self):
        self.replies = [response(list_body([tweet("100"), tweet("101"), tweet("102")], "after-102"))]
        result = self.call(limit=1)
        self.assertEqual([row["id"] for row in result["posts"]], ["100", "101", "102"])
        self.assertEqual(result["count"], 3)
        self.assertEqual(result["next_cursor"], "after-102")
        self.assertEqual(len(self.calls), 1)

    def test_add_replace_and_module_instructions_preserve_order_and_deduplicate(self):
        instructions = [
            {"type": "TimelineAddEntries", "entries": [
                tweet_entry(tweet("100")),
                {"entryId": "module-one", "content": {
                    "entryType": "TimelineTimelineModule",
                    "items": [module_item(tweet("101")), module_item(tweet("100"))],
                }},
            ]},
            {"type": "TimelineReplaceEntry", "entryIdToReplace": "old-entry",
             "entry": tweet_entry(tweet("102"))},
            {"type": "TimelineAddToModule", "moduleEntryId": "module-one",
             "moduleItems": [module_item(tweet("103")), module_item(tweet("101"))]},
            {"type": "TimelineReplaceEntry", "entryIdToReplace": "cursor-bottom",
             "entry": cursor_entry("module-next")},
        ]
        self.replies = [response(list_body(instructions=instructions))]
        result = self.call()
        self.assertEqual([row["id"] for row in result["posts"]], ["100", "101", "102", "103"])
        self.assertEqual(result["count"], 4)
        self.assertEqual(result["next_cursor"], "module-next")
        self.assertIs(result["has_more"], True)
        self.assertEqual(len(self.calls), 1)

    def test_only_real_timeline_items_are_posts_and_ads_or_related_tweets_are_excluded(self):
        related = tweet("100")
        related["quoted_status_result"] = {"result": tweet("900")}
        related["legacy"]["retweeted_status_result"] = {"result": tweet("901")}
        entries = [
            tweet_entry(related),
            tweet_entry(tweet("800"), promotedMetadata={"advertiser_id": "9"}),
            {"entryId": "module", "content": {
                "entryType": "TimelineTimelineModule", "items": [
                    module_item(tweet("101")),
                    module_item(tweet("801"), promotedMetadata={"advertiser_id": "9"}),
                ],
            }},
            {"entryId": "recommendation", "content": {
                "entryType": "TimelineTimelineItem",
                "itemContent": {"itemType": "TimelineUser", "user_results": {
                    "result": {"pinned_tweet": {"tweet_results": {"result": tweet("902")}}},
                }},
            }},
            {"entryId": "unavailable", "content": {
                "entryType": "TimelineTimelineItem",
                "itemContent": tweet_item({"__typename": "TweetUnavailable"}),
            }},
        ]
        body = list_body(instructions=[{"type": "TimelineAddEntries", "entries": entries}])
        body["data"]["list"]["metadata"] = {"tweet_results": {"result": tweet("903")}}
        self.replies = [response(body)]
        result = self.call()
        self.assertEqual([row["id"] for row in result["posts"]], ["100", "101"])
        self.assertEqual(result["count"], 2)

    def test_visibility_wrapper_preserves_tweet_fields(self):
        row = tweet("100")
        entry = tweet_entry(row)
        entry["content"]["itemContent"]["tweet_results"]["result"] = {
            "__typename": "TweetWithVisibilityResults", "tweet": row,
        }
        self.replies = [response(list_body(instructions=[{"type": "TimelineAddEntries", "entries": [entry]}]))]
        result = self.call()
        self.assertEqual(result["posts"][0]["id"], "100")
        self.assertEqual(result["posts"][0]["text"], "Hello")

    def test_only_bottom_cursor_controls_continuation(self):
        for entries, expected in (([cursor_entry("newer", "Top")], ""),
                                  ([cursor_entry("older"), cursor_entry("newer", "Top")], "older")):
            with self.subTest(expected=expected):
                self.calls.clear()
                self.replies = [response(list_body(instructions=[{"type": "TimelineAddEntries", "entries": entries}]))]
                result = self.call()
                self.assertEqual(result["next_cursor"], expected)
                self.assertIs(result["has_more"], bool(expected))
                self.assertEqual(len(self.calls), 1)

    def test_empty_page_with_cursor_is_continuable_and_empty_without_is_finished(self):
        for cursor in ("empty-page-next", ""):
            with self.subTest(cursor=cursor):
                self.calls.clear()
                self.replies = [response(list_body(cursor=cursor))]
                self.assertEqual(self.call(), {"list_id": "12345", "posts": [], "count": 0,
                                               "next_cursor": cursor, "has_more": bool(cursor)})
                self.assertEqual(len(self.calls), 1)

    def test_opaque_cursor_and_maximum_length_round_trip(self):
        for cursor in ("", "opaque/+==%&?token", "x" * 4096):
            with self.subTest(length=len(cursor)):
                self.calls.clear()
                self.replies = [response(list_body())]
                self.call(cursor=cursor)
                self.assertEqual(self.variables().get("cursor", ""), cursor)

    def test_repeated_cursor_is_actionable_failure_without_followup_request(self):
        self.replies = [response(list_body([tweet("100")], "private-repeated-cursor"))]
        with self.assertRaisesRegex(main.XRequestError, "(?i)cursor|pagination|continuation") as caught:
            self.call(cursor="private-repeated-cursor")
        self.assertNotIn("private-repeated-cursor", str(caught.exception))
        self.assertEqual(len(self.calls), 1)

    def test_invalid_list_ids_are_rejected_before_client_creation(self):
        invalid = ("", "abc", "123x", "-1", "1.2", None, True,
                   "https://evil.invalid/i/lists/12345", "https://x.com.evil.invalid/i/lists/12345",
                   "https://x.com@evil.invalid/i/lists/12345", "http://x.com/i/lists/12345",
                   "https://x.com/i/lists/", "https://x.com/reader/lists/12345",
                   "https://x.com/i/lists/12345/members")
        for value in invalid:
            with self.subTest(value=value), patch.object(main, "client_for") as factory:
                with self.assertRaisesRegex(ValueError, "(?i)list"):
                    self.call(list_id=value)
                factory.assert_not_called()
        self.assertFalse(self.calls)

    def test_invalid_limits_are_rejected_before_client_creation(self):
        for value in (0, 101, -1, 1.5, "20", True, None):
            with self.subTest(value=value), patch.object(main, "client_for") as factory:
                with self.assertRaisesRegex(ValueError, "limit"):
                    self.call(limit=value)
                factory.assert_not_called()
        self.assertFalse(self.calls)

    def test_invalid_cursors_are_rejected_before_client_creation(self):
        for value in (None, 123, False, [], "x" * 4097, "a\x00b", "a\nb", "a\rb", "a\tb", "a\x7fb", "a\x85b"):
            with self.subTest(value=repr(value)[:60]), patch.object(main, "client_for") as factory:
                with self.assertRaisesRegex(ValueError, "cursor"):
                    self.call(cursor=value)
                factory.assert_not_called()
        self.assertFalse(self.calls)

    def test_missing_list_id_is_rejected_before_client_creation(self):
        with patch.object(main, "client_for") as factory:
            with self.assertRaisesRegex(ValueError, "(?i)list"):
                main.execute("list_timeline", {}, credential())
            factory.assert_not_called()

    def test_null_list_or_unknown_timeline_schema_is_not_empty_success(self):
        for body in ({"data": {"list": None}}, {"data": {"unexpected": {}}},
                     {"data": {"list": {}}},
                     {"data": {"list": {"tweets_timeline": {"timeline": {"instructions": {}}}}}},
                     {"data": {"list": {"tweets_timeline": {"timeline": {}}}}}):
            with self.subTest(body=body):
                self.calls.clear()
                self.replies = [response(body)]
                with self.assertRaises(main.XRequestError):
                    self.call()
                self.assertEqual(len(self.calls), 1)

    def test_empty_instructions_are_a_valid_finished_page(self):
        self.replies = [response(list_body(instructions=[]))]
        self.assertEqual(self.call(), {"list_id": "12345", "posts": [], "count": 0,
                                       "next_cursor": "", "has_more": False})

    def test_http_failures_are_sanitized_and_never_retried(self):
        for status, message in ((403, "login session"), (429, "rate limit"),
                                (404, "unavailable"), (500, "unexpected")):
            with self.subTest(status=status):
                self.calls.clear()
                self.replies = [response({"error": "private-token private-csrf proxy-password"}, status)]
                with self.assertRaisesRegex(main.XRequestError, message) as caught:
                    self.call()
                for secret in ("private-token", "private-csrf", "proxy-password"):
                    self.assertNotIn(secret, str(caught.exception))
                self.assertEqual(len(self.calls), 1)

    def test_graphql_error_does_not_become_empty_success(self):
        for code, message in ((88, "rate limit"), (89, "login session")):
            with self.subTest(code=code):
                self.calls.clear()
                self.replies = [response({"errors": [{"code": code, "message": "private-token"}]})]
                with self.assertRaisesRegex(main.XRequestError, message) as caught:
                    self.call()
                self.assertNotIn("private-token", str(caught.exception))
                self.assertEqual(len(self.calls), 1)

    def test_redirect_is_rejected_without_following_location(self):
        reply = response({}, 302)
        reply.headers["Location"] = "https://evil.invalid/collect"
        self.replies = [reply]
        with self.assertRaises(main.XRequestError):
            self.call()
        self.assertEqual(len(self.calls), 1)
        self.assertFalse(self.calls[0][1]["allow_redirects"])

    def test_transport_error_closes_session_and_hides_credentials(self):
        original_close = main.GuardedSession.close
        self.replies = [requests.Timeout("private-token private-csrf proxy-password")]
        with patch.object(main.GuardedSession, "close", autospec=True, side_effect=original_close) as close:
            with self.assertRaisesRegex(main.XRequestError, "timed out") as caught:
                self.call()
            close.assert_called_once()
        self.assertNotIn("private-", str(caught.exception))
        self.assertNotIn("proxy-password", str(caught.exception))
        self.assertEqual(len(self.calls), 1)

    def test_cancelled_page_stops_before_http(self):
        cancelled = threading.Event()
        cancelled.set()
        with self.assertRaisesRegex(main.XRequestError, "timed out"):
            main.execute("list_timeline", {"list_id": "12345"}, credential(), cancelled)
        self.assertFalse(self.calls)


if __name__ == "__main__":
    unittest.main()
