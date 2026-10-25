"""Authenticated list discovery contracts, using synthetic HTTP responses only."""

import json
import threading
import unittest
from unittest.mock import patch
from urllib.parse import parse_qs, urlsplit

import requests

import main
from test_main import credential, response
from test_list_timeline import cursor_entry


def twitter_list(list_id="123", **changes):
    return {"id": "opaque-graphql-id", "id_str": list_id, "name": "AI",
            "description": "Research updates", "mode": "Private", "member_count": 2,
            "subscriber_count": 0, "user_results": {"result": {
                "__typename": "User", "rest_id": "42", "core": {"screen_name": "example"},
            }}, **changes}


def list_item(row):
    return {"itemType": "TimelineTwitterList", "list": row}


def list_entry(row):
    return {"entryId": "list-" + str(row.get("id_str", "unknown")), "content": {
        "entryType": "TimelineTimelineItem", "itemContent": list_item(row),
    }}


def list_module_item(row):
    return {"entryId": "module-list-" + row["id_str"], "item": {"itemContent": list_item(row)}}


def my_lists_body(rows=(), cursor="", *, instructions=None):
    if instructions is None:
        entries = [list_entry(row) for row in rows]
        if cursor:
            entries.append(cursor_entry(cursor))
        instructions = [{"type": "TimelineAddEntries", "entries": entries}]
    return {"data": {"viewer": {"list_management_timeline": {"timeline": {
        "instructions": instructions,
    }}}}}


class MyListsTest(unittest.TestCase):
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
            raise AssertionError("Unexpected extra request while reading one list management page")
        reply = self.replies.pop(0)
        if isinstance(reply, Exception):
            raise reply
        return reply

    def call(self, **inputs):
        return main.execute("my_lists", inputs, credential())

    def variables(self):
        query = parse_qs(urlsplit(self.calls[-1][0].url).query)
        return json.loads(query["variables"][0])

    def test_get_page_uses_count_cursor_and_existing_auth_transport_guards(self):
        self.replies = [response(my_lists_body([twitter_list()], "next-page"))]
        result = self.call(limit=7, cursor="current/page+token==")
        self.assertEqual(result, {"lists": [{
            "id": "123", "name": "AI", "description": "Research updates", "mode": "private",
            "member_count": 2, "subscriber_count": 0, "owner_id": "42", "owner_username": "example",
            "url": "https://x.com/i/lists/123",
        }], "count": 1, "next_cursor": "next-page", "has_more": True})
        self.assertEqual(len(self.calls), 1)
        request, options = self.calls[0]
        path = urlsplit(request.url).path
        self.assertEqual(request.method, "GET")
        self.assertRegex(path, r"^/i/api/graphql/[^/]+/ListsManagementPageTimeline$")
        self.assertEqual(urlsplit(request.url).hostname, "x.com")
        self.assertEqual(self.variables()["count"], 7)
        self.assertEqual(self.variables()["cursor"], "current/page+token==")
        self.assertEqual(request.headers["cookie"], "auth_token=private-token; ct0=private-csrf")
        self.assertEqual(request.headers["x-csrf-token"], "private-csrf")
        self.assertEqual(request.headers["x-client-transaction-id"], "GET-transaction")
        self.tid.assert_called_once_with("GET", path)
        self.assertFalse(options["allow_redirects"])
        self.assertGreater(options["timeout"], 0)
        self.assertLessEqual(options["timeout"], main.REQUEST_TIMEOUT)

    def test_default_and_boundary_limits(self):
        for inputs, count in (({}, 20), ({"limit": 1}, 1), ({"limit": 100}, 100)):
            with self.subTest(inputs=inputs):
                self.calls.clear()
                self.replies = [response(my_lists_body())]
                self.call(**inputs)
                self.assertEqual(self.variables()["count"], count)
                self.assertEqual(len(self.calls), 1)

    def test_full_page_is_not_sliced_to_requested_limit(self):
        self.replies = [response(my_lists_body([twitter_list("100"), twitter_list("101"),
                                              twitter_list("102")], "after-102"))]
        result = self.call(limit=1)
        self.assertEqual([row["id"] for row in result["lists"]], ["100", "101", "102"])
        self.assertEqual(result["count"], 3)
        self.assertEqual(result["next_cursor"], "after-102")
        self.assertEqual(len(self.calls), 1)

    def test_pin_add_replace_and_modules_deduplicate_in_order(self):
        instructions = [
            {"type": "TimelinePinEntry", "entry": list_entry(twitter_list("100"))},
            {"type": "TimelineAddEntries", "entries": [
                {"entryId": "header", "content": {"entryType": "TimelineTimelineItem",
                                                     "itemContent": {"itemType": "TimelineMessagePrompt"}}},
                list_entry(twitter_list("100")),
                {"entryId": "some-module", "content": {
                    "entryType": "TimelineTimelineModule",
                    "items": [list_module_item(twitter_list("101")),
                              list_module_item(twitter_list("102"))],
                }},
                list_entry(twitter_list("103")),
            ]},
            {"type": "TimelineAddToModule", "moduleEntryId": "some-module",
             "moduleItems": [list_module_item(twitter_list("104")),
                             list_module_item(twitter_list("101"))]},
            {"type": "TimelineReplaceEntry", "entryIdToReplace": "old-list",
             "entry": list_entry(twitter_list("105"))},
            {"type": "TimelineReplaceEntry", "entryIdToReplace": "cursor-bottom",
             "entry": cursor_entry("more-lists")},
        ]
        self.replies = [response(my_lists_body(instructions=instructions))]
        result = self.call()
        self.assertEqual([row["id"] for row in result["lists"]],
                         ["100", "101", "102", "103", "104", "105"])
        self.assertEqual(result["count"], 6)
        self.assertEqual(result["next_cursor"], "more-lists")
        self.assertIs(result["has_more"], True)
        self.assertEqual(len(self.calls), 1)

    def test_only_direct_twitter_list_items_are_collected(self):
        body = my_lists_body([twitter_list("100")])
        body["data"]["viewer"]["metadata"] = {"list": twitter_list("900")}
        timeline = body["data"]["viewer"]["list_management_timeline"]["timeline"]
        timeline["instructions"][0]["entries"].append({"entryId": "suggestion", "content": {
            "entryType": "TimelineTimelineItem", "itemContent": {
                "itemType": "TimelineUser", "user_results": {"result": {"list": twitter_list("901")}},
            },
        }})
        self.replies = [response(body)]
        self.assertEqual([row["id"] for row in self.call()["lists"]], ["100"])

    def test_recommendation_modules_are_excluded_regardless_of_position(self):
        owned = {"entryId": "owned-subscribed-list-module-0", "content": {
            "entryType": "TimelineTimelineModule", "items": [
                list_module_item(twitter_list("100")), list_module_item(twitter_list("101")),
            ],
        }}
        for entry_id, metadata in (("list-to-follow-module-0", {}),
                                   ("other-module", {"clientEventInfo": {
                                       "component": "suggest_list_to_follow"}})):
            recommended = {"entryId": entry_id, "content": {
                "entryType": "TimelineTimelineModule", **metadata,
                "items": [list_module_item(twitter_list("900"))],
            }}
            for entries in ([recommended, owned], [owned, recommended]):
                with self.subTest(entry_id=entry_id, first=entries[0]["entryId"]):
                    self.replies = [response(my_lists_body(instructions=[
                        {"type": "TimelineAddEntries", "entries": entries + [cursor_entry("owned-next")]},
                        {"type": "TimelineAddToModule", "moduleEntryId": entry_id,
                         "moduleItems": [list_module_item(twitter_list("901"))]},
                    ]))]
                    result = self.call()
                    self.assertEqual([row["id"] for row in result["lists"]], ["100", "101"])
                    self.assertEqual(result["next_cursor"], "owned-next")

    def test_continuation_keeps_owned_lists_and_ignores_recommended_module_cursor(self):
        self.replies = [response(my_lists_body(instructions=[
            {"type": "TimelineAddToModule", "moduleEntryId": "list-to-follow-module-0",
             "moduleItems": [list_module_item(twitter_list("900")), {
                 "item": {"itemContent": {"itemType": "TimelineTimelineCursor",
                                           "cursorType": "Bottom", "value": "recommendation-next"}},
             }]},
            {"type": "TimelineAddToModule", "moduleEntryId": "owned-subscribed-list-module-0",
             "moduleItems": [list_module_item(twitter_list("100"))]},
        ]))]
        result = self.call(cursor="owned-previous")
        self.assertEqual([row["id"] for row in result["lists"]], ["100"])
        self.assertFalse(result["has_more"])
        self.assertEqual(result["next_cursor"], "")

    def test_recommended_items_are_excluded_without_discarding_the_page_cursor(self):
        suggested = list_module_item(twitter_list("900"))
        suggested["item"]["clientEventInfo"] = {"component": "suggest_list_to_follow"}
        for rows, expected in (([suggested], []),
                               ([suggested, list_module_item(twitter_list("100"))], ["100"])):
            with self.subTest(expected=expected):
                self.replies = [response(my_lists_body(instructions=[
                    {"type": "TimelineAddToModule", "moduleEntryId": "lists-module-0",
                     "moduleItems": rows},
                    {"type": "TimelineAddEntries", "entries": [cursor_entry("owned-next")]},
                ]))]
                result = self.call()
                self.assertEqual([row["id"] for row in result["lists"]], expected)
                self.assertTrue(result["has_more"])
                self.assertEqual(result["next_cursor"], "owned-next")

    def test_id_str_is_used_instead_of_opaque_graphql_id(self):
        self.replies = [response(my_lists_body([twitter_list("2114378473250959659", id="999")]))]
        result = self.call()["lists"][0]
        self.assertEqual(result["id"], "2114378473250959659")
        self.assertEqual(result["url"], "https://x.com/i/lists/2114378473250959659")

    def test_mode_normalization_and_optional_fields(self):
        for raw, expected in (("Private", "private"), ("PUBLIC", "public"),
                              ("unknown-mode", ""), (None, "")):
            with self.subTest(mode=raw):
                self.replies = [response(my_lists_body([twitter_list(
                    mode=raw, description=None, member_count=None, subscriber_count=None,
                    user_results={"result": {"__typename": "UserUnavailable"}},
                )]))]
                row = self.call()["lists"][0]
                self.assertEqual(row["mode"], expected)
                self.assertEqual(row["description"], "")
                self.assertEqual(row["member_count"], 0)
                self.assertEqual(row["subscriber_count"], 0)
                self.assertEqual(row["owner_id"], "")
                self.assertEqual(row["owner_username"], "")

    def test_legacy_owner_username_and_nonnegative_integer_counts(self):
        owner = {"result": {"__typename": "User", "rest_id": "43",
                            "legacy": {"screen_name": "legacy_owner"}}}
        self.replies = [response(my_lists_body([twitter_list(user_results=owner, member_count="12", subscriber_count=-2)]))]
        row = self.call()["lists"][0]
        self.assertEqual(row["owner_id"], "43")
        self.assertEqual(row["owner_username"], "legacy_owner")
        self.assertEqual(row["member_count"], 12)
        self.assertEqual(row["subscriber_count"], 0)

    def test_invalid_ids_or_missing_names_cannot_produce_invalid_list_records(self):
        invalid = [twitter_list(""), twitter_list("opaque"), twitter_list("1" * 21),
                   twitter_list("１２３"), twitter_list(name=None), twitter_list(name=123)]
        no_id = twitter_list()
        no_id.pop("id_str")
        invalid.append(no_id)
        no_name = twitter_list()
        no_name.pop("name")
        invalid.append(no_name)
        for row in invalid:
            with self.subTest(row=row):
                self.calls.clear()
                self.replies = [response(my_lists_body([row]))]
                with self.assertRaises(main.XRequestError):
                    self.call()
                self.assertEqual(len(self.calls), 1)

    def test_empty_page_keeps_cursor_continuation_without_another_request(self):
        for cursor in ("empty-next-page", ""):
            with self.subTest(cursor=cursor):
                self.calls.clear()
                self.replies = [response(my_lists_body(cursor=cursor))]
                self.assertEqual(self.call(), {"lists": [], "count": 0, "next_cursor": cursor,
                                               "has_more": bool(cursor)})
                self.assertEqual(len(self.calls), 1)

    def test_empty_instructions_are_a_valid_finished_page(self):
        self.replies = [response(my_lists_body(instructions=[]))]
        self.assertEqual(self.call(), {"lists": [], "count": 0, "next_cursor": "", "has_more": False})

    def test_top_cursor_is_never_used_as_continuation(self):
        for entries, cursor in (([cursor_entry("newer", "Top")], ""),
                                ([cursor_entry("older"), cursor_entry("newer", "Top")], "older")):
            with self.subTest(cursor=cursor):
                self.replies = [response(my_lists_body(instructions=[{"type": "TimelineAddEntries", "entries": entries}]))]
                result = self.call()
                self.assertEqual(result["next_cursor"], cursor)
                self.assertIs(result["has_more"], bool(cursor))

    def test_repeated_cursor_has_readable_error_and_no_retry(self):
        self.replies = [response(my_lists_body(cursor="private-pagination-token"))]
        with self.assertRaisesRegex(main.XRequestError, "(?i)cursor|pagination|continuation") as caught:
            self.call(cursor="private-pagination-token")
        self.assertNotIn("private-pagination-token", str(caught.exception))
        self.assertEqual(len(self.calls), 1)

    def test_cursor_is_opaque_and_accepts_maximum_length(self):
        for cursor in ("", "opaque/+==%&?token", "x" * 4096):
            with self.subTest(length=len(cursor)):
                self.replies = [response(my_lists_body())]
                self.call(cursor=cursor)
                self.assertEqual(self.variables().get("cursor", ""), cursor)

    def test_invalid_inputs_are_rejected_before_client_creation(self):
        inputs = [{"limit": value} for value in (0, 101, -1, True, 1.5, "20", None)]
        inputs += [{"cursor": value} for value in (None, True, 123, [], "x" * 4097,
                                                   "a\x00b", "a\nb", "a\rb", "a\tb", "a\x7fb", "a\x85b")]
        for values in inputs:
            field = next(iter(values))
            with self.subTest(values=str(values)[:60]), patch.object(main, "client_for") as factory:
                with self.assertRaisesRegex(ValueError, field):
                    self.call(**values)
                factory.assert_not_called()
        self.assertFalse(self.calls)

    def test_missing_or_unknown_management_root_is_not_empty_success(self):
        bodies = ({"data": {"viewer": None}}, {"data": {"wrong": {}}},
                  {"data": {"viewer": {}}},
                  {"data": {"viewer": {"list_management_timeline": {"timeline": {}}}}},
                  {"data": {"viewer": {"list_management_timeline": {"timeline": {"instructions": {}}}}}})
        for body in bodies:
            with self.subTest(body=body):
                self.calls.clear()
                self.replies = [response(body)]
                with self.assertRaises(main.XRequestError):
                    self.call()
                self.assertEqual(len(self.calls), 1)

    def test_http_and_graphql_errors_are_sanitized_and_not_retried(self):
        fixtures = [(response({"error": "private-token private-csrf proxy-password"}, status), message)
                    for status, message in ((403, "login session"), (429, "rate limit"),
                                            (404, "unavailable"), (500, "unexpected"))]
        fixtures += [(response({"errors": [{"code": code, "message": "private-token private-csrf"}]}), message)
                     for code, message in ((88, "rate limit"), (89, "login session"))]
        for reply, message in fixtures:
            with self.subTest(status=reply.status_code, message=message):
                self.calls.clear()
                self.replies = [reply]
                with self.assertRaisesRegex(main.XRequestError, message) as caught:
                    self.call()
                for secret in ("private-token", "private-csrf", "proxy-password"):
                    self.assertNotIn(secret, str(caught.exception))
                self.assertEqual(len(self.calls), 1)

    def test_redirect_is_rejected_without_followup_request(self):
        reply = response({}, 302)
        reply.headers["Location"] = "https://evil.invalid/collect"
        self.replies = [reply]
        with self.assertRaises(main.XRequestError):
            self.call()
        self.assertEqual(len(self.calls), 1)
        self.assertFalse(self.calls[0][1]["allow_redirects"])

    def test_timeout_is_sanitized_and_session_is_closed(self):
        original_close = main.GuardedSession.close
        self.replies = [requests.Timeout("private-token private-csrf proxy-password")]
        with patch.object(main.GuardedSession, "close", autospec=True, side_effect=original_close) as close:
            with self.assertRaisesRegex(main.XRequestError, "timed out") as caught:
                self.call()
            close.assert_called_once()
        self.assertNotIn("private-", str(caught.exception))
        self.assertNotIn("proxy-password", str(caught.exception))
        self.assertEqual(len(self.calls), 1)

    def test_cancelled_operation_stops_before_http(self):
        cancelled = threading.Event()
        cancelled.set()
        with self.assertRaisesRegex(main.XRequestError, "timed out"):
            main.execute("my_lists", {}, credential(), cancelled)
        self.assertFalse(self.calls)


if __name__ == "__main__":
    unittest.main()
