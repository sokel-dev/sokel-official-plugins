# bluesky — Bluesky publisher plugin

Post, thread, and delete. **The first template for the unified publish contract** (see
[docs/social-publishing-plugins.md](../../docs/social-publishing-plugins.md) §3): publish
operations always return `id` + `url`, the plugin owns splitting long content, and media
travels along with the publish call.

The user-facing guide is [docs/bluesky.md](docs/bluesky.md) (shipped with the handshake;
it's what shows as "usage doc" in the UI).

## Why it was chosen first

Zero approval process, zero cost, no per-call billing — onboarding cost is close to zero,
making it a good way to validate the whole chain end to end: "publish contract +
credential + channel rate limiting + publish ledger + retry on failure". These five things
are the same on any platform, and are the real work behind this batch of publisher plugins
(something like X, which bills per call, or WeChat, which requires a verified official
account, isn't a good fit for nailing down the contract).

## Four design decisions

1. **facets are computed by UTF-8 byte offset** ([richtext.go](richtext.go)). Bluesky
   doesn't auto-detect URLs in the text, so rich-text annotations must be given
   explicitly; and an annotation's index is a byte offset — indexing by character count
   misplaces everything when CJK and ASCII are mixed (each CJK character is off by 2
   bytes). Anyone hand-rolling this integration trips over it nine times out of ten, so
   the plugin handles it.
2. **Link cards are built by default** ([embed.go](embed.go)). Eight times out of ten,
   financial content is "one sentence + one link", and without a card it's just a bare
   URL. A failed OG fetch **does not count as a publish failure** — falling back to a
   plain text link still gets it sent.
3. **Images are sent along with the publish call**, not as a separate upload operation:
   an unreferenced blob gets garbage collected, so waiting behind a human-approval node in
   between would always fail. (X's video needs chunking + transcoding, which is what would
   justify a standalone operation.)
4. **The session is self-managed by the plugin**: `accessJwt` only lives a few minutes,
   while `refreshJwt` is the long-lived credential. The credential stores "identifier +
   app password"; the short-lived session is cached in-process, and a 401 first tries a
   refresh, falling back to a fresh login.

## Files

| File | What it does |
|---|---|
| `schema/schema.go` | Operation and credential contracts (**source of truth**; run `go generate` after editing) |
| `client.go` | Session (login/renewal/caching), XRPC, error translation, address conversion |
| `richtext.go` | Facets: links/hashtags/mentions, by byte offset |
| `embed.go` | Image blob upload, link card (OG fetching) |
| `post.go` | Post/thread/delete/health check |

## Development

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```

Tests use a fake PDS ([bluesky_test.go](bluesky_test.go)), pinning down four things that
would definitely be wrong without a test: byte offsets, a thread's root/parent refs,
automatic renewal after session expiry, and a failed card fetch not blocking the publish.

## Not done

- **Video**: Bluesky supports up to 100MB/3 minutes, but that needs a separate upload and
  transcoding wait — add it once there's an actual use case.
- **Reads** (search/timeline/notifications): this plugin is positioned as a publisher.
  If sentiment monitoring is needed, extend it separately, shaped like
  `plugin-builtin/x`'s incremental cursor.
