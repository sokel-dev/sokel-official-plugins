# hackernews

First-party Hacker News plugin: read operations (item, front-page lists, search, a thread's comments) and an event
source that watches for new comments on the user's stories, replies to the user's comments, and keyword matches.
User-facing documentation: [`docs/hackernews.md`](docs/hackernews.md) (embedded into the binary and shown on the
platform).

## Why read-only

HN has no write API. Both public APIs are read-only, and automating submissions or comments through the website
breaks the site guidelines and gets accounts banned. The plugin's job is to make sure no conversation is missed;
the reply itself happens on the site through the comment's `url`.

## Two upstreams

| Upstream | Used for | Why |
|---|---|---|
| Algolia, `hn.algolia.com/api/v1` | search, thread comments, everything the event source polls | `search_by_date` filters by author tag, story tag, `parent_id` and `created_at_i` in one request; the official API only has per-item lookups |
| Firebase, `hacker-news.firebaseio.com/v0` | single items, front-page lists, user lookup | the source of truth; Algolia lags it by up to about a minute |

## Event source design (`watch.go`)

Each round runs at most four Algolia queries:

1. the user's stories in the last `watch_days` (`tags=story,author_<user>`);
2. the user's comments in the same window (`tags=comment,author_<user>`);
3. new comments on those stories plus `watch_items` (`tags=comment,(story_a,story_b,…)`);
4. replies to the user's comments on other stories (`numericFilters=…,(parent_id=a,parent_id=b,…)`).

Queries 3 and 4 overlap (a reply to the user's comment on the user's own story is in both), so hits are merged and
deduplicated by id before pushing. `is_reply_to_me` means the parent is something the user wrote (from 1 and 2).
The user's own comments are never pushed.

**Indexing lag.** Algolia indexes a comment up to about a minute after it is posted. Filtering on "created after the
cursor" alone would miss a comment created before the cursor moved but indexed after, so every query looks back
`lookbackSeconds` (300) before the cursor, and the ids pushed in that window are kept in the cursor (`seen`) and
skipped. The cursor is JSON in the credential field `watch_cursor`: one `{t, seen}` per stream, plus the keyword the
keyword stream was started for (a changed keyword restarts from now instead of replaying its history).

The usual polling-source rules apply: no history on the first round, cursor written back to the credential, a failed
push stops the round without advancing the cursor (the platform deduplicates on the event id `hn:comment:<id>` /
`hn:keyword:<id>`), interval floor of 60 s, at most 50 events per round.

## Event fields

Fields follow HN's own terms (story / comment / parent) and keep everything HN returns: `text_html` is passed
through as is, `text` is a plain-text rendering of it (links keep their full `href`, because the visible link text
HN shows is truncated with `...`). Mapping onto a shape shared with other platforms is deliberately left to the
workflow: doing it here would bend one platform's meaning into another's without anyone seeing it.

## Tests

`testdata/` holds responses captured from the real APIs on 2026-10-04 (see the header of `hackernews_test.go` for the
two consistent watch sets). The fake upstream honours the `created_at_i>=` bound like Algolia does; without that the
cross-round dedup would be tested against a server that never returns old hits. The dedup test was checked to fail
(22 pushed instead of 20) with the `seen` check removed.

To run one live round against the real APIs, write a throwaway test calling `pollComments` with a real user; the
plugin was verified that way (20 comment events, 10 of them replies to the user, for one live account).

## Regenerating

```bash
go generate ./...   # or: go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen
```
