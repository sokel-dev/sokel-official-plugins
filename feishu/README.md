# feishu — Feishu custom app plugin (first-party, self-hosted)

22 operations (messages/cards/user lookup/chat management/docs/bitable/drive + a `call`
fallback) + 3 events (message received / card button clicked / bot added to a chat). The
user-facing manual is [docs/feishu.md](docs/feishu.md). The group webhook bot is a **separate
plugin** (`../feishu-webhook`): the credential shape and authorization scope differ, so they
aren't pooled together.

## Why this is the first plugin in the whole codebase to pull in a vendor SDK

Every plugin before this one was plain HTTP. Feishu is the exception and pulls in
[larksuite/oapi-sdk-go](https://github.com/larksuite/oapi-sdk-go) v3 (MIT), for exactly two
reasons, both documented in the top comment of `client.go`:

1. **Long-lived event subscription (larkws)** — events don't arrive over a public webhook; the
   plugin actively opens a WebSocket to Feishu. The frame protocol is Feishu's own (protobuf),
   and implementing it ourselves would be both fragile and not worth it.
2. **tenant_access_token lifecycle** — obtaining/caching/refreshing on expiry, built into the SDK.

Usage is still kept minimal: REST calls always go through raw `client.Do` (paths/bodies built by
hand, matching the style of the other plugins); the typed modules are only used for multipart
uploads (im images/files, drive).

## Pitfalls hit/guarded against (read before touching the code)

- **content is a "JSON string," not JSON**: the content field of `/im/v1/messages` must be
  double-encoded (`jsonStr()`); getting this wrong shows up as invalid content.
- **Sending carries a uuid idempotency key** (`sendUUID`): derived from the platform's trace
  (run_id+node_id), so a workflow retry doesn't send twice. **It must not carry a uuid when
  there's no trace** — a constant key on test calls would silently dedup a second test call away
  into "not sent."
- **A non-zero business code is mostly HTTP 200**: `callRaw` catches this uniformly and turns it
  into an error; the `call` fallback operation is the exception (the user wants the raw code/msg
  to check against the docs) and goes through `callRawFull` instead.
- **Translation of common error codes** (`feishuErr`): 230002 = add the bot to the chat,
  99991672 = grant the permission and **republish the version**, 1254050 = share the table with
  the app. Feishu's msg is English aimed at developers, but whoever sees the error is a user on
  the canvas.
- **docx appends are capped at 50 blocks per call**: `appendBlocks` batches, since a long report
  with a few hundred blocks at once is routine.
- **Markdown-to-docx is a conservative line-level conversion**: inline bold/links are kept as
  plain text. docx's inline style model is an order of magnitude more complex; precise
  formatting goes through `call` directly against the blocks API.
- **The client is cached by app_id** (`clientOf`): creating a new one every time means
  re-exchanging the token every time, for free, against the rate limiter. Each test case gets
  its own app_id, otherwise the baseURL would cross over between tests.
- **baseURL's third branch passes a full URL through as-is**: both the httptest fake Feishu and
  a future private deployment rely on this. A test accidentally hitting the real
  open.feishu.cn shows up as `code:10003`.

## Event source (events.go)

Per-credential: one credential (= one app) gets one long-lived connection; multiple apps in a
single instance are managed by the SDK's source supervisor (the same mechanism as Telegram's
multi-bot setup). Dedup relies on Feishu's event_id, handed to the platform to process by
(pluginId, event, eventID) — the plugin doesn't build its own dedup table.

User-side prerequisite (documented in the docs): in the Open Platform's "Events & Callbacks,"
set the subscription method to **long connection**, and check `im.message.receive_v1` /
`im.chat.member.bot.added_v1`; card callbacks need the same long-connection setting.

## Files

| File | What it does |
|---|---|
| `schema/` | Contracts (source of truth); `go generate` produces `zz_*.go` |
| `client.go` | SDK client cache + raw calls + error code translation |
| `im.go` | Messages (send/reply/recall/upload), uuid idempotency |
| `misc.go` | User lookup/chat management/`call`/health_check |
| `content.go` | docx (including Markdown-to-blocks) / bitable / drive |
| `events.go` | larkws long connection -> three kinds of events |

## Development

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```
