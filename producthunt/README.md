# producthunt

First-party Product Hunt plugin: read a product, its comments and the daily leaderboard, and an event source that
watches products for new comments. User-facing documentation: [`docs/producthunt.md`](docs/producthunt.md).

## Status

Verified against the real API on 2026-10-07 with a developer token: every fixture under `testdata/` (except the
published `schema.graphql`) is a captured response, and `capture_test.go` re-captures them (`PH_CAPTURE=1
PH_TOKEN=…`). What the real API taught, each guarded by a test:

- a page is 20 items whatever `first` says (`first: 50` came back as 20 with `hasNextPage`), so comments and the
  leaderboard page with the cursor;
- a query above 500 000 complexity is refused: 20 comments × 20 replies was 1 925 608, × 5 replies fits (≈484 000);
- every user on a comment is `id "0"` / `[REDACTED]`; only `isViewer` is honest (makers and the viewer are the
  exceptions). "My comment" and "reply to me" go through `isViewer`; the username fields stay empty;
- the makers' pinned launch comment leads page one even in `NEWEST` order, so paging stops on the page's last
  edge, not its minimum.

## Design

- **Read-only by necessity**: the API's only mutations are goals and follows; there is no way to launch a product
  or post a comment.
- **Auth**: the application's developer token (does not expire, acts as the owner's account), sent as a bearer token.
  `viewer` resolves to that account, which is how "my products" (`madePosts`) and "reply to me" are found.
- **Comments**: `post.comments(order: NEWEST)` returns top-level comments; each is fetched with its newest 5
  `replies` (the complexity cap). Product Hunt threads are almost always one level deep. `is_reply_to_me` is the
  parent comment's `user.isViewer`; the watch follows up to 3 pages per post and stops once a page's last comment
  predates the cursor.
- **Leaderboard days are cut in Pacific time**, which is how Product Hunt's own daily ranking works.
- **Cursor**: newest `createdAt` pushed plus the ids pushed in a 300 s lookback window; the first round pushes
  nothing; the user's own comments are never pushed.
- **Field names follow Product Hunt's schema**; mapping onto a shape shared with other platforms is left to the
  workflow.

## Regenerating

```bash
go generate ./...
```
