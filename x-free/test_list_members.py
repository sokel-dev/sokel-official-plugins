"""List membership transport contracts; no real X account is contacted."""

import json
import threading
import unittest
from unittest.mock import patch

import requests

import main
from list_members import change_member
from test_main import credential, operation_body, response, user


def membership_body(**changes):
    # is_member concerns the logged-in viewer, not the requested target user.
    return {"data": {"list": {
        "id_str": "123", "name": "Research", "member_count": 2,
        "is_member": False, **changes,
    }}}


class ListMemberTest(unittest.TestCase):
    def setUp(self):
        self.calls = []
        self.replies = []
        self.cancelled = threading.Event()
        self.tid = patch.object(
            main.SessionTweetKit, "_tid",
            side_effect=lambda method, path: method + "-transaction",
        ).start()
        patch.object(requests.Session, "send", side_effect=self.send).start()
        self.addCleanup(patch.stopall)

    def send(self, request, **kwargs):
        self.calls.append((request, kwargs))
        result = self.replies.pop(0)
        if isinstance(result, Exception):
            raise result
        return result

    def call(self, operation="add_list_member", username="reader", proxy=""):
        with main.client_for(credential(), proxy, self.cancelled) as client:
            return change_member(client, operation, "123", username, main.request_error)

    def successful_replies(self, **changes):
        self.replies = [response(operation_body("UserByScreenName")),
                        response(membership_body(**changes))]

    def test_add_resolves_username_and_posts_once_in_same_private_session(self):
        self.successful_replies()
        proxy = "http://proxyuser:proxypass@proxy.invalid:8080"
        self.assertEqual(self.call(proxy=proxy), {
            "list_id": "123", "user_id": "9", "username": "reader", "changed": True,
        })
        self.assertEqual(len(self.calls), 2)
        lookup, mutation = [item[0] for item in self.calls]
        self.assertEqual(lookup.method, "GET")
        self.assertIn("/UserByScreenName?", lookup.url)
        self.assertEqual(mutation.method, "POST")
        self.assertEqual(mutation.url,
                         "https://x.com/i/api/graphql/-Kf1YVC1rNKkPvPFk7YXZw/ListAddMember")
        body = json.loads(mutation.body)
        self.assertEqual(body["queryId"], "-Kf1YVC1rNKkPvPFk7YXZw")
        self.assertEqual(body["variables"], {"listId": "123", "userId": "9"})
        self.assertTrue(body["features"]["responsive_web_profile_redirect_enabled"])
        self.assertEqual(mutation.headers["content-type"], "application/json")
        self.assertEqual(mutation.headers["x-client-transaction-id"], "POST-transaction")
        self.tid.assert_any_call("POST", "/i/api/graphql/-Kf1YVC1rNKkPvPFk7YXZw/ListAddMember")
        for request, options in self.calls:
            self.assertEqual(request.headers["cookie"], "auth_token=private-token; ct0=private-csrf")
            self.assertEqual(request.headers["x-csrf-token"], "private-csrf")
            self.assertEqual(options["proxies"], {"http": proxy, "https": proxy})
            self.assertFalse(options["allow_redirects"])
            self.assertGreater(options["timeout"], 0)
            self.assertLessEqual(options["timeout"], main.REQUEST_TIMEOUT)

    def test_remove_uses_target_id_and_does_not_confuse_viewer_membership(self):
        self.successful_replies(is_member=True, member_count=0)
        self.assertTrue(self.call("remove_list_member")["changed"])
        mutation = self.calls[-1][0]
        self.assertEqual(mutation.url,
                         "https://x.com/i/api/graphql/_2o1-wSMWGFSi5HUscn1Hw/ListRemoveMember")
        self.assertEqual(json.loads(mutation.body)["variables"], {"listId": "123", "userId": "9"})
        self.assertEqual(mutation.headers["x-client-transaction-id"], "POST-transaction")
        self.assertEqual(len(self.calls), 2)

    def test_canonical_username_case_is_preserved(self):
        profile = user()
        profile["core"]["screen_name"] = "Reader"
        self.replies = [response({"data": {"user": {"result": profile}}}),
                        response(membership_body())]
        self.assertEqual(self.call()["username"], "Reader")

    def test_unavailable_or_mismatched_user_never_reaches_mutation(self):
        invalid = [
            {"__typename": "UserUnavailable"},
            {**user(), "rest_id": "not-an-id"},
            {**user(), "rest_id": None},
            {**user(), "core": {"screen_name": "someone_else"}},
            {**user(), "core": {"screen_name": "invalid name"}},
        ]
        for profile in invalid:
            with self.subTest(profile=profile):
                self.calls.clear()
                self.replies = [response({"data": {"user": {"result": profile}}})]
                with self.assertRaises(main.XRequestError):
                    self.call()
                self.assertEqual(len(self.calls), 1)
                self.assertEqual(self.calls[0][0].method, "GET")

    def test_missing_or_unrelated_list_response_is_not_success(self):
        invalid = [
            {"data": {"list": None}},
            {"data": {"list": {}}},
            {"data": {"unexpected": {"id_str": "123"}}},
            membership_body(id_str="456"),
            membership_body(id_str=123),
            membership_body(name=""),
            membership_body(name=None),
            membership_body(member_count=-1),
            membership_body(member_count="2"),
            membership_body(member_count=True),
        ]
        for body in invalid:
            with self.subTest(body=body):
                self.calls.clear()
                self.replies = [response(operation_body("UserByScreenName")), response(body)]
                with self.assertRaises(main.XRequestError):
                    self.call()
                self.assertEqual(len(self.calls), 2)

    def test_graphql_failure_is_not_retried_or_reported_as_success(self):
        body = membership_body()
        body["errors"] = [{"code": 88, "message": "private-token"}]
        self.replies = [response(operation_body("UserByScreenName")), response(body)]
        with self.assertRaisesRegex(main.XRequestError, "rate limit") as caught:
            self.call()
        self.assertNotIn("private-token", str(caught.exception))
        self.assertEqual(len(self.calls), 2)

    def test_http_failures_and_redirects_never_retry_mutation(self):
        for status in [302, 403, 429, 500]:
            with self.subTest(status=status):
                self.calls.clear()
                failed = response(membership_body(), status=status)
                failed.headers["Location"] = "https://untrusted.invalid/collect"
                self.replies = [response(operation_body("UserByScreenName")), failed]
                with self.assertRaises(main.XRequestError):
                    self.call()
                self.assertEqual(len(self.calls), 2)

    def test_write_timeout_or_connection_error_is_never_retried(self):
        for failure in [requests.Timeout("private-token"), requests.ConnectionError("private-csrf")]:
            with self.subTest(failure=type(failure).__name__):
                self.calls.clear()
                self.replies = [response(operation_body("UserByScreenName")), failure]
                with self.assertRaises(type(failure)):
                    self.call()
                self.assertEqual(len(self.calls), 2)

    def test_cancelled_operation_cannot_write(self):
        self.cancelled.set()
        with self.assertRaises(requests.Timeout):
            self.call()
        self.assertFalse(self.calls)

    def test_unknown_operation_cannot_send_any_request(self):
        with self.assertRaises(ValueError):
            self.call("delete_list")
        self.assertFalse(self.calls)


if __name__ == "__main__":
    unittest.main()
