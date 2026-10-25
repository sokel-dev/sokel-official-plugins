"""Contract and transport tests; never contact X or publish real posts."""

import asyncio
import json
import threading
import unittest
from types import SimpleNamespace
from unittest.mock import patch

import requests

import main


def credential(**changes):
    return main.Credential(**{"auth_token": "private-token", "ct0": "private-csrf", **changes})


def tweet(post_id="123", text="Hello", **legacy):
    return {"__typename": "Tweet", "rest_id": post_id,
            "core": {"user_results": {"result": user()}},
            "legacy": {"id_str": post_id, "full_text": text, "user_id_str": "9",
                       "created_at": "Fri Oct 23 08:00:00 +0000 2026",
                       "favorite_count": 3, "retweet_count": 2, "reply_count": 1, **legacy}}


def user():
    return {"__typename": "User", "rest_id": "9",
            "core": {"screen_name": "reader", "name": "Reader"},
            "legacy": {"description": "Biography", "followers_count": 12, "friends_count": 7}}


def timeline(rows=(), cursor=""):
    entries = [{"content": {"itemContent": {"tweet_results": {"result": row}}}} for row in rows]
    if cursor:
        entries.append({"content": {"cursorType": "Bottom", "value": cursor}})
    return {"instructions": [{"type": "TimelineAddEntries", "entries": entries}]}


def response(body, status=200):
    value = requests.Response()
    value.status_code = status
    value._content = json.dumps(body).encode()
    value._content_consumed = True
    return value


def operation_body(operation, rows=(), cursor=""):
    if operation == "SearchTimeline":
        return {"data": {"search_by_raw_query": {"search_timeline": {"timeline": timeline(rows, cursor)}}}}
    if operation == "UserTweets":
        return {"data": {"user": {"result": {"timeline": {"timeline": timeline(rows, cursor)}}}}}
    if operation == "HomeLatestTimeline":
        return {"data": {"home": {"home_timeline_urt": timeline(rows, cursor)}}}
    if operation == "UserByScreenName":
        return {"data": {"user": {"result": user()}}}
    if operation == "TweetResultByRestId":
        return {"data": {"tweetResult": {"result": next(iter(rows), tweet())}}}
    if operation == "CreateTweet":
        return {"data": {"create_tweet": {"tweet_results": {"result": next(iter(rows), tweet())}}}}
    if operation == "DeleteTweet":
        return {"data": {"delete_tweet": {}}}
    raise AssertionError(operation)


class HttpTest(unittest.TestCase):
    """Exercise the installed dependency down to requests' prepared HTTP boundary."""

    def setUp(self):
        self.calls = []
        self.replies = []
        self.tid = patch.object(main.SessionTweetKit, "_tid", side_effect=lambda method, path: method + "-transaction").start()
        self.send = patch.object(requests.Session, "send", side_effect=self.fake_send).start()
        self.addCleanup(patch.stopall)

    def fake_send(self, request, **kwargs):
        self.calls.append((request, kwargs))
        if self.replies:
            item = self.replies.pop(0)
            if isinstance(item, Exception):
                raise item
            return item
        operation = request.url.split("?", 1)[0].rsplit("/", 1)[-1]
        return response(operation_body(operation, [tweet()]))

    def call(self, operation, **inputs):
        return main.execute(operation, inputs, credential())

    def test_search_uses_real_parser_and_post_transaction(self):
        result = self.call("search", query="from:reader", limit=1)
        self.assertEqual(result["count"], 1)
        self.assertEqual(result["posts"][0]["author_username"], "reader")
        request, options = self.calls[0]
        body = json.loads(request.body)
        self.assertEqual(request.method, "POST")
        self.assertEqual(body["variables"]["product"], "Latest")
        self.assertEqual(body["variables"]["rawQuery"], "from:reader")
        self.assertTrue(body["queryId"])
        self.assertEqual(request.headers["x-client-transaction-id"], "POST-transaction")
        self.assertEqual(request.headers["cookie"], "auth_token=private-token; ct0=private-csrf")
        self.assertEqual(request.headers["x-csrf-token"], "private-csrf")
        self.assertFalse(options["allow_redirects"])
        self.assertGreater(options["timeout"], 0)
        self.assertLessEqual(options["timeout"], 30)

    def test_public_search_paginates_and_stops_at_limit(self):
        self.replies = [response(operation_body("SearchTimeline", [tweet("100")], "next")),
                        response(operation_body("SearchTimeline", [tweet("101")], "another"))]
        result = self.call("search", query="test", limit=2)
        self.assertEqual([row["id"] for row in result["posts"]], ["100", "101"])
        self.assertEqual(len(self.calls), 2)
        self.assertEqual(json.loads(self.calls[1][0].body)["variables"]["cursor"], "next")

    def test_related_posts_are_not_counted_as_separate_search_matches(self):
        first = tweet("100")
        first["quoted_status_result"] = {"result": tweet("200")}
        first["legacy"]["retweeted_status_result"] = {"result": tweet("201")}
        self.replies = [response(operation_body("SearchTimeline", [first, tweet("101")]))]
        result = self.call("search", query="target", limit=2)
        self.assertEqual([row["id"] for row in result["posts"]], ["100", "101"])

    def test_timeline_resolves_username_and_preserves_reply_id(self):
        self.replies = [response(operation_body("UserByScreenName")),
                        response(operation_body("UserTweets", [tweet(in_reply_to_status_id_str="42")]))]
        result = self.call("user_timeline", username="@reader", limit=2)
        self.assertEqual(result["posts"][0]["reply_to_id"], "42")
        self.assertIn("UserByScreenName", self.calls[0][0].url)
        self.assertIn("UserTweets", self.calls[1][0].url)

    def test_get_post_and_get_user_keep_typed_fields(self):
        result = self.call("get_post", id="123")["post"]
        self.assertEqual(result["like_count"], 3)
        self.assertEqual(result["repost_count"], 2)
        self.assertEqual(result["reply_count"], 1)
        result = self.call("get_user", username="reader")["user"]
        self.assertEqual(result, {"id": "9", "username": "reader", "name": "Reader", "description": "Biography",
                                 "followers_count": 12, "following_count": 7, "url": "https://x.com/reader"})

    def test_user_profiles_validate_identity_and_support_current_bio_field(self):
        profile = user()
        profile["legacy"].pop("description")
        profile["profile_bio"] = {"description": "Modern biography"}
        self.replies = [response({"data": {"user": {"result": profile}}})]
        self.assertEqual(self.call("get_user", username="reader")["user"]["description"], "Modern biography")
        for change in ({"rest_id": "invalid"}, {"core": {"screen_name": "invalid/name"}}):
            self.replies = [response({"data": {"user": {"result": {**user(), **change}}}})]
            with self.subTest(change=change), self.assertRaises(main.XRequestError):
                self.call("get_user", username="reader")

    def test_writes_use_real_dependency_without_followup_reads(self):
        self.replies = [response(operation_body("CreateTweet", [tweet(text="Reply", in_reply_to_status_id_str="42")]))]
        result = self.call("post", text="Reply", reply_to="42")["post"]
        self.assertEqual(result["text"], "Reply")
        self.assertEqual(result["reply_to_id"], "42")
        self.assertEqual(result["author_id"], "9")
        self.assertEqual(len(self.calls), 1)
        self.assertEqual(json.loads(self.calls[0][0].body)["variables"]["reply"]["in_reply_to_tweet_id"], "42")
        result = self.call("delete_post", id="123")
        self.assertEqual(result, {"deleted": True, "id": "123"})
        self.assertEqual(len(self.calls), 2)

    def test_empty_authenticated_feed_is_healthy(self):
        self.replies = [response(operation_body("HomeLatestTimeline"))]
        self.assertTrue(self.call("health_check")["ok"])
        self.assertEqual(self.calls[0][0].method, "POST")

    def test_http_errors_are_actionable_private_and_not_retried(self):
        for operation, inputs in [("post", {"text": "Hello"}), ("delete_post", {"id": "123"}),
                                  ("search", {"query": "hello"})]:
            for status, message in [(401, "login session"), (403, "login session"),
                                    (429, "rate limit"), (404, "unavailable"), (500, "unexpected")]:
                with self.subTest(operation=operation, status=status):
                    self.calls.clear()
                    self.replies = [response({"error": "private-token private-csrf proxy-password"}, status)]
                    with self.assertRaisesRegex(main.XRequestError, message) as caught:
                        self.call(operation, **inputs)
                    self.assertNotIn("private-", str(caught.exception))
                    self.assertNotIn("proxy-password", str(caught.exception))
                    self.assertEqual(len(self.calls), 1)

    def test_graphql_errors_are_not_empty_success(self):
        for code, message in [(89, "login session"), (88, "rate limit"), (187, "duplicate"),
                              (186, "length"), (226, "blocked"), (144, "unavailable"), (999, "unexpected")]:
            with self.subTest(code=code):
                self.replies = [response({"errors": [{"code": code, "message": "private-token"}]})]
                with self.assertRaisesRegex(main.XRequestError, message):
                    self.call("search", query="hello")

    def test_delete_and_health_reject_false_success(self):
        for operation, inputs, body in [("delete_post", {"id": "123"}, {}),
                                        ("delete_post", {"id": "123"}, {"data": {"wrong": {}}}),
                                        ("health_check", {}, {"data": {"home": {}}})]:
            with self.subTest(operation=operation, body=body):
                self.replies = [response(body)]
                with self.assertRaises(main.XRequestError):
                    self.call(operation, **inputs)

    def test_unclassified_errors_show_only_numeric_diagnostics(self):
        cases = [(response({"error": "private-token private-csrf"}, 400), "HTTP 400"),
                 (response({"errors": [{"code": 214, "message": "private-token private-csrf"}]}),
                  "HTTP 200; X error codes: 214")]
        for reply, diagnostic in cases:
            with self.subTest(diagnostic=diagnostic):
                self.replies = [reply]
                with self.assertRaises(main.XRequestError) as caught:
                    self.call("search", query="hello")
                self.assertIn(diagnostic, str(caught.exception))
                self.assertNotIn("private-", str(caught.exception))
        message = str(main.request_error("private-token", ["private-csrf", True, 214.0, -1, 10000]))
        self.assertNotIn("HTTP", message)
        self.assertNotIn("codes:", message)
        self.assertNotIn("private-", message)

    def test_list_operations_reject_unavailable_or_unknown_schema(self):
        for body in ({"data": {"search_by_raw_query": None}}, {"data": {"unexpected": {}}}):
            with self.subTest(body=body):
                self.replies = [response(body)]
                with self.assertRaises(main.XRequestError):
                    self.call("search", query="hello")
        self.replies = [response(operation_body("UserByScreenName")),
                        response({"data": {"user": {"result": {"__typename": "UserUnavailable"}}}})]
        with self.assertRaises(main.XRequestError):
            self.call("user_timeline", username="reader")
        for operation in ("SearchTimeline", "UserTweets"):
            self.replies = ([response(operation_body("UserByScreenName"))] if operation == "UserTweets" else [])
            self.replies.append(response(operation_body(operation)))
            result = (self.call("search", query="hello") if operation == "SearchTimeline" else
                      self.call("user_timeline", username="reader"))
            self.assertEqual(result, {"posts": [], "count": 0})

    def test_transport_failures_are_sanitized_and_closed(self):
        original_close = main.GuardedSession.close
        for error, message in [(requests.Timeout("private-token"), "timed out"),
                               (requests.exceptions.ProxyError("proxy-password"), "Cannot connect"),
                               (RuntimeError("private-csrf"), "unexpected")]:
            with self.subTest(error=error), patch.object(main.GuardedSession, "close", autospec=True, side_effect=original_close) as close:
                self.replies = [error]
                with self.assertRaisesRegex(main.XRequestError, message) as caught:
                    self.call("post", text="hello")
                self.assertNotIn("private", str(caught.exception))
                self.assertNotIn("proxy-password", str(caught.exception))
                close.assert_called_once()

    def test_proxy_and_cookies_are_private_to_each_call(self):
        with patch.dict("os.environ", {"HTTPS_PROXY": "http://env.invalid:1234"}):
            for token, proxy in [("one", "http://proxyuser:proxypass@proxy.invalid:8080"), ("two", "")]:
                main.execute("get_post", {"id": "123"}, credential(auth_token=token, proxy=proxy))
        first, second = self.calls
        self.assertIn("auth_token=one", first[0].headers["cookie"])
        self.assertIn("auth_token=two", second[0].headers["cookie"])
        self.assertEqual(first[1]["proxies"]["https"], "http://proxyuser:proxypass@proxy.invalid:8080")
        self.assertFalse(second[1]["proxies"])

    def test_malformed_or_unavailable_results_fail(self):
        invalid_json = response({})
        invalid_json._content = b"not JSON private-token"
        fixtures = [("get_post", {"id": "123"}, response(["private-token"])),
                    ("get_post", {"id": "123"}, invalid_json),
                    ("get_post", {"id": "124"}, response(operation_body("TweetResultByRestId", [tweet()]))),
                    ("get_post", {"id": "123"}, response({"data": {"tweetResult": {"result": {"__typename": "TweetUnavailable"}}}})),
                    ("get_user", {"username": "reader"}, response({"data": {"user": {"result": {"__typename": "UserUnavailable"}}}})),
                    ("post", {"text": "hello"}, response({"data": {"create_tweet": {}}}))]
        for operation, inputs, reply in fixtures:
            with self.subTest(operation=operation):
                self.replies = [reply]
                with self.assertRaises(main.XRequestError):
                    self.call(operation, **inputs)

    def test_cancelled_call_or_deadline_stops_before_network(self):
        cancelled = threading.Event()
        cancelled.set()
        with self.assertRaisesRegex(main.XRequestError, "timed out"):
            main.execute("post", {"text": "Hello"}, credential(), cancelled)
        self.assertFalse(self.calls)
        with main.client_for(credential(), "", threading.Event()) as client:
            client._session.deadline = 0
            with self.assertRaises(requests.Timeout):
                client._session.get("https://x.com/i/jf/")

    def test_cookie_headers_cannot_be_redirected_or_sent_to_other_hosts(self):
        self.replies = [response({}, 302)]
        with self.assertRaises(main.XRequestError):
            self.call("get_post", id="123")
        with main.client_for(credential(), "", threading.Event()) as client:
            with self.assertRaises(main.XRequestError):
                client._session.get("https://evil.invalid/i/api/graphql/query/GetPost")
        self.assertEqual(len(self.calls), 1)


class PluginTest(unittest.TestCase):
    def test_post_metadata_mapping(self):
        value = main.normalize_post({
            "id": "123", "text": "Hello", "author_id": "9", "author": "reader",
            "created_at": "Fri Oct 23 08:00:00 +0000 2026", "likes": 3,
            "retweets": 2, "replies": 1,
        })
        self.assertEqual(value.get("id"), "123")
        self.assertEqual(value.get("text"), "Hello")
        self.assertEqual(value.get("author_username"), "reader")
        self.assertEqual(value.get("url"), "https://x.com/reader/status/123")
        self.assertEqual(value.get("like_count"), 3)

    def test_missing_cookie_refuses_before_request(self):
        with self.assertRaisesRegex(ValueError, "auth_token.*ct0"):
            main.execute("get_post", {"id": "123"}, SimpleNamespace(auth_token="", ct0="", proxy=""))

    def test_invalid_limit_refuses_before_request(self):
        with self.assertRaisesRegex(ValueError, "limit"):
            main.execute("search", {"query": "hello", "limit": 101},
                         SimpleNamespace(auth_token="token", ct0="csrf", proxy=""))

    def test_all_inputs_are_checked_before_client_creation(self):
        for operation, values in [("search", {"query": ""}), ("search", {"query": "x" * 501}),
                                  ("search", {"query": "x", "limit": 0}), ("search", {"query": "x", "limit": True}),
                                  ("get_user", {"username": "https://x.com/reader"}),
                                  ("get_post", {"id": "abc"}), ("delete_post", {"id": "1;2"}),
                                  ("post", {"text": "hello", "reply_to": "https://x.com"}),
                                  ("post", {"text": " \n "})]:
            with self.subTest(operation=operation, values=values), patch.object(main, "client_for") as factory:
                with self.assertRaises(ValueError):
                    main.execute(operation, values, credential())
                factory.assert_not_called()
        for auth, csrf, proxy in [("token; ct0=other", "csrf", ""), ("token", "csrf\r\n", ""),
                                  ("token", "csrf", "socks5://host:80"), ("token", "csrf", "http://host:bad"),
                                  ("token", "csrf", "http://host:0"), ("token", "csrf", "http://host/path")]:
            with self.subTest(auth=auth, csrf=csrf, proxy=proxy), self.assertRaises(ValueError):
                main.execute("get_post", {"id": "123"}, credential(auth_token=auth, ct0=csrf, proxy=proxy))

    def test_normalization_handles_optional_fields(self):
        result = main.normalize_post({"id": "123", "author": "https://evil.invalid", "likes": "not-a-number", "retweets": -3})
        self.assertEqual(result["url"], "https://x.com/i/status/123")
        self.assertEqual(result["like_count"], 0)
        self.assertEqual(result["repost_count"], 0)
        self.assertEqual(result["created_at"], "")
        self.assertEqual(result["reply_to_id"], "")
        with self.assertRaises(main.XRequestError):
            main.normalize_post({"id": ""})

    def test_bootstrap_uses_guarded_proxy_session_and_keeps_credentials_off_assets(self):
        calls = []
        transaction = SimpleNamespace(generate_transaction_id=lambda **kwargs: "transaction")
        def send(request, **kwargs):
            calls.append((request, kwargs))
            value = response({})
            value._content = b"<html></html>"
            return value
        for migration in (False, True):
            main._TRANSACTION_CACHE.clear()
            with self.subTest(migration=migration), patch.object(requests.Session, "send", side_effect=send), \
                    patch.object(main, "ClientTransaction", return_value=transaction), \
                    patch.object(main, "get_ondemand_file_url", side_effect=[AttributeError(), "https://abs.twimg.com/script.js"] if migration else ["https://abs.twimg.com/script.js"]), \
                    patch.object(main, "handle_x_migration", return_value=main.BeautifulSoup("<html></html>", "html.parser")) as migrate:
                with main.client_for(credential(), "http://proxy.invalid:8080", threading.Event()) as client:
                    self.assertIs(client._transaction(), transaction)
                    self.assertIs(client._transaction(), transaction)
                self.assertEqual(migrate.call_count, int(migration))
        self.assertEqual(len(calls), 4)
        for request, options in calls:
            self.assertNotIn("cookie", request.headers)
            self.assertNotIn("x-csrf-token", request.headers)
            self.assertEqual(options["proxies"]["https"], "http://proxy.invalid:8080")
            self.assertLessEqual(options["timeout"], 30)
        main._TRANSACTION_CACHE.clear()


class TransactionCacheTest(unittest.TestCase):
    def setUp(self):
        main._TRANSACTION_CACHE.clear()
        self.calls = []
        self.transaction = SimpleNamespace(generate_transaction_id=lambda **kwargs: "transaction")
        self.send = patch.object(requests.Session, "send", autospec=True, side_effect=self.fake_send).start()
        self.build = patch.object(main, "ClientTransaction", return_value=self.transaction).start()
        patch.object(main, "get_ondemand_file_url", return_value="https://abs.twimg.com/script.js").start()
        self.addCleanup(patch.stopall)
        self.addCleanup(main._TRANSACTION_CACHE.clear)

    def fake_send(self, session, request, **kwargs):
        self.calls.append((session, request, kwargs))
        if "/i/api/graphql/" in request.url:
            return response(operation_body(request.url.rsplit("/", 1)[-1].split("?", 1)[0]))
        value = response({})
        value._content = b"<html></html>"
        return value

    def test_operations_share_public_bootstrap_but_keep_private_sessions(self):
        proxy = "http://proxy-user:proxy-password@proxy.invalid:8080"
        for token in ("first-account", "second-account"):
            self.assertTrue(main.execute("health_check", {}, credential(auth_token=token, ct0=token + "-csrf", proxy=proxy))["ok"])
        public = [(session, request, options) for session, request, options in self.calls
                  if "/i/api/graphql/" not in request.url]
        private = [(session, request, options) for session, request, options in self.calls
                   if "/i/api/graphql/" in request.url]
        self.assertEqual(len(public), 2)
        self.build.assert_called_once()
        self.assertIsNot(private[0][0], private[1][0])
        for index, token in enumerate(("first-account", "second-account")):
            self.assertEqual(private[index][1].headers["cookie"], f"auth_token={token}; ct0={token}-csrf")
            self.assertEqual(private[index][1].headers["x-csrf-token"], token + "-csrf")
        for _, request, options in public:
            self.assertNotIn("cookie", request.headers)
            self.assertNotIn("x-csrf-token", request.headers)
            self.assertEqual(options["proxies"]["https"], proxy)

    def test_proxy_identity_expiry_and_capacity_limit(self):
        with patch.object(main.time, "monotonic", return_value=100) as clock:
            first = "http://first:secret@proxy.invalid:8080"
            second = "http://second:secret@proxy.invalid:8080"
            for proxy in (first, second, ""):
                main.execute("health_check", {}, credential(proxy=proxy))
            self.assertEqual(self.build.call_count, 3)
            for key in main._TRANSACTION_CACHE:
                self.assertIsInstance(key, bytes)
                self.assertEqual(len(key), 32)
                self.assertNotIn(b"secret", key)
            main.execute("health_check", {}, credential(proxy=first))
            self.assertEqual(self.build.call_count, 3)
            clock.return_value = 100 + main.TRANSACTION_CACHE_TTL
            main.execute("health_check", {}, credential(proxy=first))
            self.assertEqual(self.build.call_count, 4)
            for index in range(main.TRANSACTION_CACHE_SIZE):
                main.execute("health_check", {}, credential(proxy=f"http://proxy{index}.invalid:8080"))
            self.assertEqual(len(main._TRANSACTION_CACHE), main.TRANSACTION_CACHE_SIZE)
            before = self.build.call_count
            main.execute("health_check", {}, credential(proxy=first))
            self.assertEqual(self.build.call_count, before + 1)

    def test_failed_or_cancelled_bootstrap_is_not_cached(self):
        self.build.side_effect = [RuntimeError("bootstrap failed"), self.transaction]
        with self.assertRaises(main.XRequestError):
            main.execute("health_check", {}, credential())
        self.assertFalse(main._TRANSACTION_CACHE)
        main.execute("health_check", {}, credential())
        self.assertEqual(self.build.call_count, 2)
        main._TRANSACTION_CACHE.clear()
        cancelled = threading.Event()
        self.build.side_effect = lambda *args: (cancelled.set(), self.transaction)[1]
        before = len(self.calls)
        with self.assertRaisesRegex(main.XRequestError, "timed out"):
            main.execute("post", {"text": "Hello"}, credential(), cancelled)
        self.assertFalse(main._TRANSACTION_CACHE)
        self.assertEqual(len(self.calls), before + 2)
        self.assertNotIn("/i/api/graphql/", self.calls[-1][1].url)

    def test_cancelled_or_expired_calls_cannot_use_cached_generator(self):
        main.execute("health_check", {}, credential())
        before = len(self.calls)
        for cancelled_call in (False, True):
            with self.subTest(cancelled=cancelled_call):
                cancelled = threading.Event()
                with main.client_for(credential(), "", cancelled) as client:
                    if cancelled_call:
                        cancelled.set()
                    else:
                        client._session.deadline = 0
                    with self.assertRaises(requests.Timeout):
                        client._transaction()
        self.assertEqual(len(self.calls), before)

    def test_lock_wait_honors_cancellation_and_deadline(self):
        for cancellation in (False, True):
            with self.subTest(cancellation=cancellation):
                cancelled = threading.Event()
                with main.client_for(credential(), "", cancelled) as client:
                    client._session.deadline = main.time.monotonic() + 0.08
                    timer = threading.Timer(0.02, cancelled.set) if cancellation else None
                    main._TRANSACTION_CACHE_LOCK.acquire()
                    try:
                        if timer:
                            timer.start()
                        with self.assertRaises(requests.Timeout):
                            client._transaction()
                    finally:
                        main._TRANSACTION_CACHE_LOCK.release()
                        if timer:
                            timer.join()
        self.assertFalse(self.calls)

    def test_bootstrap_does_not_hold_cache_lock_or_cache_network_failure(self):
        original = self.fake_send
        def send(session, request, **kwargs):
            self.assertTrue(main._TRANSACTION_CACHE_LOCK.acquire(blocking=False))
            main._TRANSACTION_CACHE_LOCK.release()
            if request.url == "https://x.com/i/jf/":
                raise requests.ConnectionError("private proxy error")
            return original(session, request, **kwargs)
        self.send.side_effect = send
        with self.assertRaisesRegex(main.XRequestError, "Cannot connect"):
            main.execute("health_check", {}, credential())
        self.assertFalse(main._TRANSACTION_CACHE)
        self.build.assert_not_called()
        self.send.side_effect = original
        main.execute("health_check", {}, credential())
        self.build.assert_called_once()


class DispatchTest(unittest.IsolatedAsyncioTestCase):
    async def test_all_sdk_operations_produce_their_contracts(self):
        values = credential().model_dump()
        def send(request, **kwargs):
            operation = request.url.split("?", 1)[0].rsplit("/", 1)[-1]
            return response(operation_body(operation, [tweet()]))
        with patch.object(main.SessionTweetKit, "_tid", return_value="transaction"), \
                patch.object(requests.Session, "send", side_effect=send):
            for operation, inputs, output in [("search", {"query": "hello"}, "posts"),
                                               ("user_timeline", {"username": "reader"}, "posts"),
                                               ("get_post", {"id": "123"}, "post"),
                                               ("get_user", {"username": "reader"}, "user"),
                                               ("post", {"text": "Hello"}, "post"),
                                               ("delete_post", {"id": "123"}, "deleted"),
                                               ("health_check", {}, "ok")]:
                with self.subTest(operation=operation):
                    result = await main.p.dispatch_buffered({"operation": operation, "input": inputs, "credential": values})
                    self.assertTrue(result[output])

    async def test_health_failure_is_reported_in_output(self):
        result = await main.p.dispatch_buffered({"operation": "health_check", "input": {}})
        self.assertFalse(result["ok"])
        self.assertIn("auth_token", result["message"])
        with patch.object(main.SessionTweetKit, "_tid", return_value="transaction"), \
                patch.object(requests.Session, "send", return_value=response({"errors": []}, 401)):
            result = await main.p.dispatch_buffered({"operation": "health_check", "input": {}, "credential": credential().model_dump()})
        self.assertFalse(result["ok"])
        self.assertIn("login session", result["message"])

    async def test_cancellation_stops_worker_at_next_request_and_cleans_up(self):
        started, release, cancelled_events = threading.Event(), threading.Event(), []
        def execute(operation, inputs, credentials, cancelled):
            cancelled_events.append(cancelled)
            started.set()
            release.wait(2)
            return {"posts": [], "count": 0}
        ctx = SimpleNamespace(credential_as=lambda cls: credential())
        with patch.object(main, "execute", side_effect=execute):
            task = asyncio.create_task(main.invoke("search", ctx, main.SearchIn(query="hello")))
            try:
                await asyncio.to_thread(started.wait, 1)
                task.cancel()
                with self.assertRaises(asyncio.CancelledError):
                    await task
                self.assertTrue(cancelled_events[0].is_set())
            finally:
                release.set()

    async def test_async_timeout_sets_cancellation_flag(self):
        seen = []
        def execute(operation, inputs, credentials, cancelled):
            seen.append(cancelled)
            cancelled.wait(1)
            return {"posts": [], "count": 0}
        ctx = SimpleNamespace(credential_as=lambda cls: credential())
        with patch.object(main, "execute", side_effect=execute), patch.object(main, "REQUEST_TIMEOUT", 0.03):
            with self.assertRaisesRegex(main.XRequestError, "timed out"):
                await main.invoke("search", ctx, main.SearchIn(query="hello"))
        self.assertTrue(seen[0].is_set())


if __name__ == "__main__":
    unittest.main()
