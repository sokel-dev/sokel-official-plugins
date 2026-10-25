"""Bounded X web-session operations backed by a pinned TweetKit release.

The compatibility layer keeps all upstream requests in one private session,
checks HTTP/GraphQL failures, and applies X's POST-only search transport.
"""

from __future__ import annotations

import asyncio
from collections import OrderedDict
from contextlib import contextmanager
import hashlib
import json
import re
import threading
import time
from urllib.parse import urlsplit

import requests
from bs4 import BeautifulSoup
from tweetkit_x import TweetKit
from tweetkit_x.constants import USER_TWEETS_FEATURES
from tweetkit_x.client import ClientTransaction, get_ondemand_file_url, handle_x_migration

from sokel import Ctx
from list_members import change_member
from sokel_gen import (
    AddListMemberIn, AddListMemberOut, RemoveListMemberIn, RemoveListMemberOut,
    ListTimelineIn, ListTimelineOut, MyListsIn, MyListsOut,
    Credential, DeletePostIn, DeletePostOut, GetPostIn, GetPostOut,
    GetUserIn, GetUserOut, HealthCheckIn, HealthCheckOut, PostIn, PostOut,
    SearchIn, SearchOut, UserTimelineIn, UserTimelineOut,
    credential as read_credential, new_plugin, on_delete_post, on_get_post,
    on_get_user, on_health_check, on_post, on_search, on_user_timeline,
    on_list_timeline, on_my_lists, on_add_list_member, on_remove_list_member,
)

REQUEST_TIMEOUT = 30
TRANSACTION_CACHE_TTL = 300
TRANSACTION_CACHE_SIZE = 8
_TRANSACTION_CACHE = OrderedDict()
_TRANSACTION_CACHE_LOCK = threading.Lock()
# X web operation IDs and features from twitter-openapi, commit 252713ac4747.
# Keep these local: changing TweetKit's global configuration leaks across calls.
LIST_QUERIES = {
    "list_timeline": ("RgfF9JYDXXI1F-ZrDqjUfQ", "ListLatestTweetsTimeline",
                      ("list", "tweets_timeline", "timeline", "instructions")),
    "my_lists": ("FMDRDwDKo_nl4kT_AKWOCg", "ListsManagementPageTimeline",
                 ("viewer", "list_management_timeline", "timeline", "instructions")),
}
LIST_FEATURES = {
    **USER_TWEETS_FEATURES,
    "responsive_web_profile_redirect_enabled": True,
    "rweb_sports_post_context_enabled": True,
    "responsive_web_nested_quote_preview_enabled": True,
    "responsive_web_enhance_cards_enabled": False,
}
p = new_plugin()


class XRequestError(RuntimeError):
    """Only fixed messages and numeric status codes may cross the SDK boundary."""


def request_error(status=0, codes=()):
    codes = {code for code in codes if type(code) is int and 0 <= code <= 9999}
    if status == 429 or 88 in codes:
        return XRequestError("X rate limit reached; wait before trying again. No request was retried.")
    if status in (401, 403) or codes.intersection({32, 64, 89, 215, 239, 326}):
        return XRequestError("X rejected the login session or access; refresh auth_token and ct0 from the same logged-in account and check account restrictions.")
    if status == 404 or codes.intersection({34, 50, 63, 144}):
        return XRequestError("The X post, user, List or endpoint is unavailable; check the ID, account access and plugin version.")
    if 187 in codes:
        return XRequestError("X rejected a duplicate post; check your profile before submitting again.")
    if 186 in codes:
        return XRequestError("X rejected the post length; shorten the text for this account.")
    if 226 in codes:
        return XRequestError("X blocked this action; check account restrictions in the browser before trying again.")
    details = []
    if type(status) is int and 100 <= status <= 599:
        details.append(f"HTTP {status}")
    if codes:
        details.append("X error codes: " + ", ".join(str(code) for code in sorted(codes)[:5]))
    suffix = " (" + "; ".join(details) + ")" if details else ""
    return XRequestError("X returned an unexpected response; check account access and update the plugin if the web API changed." + suffix)


def _number(value):
    try:
        return max(0, int(value or 0))
    except (TypeError, ValueError):
        return 0


def normalize_post(value):
    post_id = str(value.get("id") or "")
    if not re.fullmatch(r"[0-9]{1,20}", post_id):
        raise request_error()
    author = str(value.get("author") or "")
    if not re.fullmatch(r"[A-Za-z0-9_]{1,15}", author) or author == "i":
        author = ""
    return {
        "id": post_id, "text": str(value.get("text") or ""),
        "url": f"https://x.com/{author or 'i'}/status/{post_id}",
        "created_at": str(value.get("created_at") or ""),
        "author_id": str(value.get("author_id") or ""), "author_username": author,
        "reply_to_id": str(value.get("reply_to_id") or ""),
        "like_count": _number(value.get("likes")),
        "repost_count": _number(value.get("retweets")), "reply_count": _number(value.get("replies")),
    }


def _at(node, *path):
    for key in path:
        node = node.get(key) if isinstance(node, dict) else None
    return node


def _cursor(value):
    return isinstance(value, str) and len(value) <= 4096 and not re.search(r"[\x00-\x1f\x7f-\x9f]", value)


def normalize_list_id(value):
    if not isinstance(value, str) or re.search(r"[\x00-\x1f\x7f-\x9f]", value):
        raise ValueError("list_id must be a numeric List ID or an HTTPS X List link")
    value = value.strip()
    if re.fullmatch(r"[1-9][0-9]{0,19}", value):
        return value
    try:
        url = urlsplit(value)
        match = re.fullmatch(r"/i/lists/([1-9][0-9]{0,19})/?", url.path)
        if (url.scheme == "https" and url.netloc in ("x.com", "twitter.com") and match):
            return match[1]
    except ValueError:
        pass
    raise ValueError("list_id must be a numeric List ID or an HTTPS X List link")


def timeline_page(instructions, previous_cursor, *, skip_list_recommendations=False):
    """Read explicit timeline slots, never recursively collect embedded content."""
    if not isinstance(instructions, list):
        raise request_error()
    items, next_cursor = [], ""
    skipped_modules = set()

    def recommended(module_id, content=None):
        if not skip_list_recommendations:
            return False
        return (isinstance(module_id, str) and
                (module_id.startswith("list-to-follow-module-") or module_id in skipped_modules) or
                _at(content, "clientEventInfo", "component") == "suggest_list_to_follow")

    def consume_entry(entry):
        module_id, content = _at(entry, "entryId"), _at(entry, "content")
        if recommended(module_id, content):
            if isinstance(module_id, str):
                skipped_modules.add(module_id)
            return
        consume(content)

    def consume(content):
        nonlocal next_cursor
        if not isinstance(content, dict):
            raise request_error()
        if "promotedMetadata" in content or recommended(None, content):
            return
        entry_type = content.get("entryType")
        # Module items are optional; an empty UI module may omit the field.
        # When present, modules() still requires an array and valid item slots.
        if entry_type == "TimelineTimelineItem" and "itemContent" not in content:
            raise request_error()
        if content.get("cursorType") == "Bottom":
            value = content.get("value")
            if not _cursor(value):
                raise request_error()
            next_cursor = value
        elif "itemContent" in content:
            item = content["itemContent"]
            if not isinstance(item, dict):
                raise request_error()
            if "promotedMetadata" not in item and not recommended(None, item):
                if item.get("cursorType"):
                    consume(item)
                else:
                    items.append(item)
        elif "items" in content:
            modules(content["items"])

    def modules(rows):
        if not isinstance(rows, list):
            raise request_error()
        for row in rows:
            if not isinstance(row, dict):
                raise request_error()
            if "promotedMetadata" not in row:
                consume(row.get("item"))

    for instruction in instructions:
        if not isinstance(instruction, dict):
            raise request_error()
        kind = instruction.get("type")
        if kind == "TimelineAddEntries":
            entries = instruction.get("entries")
            if not isinstance(entries, list):
                raise request_error()
            for entry in entries:
                consume_entry(entry)
        elif kind in ("TimelineReplaceEntry", "TimelinePinEntry"):
            consume_entry(instruction.get("entry"))
        elif kind == "TimelineAddToModule":
            if not recommended(instruction.get("moduleEntryId")):
                modules(instruction.get("moduleItems"))
        elif kind == "TimelineTerminateTimeline" and instruction.get("direction") == "Bottom":
            next_cursor = ""
    if next_cursor and next_cursor == previous_cursor:
        raise XRequestError("X returned a repeated pagination cursor; stop this run and start a new run from the first page.")
    return items, next_cursor


def list_posts(items, session):
    posts, seen = [], set()
    for item in items:
        if item.get("itemType") != "TimelineTweet" and "tweet_results" not in item:
            continue
        # X retains deleted tweet slots with an explicitly empty result object.
        # Missing fields or malformed nonempty results still signal API drift.
        if item.get("tweet_results") == {}:
            continue
        tweet = _at(item, "tweet_results", "result")
        if not isinstance(tweet, dict):
            raise request_error()
        if tweet.get("__typename") == "TweetWithVisibilityResults":
            tweet = tweet.get("tweet")
        if not isinstance(tweet, dict):
            raise request_error()
        if tweet.get("__typename") in ("TweetUnavailable", "TweetTombstone"):
            continue
        post_id = str(tweet.get("rest_id") or _at(tweet, "legacy", "id_str") or "")
        value = session.posts.get(post_id)
        if not value:
            raise request_error()
        note = _at(tweet, "note_tweet", "note_tweet_results", "result", "text")
        if isinstance(note, str):
            value = {**value, "text": note}
        if post_id not in seen:
            posts.append(normalize_post(value))
            seen.add(post_id)
    return posts


def list_summaries(items):
    lists, seen = [], set()
    for item in items:
        if item.get("itemType") != "TimelineTwitterList" and "list" not in item:
            continue
        value = item.get("list")
        if (not isinstance(value, dict) or
                not isinstance(value.get("id_str"), str) or
                not re.fullmatch(r"[0-9]{1,20}", value["id_str"]) or
                not isinstance(value.get("name"), str)):
            raise request_error()
        list_id = value["id_str"]
        if list_id in seen:
            continue
        owner = _at(value, "user_results", "result") or {}
        owner_id = str(owner.get("rest_id") or "")
        owner_username = _at(owner, "core", "screen_name") or _at(owner, "legacy", "screen_name") or ""
        if owner.get("__typename") == "UserUnavailable" or not re.fullmatch(r"[0-9]{1,20}", owner_id):
            owner_id = ""
        if not isinstance(owner_username, str) or not re.fullmatch(r"[A-Za-z0-9_]{1,15}", owner_username):
            owner_username = ""
        mode = str(value.get("mode") or "").lower()
        lists.append({
            "id": list_id, "name": value["name"], "description": str(value.get("description") or ""),
            "mode": mode if mode in ("public", "private") else "",
            "member_count": _number(value.get("member_count")),
            "subscriber_count": _number(value.get("subscriber_count")),
            "owner_id": owner_id, "owner_username": owner_username,
            "url": f"https://x.com/i/lists/{list_id}",
        })
        seen.add(list_id)
    return lists


def _list_banner_error_has_valid_target(data, path):
    if (not isinstance(path, list)
            or path[:4] != ["viewer", "list_management_timeline", "timeline", "instructions"]
            or path[-2:] != ["default_banner_media_results", "result"]):
        return False

    def resolve(parts):
        node = data
        for part in parts:
            if isinstance(part, str) and isinstance(node, dict):
                node = node.get(part)
            elif type(part) is int and isinstance(node, list) and 0 <= part < len(node):
                node = node[part]
            else:
                return None
        return node

    instruction = resolve(path[:5])
    if not isinstance(instruction, dict):
        return False
    kind = instruction.get("type")
    item = ("itemContent", "list")
    module = ("items", None, "item", *item)
    if kind == "TimelineAddEntries":
        patterns = [("entries", None, "content", *tail) for tail in (item, module)]
    elif kind in ("TimelinePinEntry", "TimelineReplaceEntry"):
        patterns = [("entry", "content", *tail) for tail in (item, module)]
    elif kind == "TimelineAddToModule":
        patterns = [("moduleItems", None, "item", *item)]
    else:
        return False
    relative = path[5:-2]
    if not any(len(pattern) == len(relative) and all(
            type(actual) is int and actual >= 0 if expected is None else actual == expected
            for actual, expected in zip(relative, pattern)) for pattern in patterns):
        return False
    if kind != "TimelineAddToModule":
        content = resolve(path[:6 + relative.index("content")])
        expected_type = "TimelineTimelineModule" if "items" in relative else "TimelineTimelineItem"
        if not isinstance(content, dict) or content.get("entryType") != expected_type:
            return False
    item_content = resolve(path[:-3])
    value = resolve(path[:-2])
    return (isinstance(item_content, dict) and item_content.get("itemType") == "TimelineTwitterList"
            and isinstance(value, dict) and isinstance(value.get("id_str"), str)
            and re.fullmatch(r"[0-9]{1,20}", value["id_str"]) is not None
            and isinstance(value.get("name"), str))


def _only_optional_list_banner_errors(method, operation, errors, data):
    """Allow only the observed partial failure of an unused List cover field."""
    if (method.upper() != "GET" or operation != "ListsManagementPageTimeline"
            or not isinstance(errors, list) or not errors):
        return False
    for error in errors:
        if not isinstance(error, dict):
            return False
        for details in (error, error.get("extensions")):
            if (not isinstance(details, dict) or type(details.get("code")) is not int
                    or details["code"] != 214 or details.get("kind") != "Validation"
                    or details.get("name") != "BadRequestError"):
                return False
        if error.get("message") != "BadRequest: com.twitter.strato.serialization.DecodeException":
            return False
        if not _list_banner_error_has_valid_target(data, error.get("path")):
            return False
    return True


class GuardedSession(requests.Session):
    def __init__(self, client, proxy, cancelled):
        super().__init__()
        self.client = client
        self.cancelled = cancelled
        self.deadline = time.monotonic() + REQUEST_TIMEOUT
        self.trust_env = False
        self.proxies = {"http": proxy, "https": proxy} if proxy else {}
        self.transaction_cache_key = hashlib.sha256(proxy.encode("utf-8")).digest()
        self.posts = {}
        self.profile_bios = {}

    def remaining(self):
        remaining = self.deadline - time.monotonic()
        if self.cancelled.is_set() or remaining <= 0:
            raise requests.Timeout()
        return remaining

    def request(self, method, url, **kwargs):
        self.remaining()
        parsed = urlsplit(url)
        graphql = parsed.path.startswith("/i/api/graphql/")
        if graphql:
            # Raw cookie/CSRF headers must never follow a redirect to another host.
            if parsed.scheme != "https" or parsed.hostname != "x.com":
                raise request_error()
            kwargs["allow_redirects"] = False
            if method.upper() == "GET" and parsed.path.endswith("/SearchTimeline"):
                params = kwargs.pop("params")
                kwargs["json"] = {key: json.loads(value) for key, value in params.items()}
                kwargs["json"]["queryId"] = parsed.path.split("/")[-2]
                method = "POST"
                kwargs["headers"] = dict(kwargs.get("headers", {}))
                kwargs["headers"]["content-type"] = "application/json"
                kwargs["headers"]["x-client-transaction-id"] = self.client._tid(method, parsed.path)
        kwargs["timeout"] = min(REQUEST_TIMEOUT, self.remaining())
        response = super().request(method, url, **kwargs)
        if not 200 <= response.status_code < 300:
            response.close()
            raise request_error(response.status_code)
        if graphql:
            try:
                body = response.json()
            except ValueError:
                raise request_error() from None
            if not isinstance(body, dict):
                raise request_error()
            operation = parsed.path.rsplit("/", 1)[-1]
            errors = body.get("errors")
            data = body.get("data")
            if errors and not _only_optional_list_banner_errors(method, operation, errors, data):
                codes = ([item.get("code") for item in errors if isinstance(item, dict)]
                         if isinstance(errors, list) else [])
                raise request_error(response.status_code, codes=[code for code in codes if isinstance(code, int)])
            if not isinstance(data, dict) or not data:
                raise request_error()
            if parsed.path.endswith("/DeleteTweet") and not isinstance(data.get("delete_tweet"), dict):
                raise request_error()
            timeline_paths = {
                "SearchTimeline": [("search_by_raw_query", "search_timeline", "timeline")],
                "UserTweets": [("user", "result", "timeline", "timeline"),
                               ("user", "result", "timeline_v2", "timeline")],
                "HomeLatestTimeline": [("home", "home_timeline_urt")],
            }
            if operation in timeline_paths:
                timelines = []
                for path in timeline_paths[operation]:
                    node = data
                    for key in path:
                        node = node.get(key) if isinstance(node, dict) else None
                    if isinstance(node, dict) and isinstance(node.get("instructions"), list):
                        timelines.append(node)
                if not timelines:
                    raise request_error()
            self._remember_posts(data)
            # TweetKit recursively treats quoted/reposted objects as independent
            # matches. Remove those copies before its counting and pagination.
            self._prune_related(data)
            response._content = json.dumps(body).encode("utf-8")
        return response

    @staticmethod
    def _prune_related(node):
        if isinstance(node, dict):
            node.pop("quoted_status_result", None)
            node.pop("retweeted_status_result", None)
            for value in node.values():
                GuardedSession._prune_related(value)
        elif isinstance(node, list):
            for value in node:
                GuardedSession._prune_related(value)

    def _remember_posts(self, node):
        """Preserve reply IDs and complete write responses omitted by TweetKit."""
        if isinstance(node, dict):
            if node.get("__typename") == "User" and node.get("rest_id"):
                bio = node.get("profile_bio")
                if isinstance(bio, dict) and isinstance(bio.get("description"), str):
                    self.profile_bios[str(node["rest_id"])] = bio["description"]
            legacy = node.get("legacy")
            if isinstance(legacy, dict) and "full_text" in legacy:
                post_id = legacy.get("id_str") or node.get("rest_id")
                if post_id:
                    user = (((node.get("core") or {}).get("user_results") or {}).get("result") or {})
                    author = (user.get("core") or {}).get("screen_name") or (user.get("legacy") or {}).get("screen_name")
                    self.posts[str(post_id)] = {
                        "id": str(post_id), "text": legacy.get("full_text"),
                        "created_at": legacy.get("created_at"), "author_id": legacy.get("user_id_str"),
                        "reply_to_id": legacy.get("in_reply_to_status_id_str"),
                        "likes": legacy.get("favorite_count"), "retweets": legacy.get("retweet_count"),
                        "replies": legacy.get("reply_count"), **({"author": author} if author else {}),
                    }
            for value in node.values():
                self._remember_posts(value)
        elif isinstance(node, list):
            for value in node:
                self._remember_posts(value)


@contextmanager
def _transaction_cache_lock(session):
    # Poll cancellation while waiting; every operation keeps its own deadline.
    while not _TRANSACTION_CACHE_LOCK.acquire(timeout=min(0.05, session.remaining())):
        pass
    try:
        session.remaining()
        yield
    finally:
        _TRANSACTION_CACHE_LOCK.release()


class SessionTweetKit(TweetKit):
    def list_page(self, operation, values):
        query_id, name, path = LIST_QUERIES[operation]
        endpoint = f"/i/api/graphql/{query_id}/{name}"
        variables = {"count": values["limit"]}
        if operation == "list_timeline":
            variables["listId"] = values["list_id"]
        if values["cursor"]:
            variables["cursor"] = values["cursor"]
        response = self._session.get(
            "https://x.com" + endpoint,
            params={"variables": json.dumps(variables), "features": json.dumps(LIST_FEATURES)},
            headers=self._headers("GET", endpoint), timeout=self.timeout,
        )
        return timeline_page(_at(response.json(), "data", *path), values["cursor"],
                             skip_list_recommendations=operation == "my_lists")

    def _transaction(self):
        session = self._session
        session.remaining()
        if self._ct is not None:
            return self._ct
        key = session.transaction_cache_key
        with _transaction_cache_lock(session):
            cached = _TRANSACTION_CACHE.pop(key, None)
            if cached is not None and cached[0] > time.monotonic():
                _TRANSACTION_CACHE[key] = cached
                self._ct = cached[1]
                return self._ct

        # Only anonymous public assets enter the cache. The pinned generator
        # reads its fields without mutation; sessions and cookies stay private.
        # Bootstrap outside the lock so another proxy never waits for this I/O.
        home = BeautifulSoup(session.get("https://x.com/i/jf/").text, "html.parser")
        try:
            url = get_ondemand_file_url(home)
        except AttributeError:
            home = handle_x_migration(session)
            url = get_ondemand_file_url(home)
        script = session.get(url)
        transaction = ClientTransaction(home, BeautifulSoup(script.text, "html.parser"))
        with _transaction_cache_lock(session):
            _TRANSACTION_CACHE[key] = (time.monotonic() + TRANSACTION_CACHE_TTL, transaction)
            _TRANSACTION_CACHE.move_to_end(key)
            while len(_TRANSACTION_CACHE) > TRANSACTION_CACHE_SIZE:
                _TRANSACTION_CACHE.popitem(last=False)
        self._ct = transaction
        return self._ct


def validate(operation, inputs, credential):
    for value in (credential.auth_token, credential.ct0):
        if not value or re.search(r"[\s\x00-\x1f\x7f;,]", value):
            raise ValueError("Provide auth_token and ct0 cookie values from the same logged-in account, without Cookie headers or separators.")
    proxy = credential.proxy.strip()
    if proxy:
        try:
            parsed = urlsplit(proxy)
            if (parsed.scheme not in ("http", "https") or not parsed.hostname or
                    parsed.path not in ("", "/") or parsed.query or parsed.fragment or
                    re.search(r"\s", proxy) or parsed.port == 0):
                raise ValueError()
        except ValueError:
            raise ValueError("proxy must be a valid HTTP or HTTPS proxy URL") from None
    values = dict(inputs)
    if operation in ("search", "user_timeline", "list_timeline", "my_lists"):
        limit = values.setdefault("limit", 20)
        if type(limit) is not int or not 1 <= limit <= 100:
            raise ValueError("limit must be between 1 and 100")
    if operation in ("list_timeline", "my_lists"):
        if not _cursor(values.setdefault("cursor", "")):
            raise ValueError("cursor must be a string of at most 4096 characters without control characters")
    if operation in ("list_timeline", "add_list_member", "remove_list_member"):
        values["list_id"] = normalize_list_id(values.get("list_id"))
    if operation == "search":
        values["query"] = values.get("query", "").strip()
        if not 1 <= len(values["query"]) <= 500:
            raise ValueError("query must contain 1 to 500 characters")
    if operation in ("user_timeline", "get_user", "add_list_member", "remove_list_member"):
        username = values.get("username", "")
        if not isinstance(username, str):
            raise ValueError("username must be an X account name, optionally prefixed with @")
        values["username"] = username.strip().removeprefix("@")
        if not re.fullmatch(r"[A-Za-z0-9_]{1,15}", values["username"]):
            raise ValueError("username must be an X account name, optionally prefixed with @")
    if operation in ("get_post", "delete_post") or values.get("reply_to"):
        field = "reply_to" if operation == "post" else "id"
        if not re.fullmatch(r"[0-9]{1,20}", values.get(field, "")):
            raise ValueError(f"{field} must be a numeric X post ID")
    if operation == "post" and not values.get("text", "").strip():
        raise ValueError("text must not be empty")
    return values, proxy


@contextmanager
def client_for(credential, proxy, cancelled):
    client = SessionTweetKit(cookie=f"auth_token={credential.auth_token}; ct0={credential.ct0}", timeout=REQUEST_TIMEOUT)
    old_session = client._session
    session = GuardedSession(client, proxy, cancelled)
    session.headers.update(old_session.headers)
    old_session.close()
    client._session = session
    try:
        yield client
    finally:
        session.close()


def execute(operation, inputs, credential, cancelled=None):
    values, proxy = validate(operation, inputs, credential)
    try:
        with client_for(credential, proxy, cancelled or threading.Event()) as client:
            def post_value(value):
                details = client._session.posts.get(str(value.get("id")), {})
                return normalize_post({**value, **details})

            if operation == "list_timeline":
                items, next_cursor = client.list_page(operation, values)
                posts = list_posts(items, client._session)
                return {"list_id": values["list_id"], "posts": posts, "count": len(posts),
                        "next_cursor": next_cursor, "has_more": bool(next_cursor)}
            if operation == "my_lists":
                items, next_cursor = client.list_page(operation, values)
                lists = list_summaries(items)
                return {"lists": lists, "count": len(lists),
                        "next_cursor": next_cursor, "has_more": bool(next_cursor)}
            if operation in ("add_list_member", "remove_list_member"):
                return change_member(client, operation, values["list_id"], values["username"], request_error)
            if operation in ("search", "user_timeline"):
                rows = (client.search_x(values["query"], product="Latest", limit=values["limit"])
                        if operation == "search" else
                        client.get_tweets(username=values["username"], limit=values["limit"], include_replies=True))
                posts = [post_value(row) for row in rows]
                return {"posts": posts, "count": len(posts)}
            if operation in ("get_post", "post"):
                result = (client.get_tweet(values["id"]) if operation == "get_post" else
                          client.post(values["text"], reply_to=values.get("reply_to") or None))
                if not result.get("ok") or not result.get("id"):
                    raise request_error()
                if operation == "get_post" and str(result["id"]) != values["id"]:
                    raise request_error()
                return {"post": post_value({"text": values.get("text", ""), "reply_to_id": values.get("reply_to", ""), **result})}
            if operation == "get_user":
                result = client.user_profile(values["username"])
                if (not result.get("ok") or not re.fullmatch(r"[0-9]{1,20}", str(result.get("id") or ""))
                        or not re.fullmatch(r"[A-Za-z0-9_]{1,15}", result.get("username") or "")):
                    raise request_error(404)
                return {"user": {
                    "id": str(result["id"]), "username": result["username"],
                    "name": result.get("name") or "",
                    "description": client._session.profile_bios.get(str(result["id"]), result.get("bio") or ""),
                    "followers_count": _number(result.get("followers")), "following_count": _number(result.get("following")),
                    "url": f"https://x.com/{result['username']}",
                }}
            if operation == "delete_post":
                result = client.delete(values["id"])
                if not result.get("ok"):
                    raise request_error()
                return {"deleted": True, "id": values["id"]}
            if operation == "health_check":
                client.home_timeline(following=True, limit=1)
                return {"ok": True, "message": "X accepted an authenticated read request; write permissions were not tested."}
            raise ValueError("Unknown operation")
    except XRequestError:
        raise
    except requests.Timeout:
        raise XRequestError("X request timed out; check proxy connectivity. For writes, check the result on X before submitting again.") from None
    except requests.RequestException:
        raise XRequestError("Cannot connect to X; check the proxy, TLS certificates and outbound network. For writes, check the result on X before submitting again.") from None
    except Exception:
        # Upstream errors can contain cookies, response text and proxy passwords.
        raise request_error() from None


async def invoke(operation, ctx, in_):
    cancelled = threading.Event()
    try:
        async with asyncio.timeout(REQUEST_TIMEOUT):
            return await asyncio.to_thread(execute, operation, in_.model_dump(), read_credential(ctx), cancelled)
    except TimeoutError:
        raise XRequestError("X request timed out; for writes, check the result on X before submitting again.") from None
    finally:
        # A blocking request may finish later; prevent any subsequent page/write.
        cancelled.set()


async def search(ctx: Ctx, in_: SearchIn) -> SearchOut:
    return SearchOut(**await invoke("search", ctx, in_))


async def user_timeline(ctx: Ctx, in_: UserTimelineIn) -> UserTimelineOut:
    return UserTimelineOut(**await invoke("user_timeline", ctx, in_))


async def list_timeline(ctx: Ctx, in_: ListTimelineIn) -> ListTimelineOut:
    return ListTimelineOut(**await invoke("list_timeline", ctx, in_))


async def my_lists(ctx: Ctx, in_: MyListsIn) -> MyListsOut:
    return MyListsOut(**await invoke("my_lists", ctx, in_))


async def add_list_member(ctx: Ctx, in_: AddListMemberIn) -> AddListMemberOut:
    return AddListMemberOut(**await invoke("add_list_member", ctx, in_))


async def remove_list_member(ctx: Ctx, in_: RemoveListMemberIn) -> RemoveListMemberOut:
    return RemoveListMemberOut(**await invoke("remove_list_member", ctx, in_))


async def get_post(ctx: Ctx, in_: GetPostIn) -> GetPostOut:
    return GetPostOut(**await invoke("get_post", ctx, in_))


async def get_user(ctx: Ctx, in_: GetUserIn) -> GetUserOut:
    return GetUserOut(**await invoke("get_user", ctx, in_))


async def post(ctx: Ctx, in_: PostIn) -> PostOut:
    return PostOut(**await invoke("post", ctx, in_))


async def delete_post(ctx: Ctx, in_: DeletePostIn) -> DeletePostOut:
    return DeletePostOut(**await invoke("delete_post", ctx, in_))


async def health_check(ctx: Ctx, in_: HealthCheckIn) -> HealthCheckOut:
    try:
        return HealthCheckOut(**await invoke("health_check", ctx, in_))
    except (ValueError, XRequestError) as error:
        return HealthCheckOut(ok=False, message=str(error))


on_search(p, search)
on_user_timeline(p, user_timeline)
on_list_timeline(p, list_timeline)
on_my_lists(p, my_lists)
on_add_list_member(p, add_list_member)
on_remove_list_member(p, remove_list_member)
on_get_post(p, get_post)
on_get_user(p, get_user)
on_post(p, post)
on_delete_post(p, delete_post)
on_health_check(p, health_check)


if __name__ == "__main__":
    asyncio.run(p.run())
