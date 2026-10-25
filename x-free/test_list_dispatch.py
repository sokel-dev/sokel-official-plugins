"""List SDK dispatch and parser regressions using only fake HTTP responses."""

import json
import unittest
from unittest.mock import patch
from urllib.parse import parse_qs, urlsplit

import requests

import main
from test_main import credential, response, tweet, user
from test_list_timeline import cursor_entry, list_body, tweet_entry
from test_my_lists import my_lists_body, twitter_list


class FakeHttpMixin:
    def setUp(self):
        super().setUp()
        self.calls = []
        self.replies = []
        tid_patch = patch.object(main.SessionTweetKit, "_tid", return_value="transaction")
        send_patch = patch.object(requests.Session, "send", side_effect=self.fake_send)
        tid_patch.start()
        send_patch.start()
        self.addCleanup(tid_patch.stop)
        self.addCleanup(send_patch.stop)

    def fake_send(self, request, **kwargs):
        self.calls.append((request, kwargs))
        self.assertTrue(self.replies, "Unexpected followup HTTP request")
        operation, body = self.replies.pop(0)
        self.assertEqual(urlsplit(request.url).path.rsplit("/", 1)[-1], operation)
        return response(body)

    def variables(self, index=0):
        query = parse_qs(urlsplit(self.calls[index][0].url).query)
        return json.loads(query["variables"][0])


class ListDispatchTest(FakeHttpMixin, unittest.IsolatedAsyncioTestCase):
    async def dispatch(self, operation, inputs):
        return await main.p.dispatch_buffered({
            "operation": operation, "input": inputs, "credential": credential().model_dump(),
        })

    async def test_list_timeline_dispatch_preserves_input_and_output_fields(self):
        self.replies = [("ListLatestTweetsTimeline", list_body([tweet("123")], "next-post-page"))]
        result = await self.dispatch("list_timeline", {
            "list_id": "https://twitter.com/i/lists/2114378473250959659/?s=20",
            "limit": 4, "cursor": "current-post-page",
        })
        self.assertEqual(result, {
            "list_id": "2114378473250959659", "count": 1,
            "next_cursor": "next-post-page", "has_more": True,
            "posts": [{"id": "123", "text": "Hello", "url": "https://x.com/reader/status/123",
                       "created_at": "Fri Oct 23 08:00:00 +0000 2026", "author_id": "9",
                       "author_username": "reader", "reply_to_id": "", "like_count": 3,
                       "repost_count": 2, "reply_count": 1}],
        })
        self.assertEqual(self.variables()["listId"], "2114378473250959659")
        self.assertEqual(self.variables()["count"], 4)
        self.assertEqual(self.variables()["cursor"], "current-post-page")
        self.assertEqual(len(self.calls), 1)

    async def test_my_lists_dispatch_preserves_nested_summary_and_pagination(self):
        self.replies = [("ListsManagementPageTimeline", my_lists_body([twitter_list()], "next-list-page"))]
        result = await self.dispatch("my_lists", {"limit": 6, "cursor": "current-list-page"})
        self.assertEqual(result, {
            "lists": [{"id": "123", "name": "AI", "description": "Research updates",
                       "mode": "private", "member_count": 2, "subscriber_count": 0,
                       "owner_id": "42", "owner_username": "example", "url": "https://x.com/i/lists/123"}],
            "count": 1, "next_cursor": "next-list-page", "has_more": True,
        })
        self.assertEqual(self.variables()["count"], 6)
        self.assertEqual(self.variables()["cursor"], "current-list-page")
        self.assertEqual(len(self.calls), 1)

    async def test_add_member_dispatch_resolves_username_and_serializes_result(self):
        await self.check_membership_dispatch("add_list_member", "ListAddMember", False)

    async def test_remove_member_dispatch_resolves_username_and_serializes_result(self):
        await self.check_membership_dispatch("remove_list_member", "ListRemoveMember", True)

    async def check_membership_dispatch(self, operation, graphql_operation, viewer_is_member):
        profile = user()
        profile["core"]["screen_name"] = "Reader"
        self.replies = [
            ("UserByScreenName", {"data": {"user": {"result": profile}}}),
            (graphql_operation, {"data": {"list": {
                "id_str": "2114378473250959659", "name": "Research", "member_count": 2,
                "is_member": viewer_is_member,
            }}}),
        ]
        result = await self.dispatch(operation, {
            "list_id": "https://x.com/i/lists/2114378473250959659", "username": "@reader",
        })
        self.assertEqual(result, {"list_id": "2114378473250959659", "user_id": "9",
                                  "username": "Reader", "changed": True})
        self.assertEqual(self.variables()["screen_name"], "reader")
        self.assertEqual(len(self.calls), 2)
        request = self.calls[1][0]
        self.assertEqual(request.method, "POST")
        self.assertEqual(json.loads(request.body)["variables"],
                         {"listId": "2114378473250959659", "userId": "9"})

    async def test_dispatch_requires_list_id_and_member_username_before_network(self):
        for operation, inputs in (("list_timeline", {}), ("add_list_member", {"list_id": "123"}),
                                  ("remove_list_member", {"username": "reader"})):
            with self.subTest(operation=operation), patch.object(main, "client_for") as factory:
                with self.assertRaises(ValueError):
                    await self.dispatch(operation, inputs)
                factory.assert_not_called()
        self.assertFalse(self.calls)

    async def test_dispatch_default_limits_and_empty_continuation_are_serialized(self):
        for operation, graphql_operation, inputs, body, collection in (
                ("my_lists", "ListsManagementPageTimeline", {}, my_lists_body(), "lists"),
                ("list_timeline", "ListLatestTweetsTimeline", {"list_id": "123"}, list_body(), "posts")):
            with self.subTest(operation=operation):
                self.calls.clear()
                self.replies = [(graphql_operation, body)]
                result = await self.dispatch(operation, inputs)
                self.assertEqual(result[collection], [])
                self.assertEqual(result["count"], 0)
                self.assertEqual(result["next_cursor"], "")
                self.assertIs(result["has_more"], False)
                self.assertEqual(self.variables()["count"], 20)
                self.assertEqual(len(self.calls), 1)


class ListParserRegressionTest(FakeHttpMixin, unittest.TestCase):
    def call_page(self, operation, instructions):
        graphql_operation, make_body, inputs = {
            "list_timeline": ("ListLatestTweetsTimeline", list_body, {"list_id": "123"}),
            "my_lists": ("ListsManagementPageTimeline", my_lists_body, {}),
        }[operation]
        self.replies = [(graphql_operation, make_body(instructions=instructions))]
        return main.execute(operation, inputs, credential())

    def test_list_posts_keep_full_long_note_text_including_visibility_wrapper(self):
        full_text = "Long article paragraph.\n" * 100 + "The final sentence must survive."
        for wrapped in (False, True):
            with self.subTest(wrapped=wrapped):
                self.calls.clear()
                row = tweet("123", text="Short preview…")
                row["note_tweet"] = {"note_tweet_results": {"result": {"text": full_text}}}
                entry = tweet_entry(row)
                if wrapped:
                    entry["content"]["itemContent"]["tweet_results"]["result"] = {
                        "__typename": "TweetWithVisibilityResults", "tweet": row,
                    }
                result = self.call_page("list_timeline", [{"type": "TimelineAddEntries", "entries": [entry]}])
                self.assertEqual(result["posts"][0]["text"], full_text)
                self.assertEqual(len(self.calls), 1)

    def test_deleted_tweet_placeholder_keeps_other_posts_and_pagination(self):
        deleted = {"entryId": "tweet-deleted", "content": {
            "entryType": "TimelineTimelineItem",
            "itemContent": {"itemType": "TimelineTweet", "tweet_results": {}},
        }}
        result = self.call_page("list_timeline", [{"type": "TimelineAddEntries", "entries": [
            tweet_entry(tweet("123")), deleted, tweet_entry(tweet("124")), cursor_entry("older"),
        ]}])
        self.assertEqual([post["id"] for post in result["posts"]], ["123", "124"])
        self.assertEqual(result["next_cursor"], "older")
        self.assertTrue(result["has_more"])

    def test_bottom_termination_ends_pagination_while_top_termination_does_not(self):
        for operation in ("list_timeline", "my_lists"):
            for direction, expected_cursor in (("Bottom", ""), ("Top", "older")):
                with self.subTest(operation=operation, direction=direction):
                    self.calls.clear()
                    result = self.call_page(operation, [
                        {"type": "TimelineAddEntries", "entries": [cursor_entry("older")]},
                        {"type": "TimelineTerminateTimeline", "direction": direction},
                    ])
                    self.assertEqual(result["next_cursor"], expected_cursor)
                    self.assertIs(result["has_more"], bool(expected_cursor))
                    self.assertEqual(len(self.calls), 1)

    def test_module_bottom_cursors_are_preserved_for_both_list_reads(self):
        cursor_item = {"entryId": "module-cursor", "item": {"itemContent": {
            "itemType": "TimelineTimelineCursor", "cursorType": "Bottom", "value": "module-next",
        }}}
        cases = [
            [{"type": "TimelineAddEntries", "entries": [{"entryId": "module", "content": {
                "entryType": "TimelineTimelineModule", "items": [cursor_item],
            }}]}],
            [{"type": "TimelineAddToModule", "moduleEntryId": "module", "moduleItems": [cursor_item]}],
        ]
        for operation in ("list_timeline", "my_lists"):
            for instructions in cases:
                with self.subTest(operation=operation, instruction=instructions[0]["type"]):
                    self.calls.clear()
                    result = self.call_page(operation, instructions)
                    self.assertEqual(result["next_cursor"], "module-next")
                    self.assertIs(result["has_more"], True)
                    self.assertEqual(len(self.calls), 1)

    def test_malformed_known_entries_fail_instead_of_returning_empty_success(self):
        broken_contents = [
            {"entryType": "TimelineTimelineItem"},
            {"entryType": "TimelineTimelineItem", "itemContent": None},
            {"entryType": "TimelineTimelineModule"},
            {"entryType": "TimelineTimelineModule", "items": None},
        ]
        for operation in ("list_timeline", "my_lists"):
            for content in broken_contents:
                with self.subTest(operation=operation, content=content):
                    self.calls.clear()
                    with self.assertRaises(main.XRequestError):
                        self.call_page(operation, [{"type": "TimelineAddEntries", "entries": [
                            {"entryId": "broken-entry", "content": content},
                        ]}])
                    self.assertEqual(len(self.calls), 1)

    def test_declared_tweet_and_list_items_require_their_payload(self):
        for operation, item_type in (("list_timeline", "TimelineTweet"), ("my_lists", "TimelineTwitterList")):
            with self.subTest(operation=operation):
                self.calls.clear()
                with self.assertRaises(main.XRequestError):
                    self.call_page(operation, [{"type": "TimelineAddEntries", "entries": [{
                        "entryId": "missing-payload", "content": {
                            "entryType": "TimelineTimelineItem", "itemContent": {"itemType": item_type},
                        },
                    }]}])
                self.assertEqual(len(self.calls), 1)


if __name__ == "__main__":
    unittest.main()
