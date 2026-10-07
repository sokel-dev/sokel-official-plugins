# reddit

First-party Reddit plugin on Reddit's public Atom feeds: search posts, list a subreddit's or a user's posts, read a
post's comments; watch the user's posts for new comments and a keyword for new posts, as events. User-facing
documentation: [`docs/reddit.md`](docs/reddit.md).

## Why feeds and not the API

Reddit closed self-service API apps on 2025-11-11 (Responsible Builder Policy): `/prefs/apps` no longer creates
anything, approval takes weeks and is rarely granted. The earlier version of this plugin (OAuth password grant,
submit / reply / inbox) could not be verified and was replaced. Feeds need no key, so the plugin is read-only and
cannot see the inbox; "replies to my comments on other people's posts" are out of reach by design.

## What the real site taught (2026-10-07), each guarded by a test

- Unauthenticated feeds allow **one request per minute per IP**: `x-ratelimit-remaining: 0` after a single request,
  `x-ratelimit-reset` counting down from ~60 s, 429 for anything sooner. `client.go` has a process-wide pacer that
  claims the slot before a request goes out and waits out one 429 (`TestPacer`).
- After ~30 requests in 15 minutes with many 429s, **www.reddit.com dropped the exit IP at the TLS layer** (curl too);
  `old.reddit.com` redirects feeds to login. Pacing is therefore not optional, and a datacenter IP may still be cut
  off; the user doc says what that looks like.
- `.json` endpoints answer 403 to non-browser clients; `.rss` is what is open.
- A post's feed (`/comments/<id>/.rss`) leads with the post (`t3_`) and then up to 25 comments (`t1_`) in Reddit's
  default order; comments carry only `<updated>`, posts `<published>` (`TestParseFeed`).
- `search.rss?type=comment` does not return comments (subreddit `t5_` entries and posts instead), so keyword
  watching is posts only.
- Entries name the author as `/u/<name>`; the prefix is stripped.

## Fixtures

Every file under `testdata/` is a captured feed (see the header of `reddit_test.go`). `live_test.go` runs two
real fetches when `REDDIT_LIVE=1` (`REDDIT_PROXY` for an outbound proxy).

## Regenerating

```bash
go generate ./...
```
