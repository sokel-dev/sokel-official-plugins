"""Regressions for an observed partial ListsManagementPageTimeline response."""

import threading
import unittest
from unittest.mock import patch

import requests

import main
from test_main import credential, response


def banner_error(index=0):
    return {
        "code": 214, "kind": "Validation", "name": "BadRequestError",
        "extensions": {"code": 214, "kind": "Validation", "name": "BadRequestError"},
        "message": "BadRequest: com.twitter.strato.serialization.DecodeException",
        "path": ["viewer", "list_management_timeline", "timeline", "instructions", 3,
                 "entries", 0, "content", "items", index, "item", "itemContent", "list",
                 "default_banner_media_results", "result"],
    }


def partial_lists_body():
    """Minimal structural extract of the redacted real HTTP 200 response.

    X returned complete List records alongside code 214 errors for each optional
    default banner result. Personal content and unrelated profile fields are omitted.
    """
    items = [{
        "entryId": f"owned-subscribed-list-module-1-list-{list_id}",
        "item": {
            "clientEventInfo": {"component": "suggest_owned_subscribed_list"},
            "itemContent": {
                "itemType": "TimelineTwitterList", "__typename": "TimelineTwitterList",
                "displayType": "ListWithPin",
                "list": {
                    "id_str": list_id, "name": "Fixture", "mode": "public",
                    "following": True, "default_banner_media_results": {},
                    "user_results": {"result": {
                        "__typename": "User", "rest_id": "42", "core": {"screen_name": "fixture"},
                    }},
                },
            },
        },
    } for list_id in ("123", "124")]
    return {
        "data": {"viewer": {"list_management_timeline": {"timeline": {"instructions": [
            {"type": "TimelineClearCache"},
            {"type": "TimelineTerminateTimeline", "direction": "Top"},
            {"type": "TimelineTerminateTimeline", "direction": "Bottom"},
            {"type": "TimelineAddEntries", "entries": [{
                "entryId": "owned-subscribed-list-module-1",
                "content": {
                    "entryType": "TimelineTimelineModule", "displayType": "Vertical",
                    "clientEventInfo": {"component": "suggest_owned_subscribed_list"},
                    "items": items,
                },
            }, {"entryId": "cursor-bottom-1", "content": {
                "entryType": "TimelineTimelineCursor", "cursorType": "Bottom", "value": "older",
            }}]},
        ]}}}},
        "errors": [banner_error(0), banner_error(1)],
    }


class PartialGraphqlTest(unittest.TestCase):
    def setUp(self):
        self.calls = []
        self.body = partial_lists_body()
        tid_patch = patch.object(main.SessionTweetKit, "_tid", return_value="transaction")
        send_patch = patch.object(requests.Session, "send", side_effect=self.fake_send)
        tid_patch.start()
        send_patch.start()
        self.addCleanup(tid_patch.stop)
        self.addCleanup(send_patch.stop)

    def fake_send(self, request, **kwargs):
        self.calls.append(request)
        return response(self.body)

    def call(self):
        return main.execute("my_lists", {}, credential())

    def test_observed_banner_errors_keep_lists_and_pagination(self):
        result = self.call()
        self.assertEqual([row["id"] for row in result["lists"]], ["123", "124"])
        self.assertEqual(result["count"], 2)
        self.assertEqual(result["next_cursor"], "older")
        self.assertTrue(result["has_more"])
        self.assertEqual(len(self.calls), 1)

    def test_banner_errors_support_existing_timeline_containers(self):
        cases = [("TimelineAddEntries", False), ("TimelineAddToModule", True)]
        cases += [(kind, module) for kind in ("TimelinePinEntry", "TimelineReplaceEntry")
                  for module in (False, True)]
        for kind, module in cases:
            with self.subTest(kind=kind, module=module):
                self.body = partial_lists_body()
                instructions = self.body["data"]["viewer"]["list_management_timeline"]["timeline"]["instructions"]
                entry = instructions[3]["entries"][0]
                row = entry["content"]["items"][0]
                tail = ["items", 0, "item", "itemContent", "list"]
                if not module:
                    entry = {"entryId": "list-123", "content": {
                        "entryType": "TimelineTimelineItem", "itemContent": row["item"]["itemContent"],
                    }}
                    tail = ["itemContent", "list"]
                if kind == "TimelineAddToModule":
                    instructions[3] = {"type": kind, "moduleEntryId": "owned-subscribed-list-module-1",
                                       "moduleItems": [row]}
                    relative = ["moduleItems", 0, "item", "itemContent", "list"]
                elif kind == "TimelineAddEntries":
                    instructions[3] = {"type": kind, "entries": [entry]}
                    relative = ["entries", 0, "content", *tail]
                else:
                    instructions[3] = {"type": kind, "entry": entry}
                    relative = ["entry", "content", *tail]
                error = banner_error()
                error["path"] = error["path"][:5] + relative + error["path"][-2:]
                self.body["errors"] = [error]
                self.assertIn("123", [value["id"] for value in self.call()["lists"]])

    def test_core_field_and_mixed_errors_still_fail(self):
        paths = [banner_error()["path"][:-2] + [field] for field in ("id_str", "name")]
        paths += [["viewer", "list_management_timeline", "timeline"]]
        for path in paths:
            for mixed in (False, True):
                with self.subTest(path=path, mixed=mixed):
                    error = {**banner_error(), "path": path}
                    self.body = partial_lists_body()
                    self.body["errors"] = [banner_error(), error] if mixed else [error]
                    with self.assertRaises(main.XRequestError):
                        self.call()

    def test_authentication_and_rate_limit_errors_are_never_ignored(self):
        for code, message in ((89, "login session"), (88, "rate limit")):
            with self.subTest(code=code):
                self.body = partial_lists_body()
                self.body["errors"].append({"code": code, "message": "private-token private-csrf"})
                with self.assertRaisesRegex(main.XRequestError, message) as caught:
                    self.call()
                self.assertNotIn("private-token", str(caught.exception))
                self.assertNotIn("private-csrf", str(caught.exception))

    def test_only_the_observed_decode_error_signature_is_allowed(self):
        changes = (
            {"code": 213}, {"code": 214.0}, {"kind": "Other"}, {"name": "OtherError"},
            {"message": "Other failure"}, {"extensions": {"code": 89}},
            {"extensions": {"kind": "Other"}}, {"extensions": {"name": "OtherError"}},
        )
        for change in changes:
            with self.subTest(change=change):
                self.body = partial_lists_body()
                self.body["errors"] = [{**banner_error(), **change}]
                with self.assertRaises(main.XRequestError):
                    self.call()

    def test_banner_path_requires_exact_structure_and_array_indexes(self):
        original = banner_error()["path"]
        paths = [None, {}, "default_banner_media_results.result", ["data", *original],
                 original[:-1], original + ["extra"],
                 original[:-2] + ["custom_banner_media_results", "result"]]
        for index in (4, 6, 9):
            for invalid in (-1, True, "0", 0.0):
                path = original.copy()
                path[index] = invalid
                paths.append(path)
        for path in paths:
            with self.subTest(path=path):
                self.body = partial_lists_body()
                self.body["errors"] = [{**banner_error(), "path": path}]
                with self.assertRaises(main.XRequestError):
                    self.call()

    def test_banner_error_must_resolve_to_a_valid_list_in_a_known_instruction(self):
        for target in ("instruction", "entry_type", "item_type", "id", "instruction_index",
                       "entry_index", "item_index"):
            with self.subTest(target=target):
                self.body = partial_lists_body()
                self.body["errors"] = [banner_error()]
                instruction = self.body["data"]["viewer"]["list_management_timeline"]["timeline"]["instructions"][3]
                content = instruction["entries"][0]["content"]
                item = content["items"][0]["item"]["itemContent"]
                if target == "instruction":
                    instruction["type"] = "UnknownInstruction"
                elif target == "entry_type":
                    content["entryType"] = "TimelineTimelineItem"
                elif target == "item_type":
                    item["itemType"] = "TimelineUser"
                elif target == "id":
                    item["list"]["id_str"] = "invalid"
                else:
                    index = {"instruction_index": 4, "entry_index": 6, "item_index": 9}[target]
                    self.body["errors"][0]["path"][index] = 100
                with self.assertRaises(main.XRequestError):
                    self.call()

    def test_allowed_banner_errors_do_not_bypass_data_or_list_validation(self):
        bodies = [partial_lists_body() for _ in range(4)]
        bodies[0]["data"] = {}
        bodies[1]["data"] = {"wrong": {}}
        bodies[2]["data"]["viewer"]["list_management_timeline"]["timeline"]["instructions"] = {}
        row = bodies[3]["data"]["viewer"]["list_management_timeline"]["timeline"]["instructions"][3]
        del row["entries"][0]["content"]["items"][0]["item"]["itemContent"]["list"]["name"]
        for index, body in enumerate(bodies):
            with self.subTest(index=index):
                self.body = body
                with self.assertRaises(main.XRequestError):
                    self.call()

    def test_other_operations_and_all_writes_still_reject_banner_errors(self):
        cases = [("GET", name) for name in ("ListLatestTweetsTimeline", "UserByScreenName")]
        cases += [("POST", name) for name in (
            "ListsManagementPageTimeline", "CreateTweet", "DeleteTweet", "ListAddMember", "ListRemoveMember",
        )]
        for method, operation in cases:
            with self.subTest(method=method, operation=operation):
                with main.client_for(credential(), "", threading.Event()) as client:
                    with self.assertRaises(main.XRequestError):
                        client._session.request(method, f"https://x.com/i/api/graphql/query/{operation}")

    def test_malformed_error_collections_still_fail(self):
        for errors in ({"code": 214}, [None], ["error"], [banner_error(), {}]):
            with self.subTest(errors=errors):
                self.body = partial_lists_body()
                self.body["errors"] = errors
                with self.assertRaises(main.XRequestError):
                    self.call()


if __name__ == "__main__":
    unittest.main()
