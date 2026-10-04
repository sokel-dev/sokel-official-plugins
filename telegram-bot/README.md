# telegram-bot — the send/operate side of a Telegram Bot (first-party self-hosted plugin)

16 operations (send message/photo/document, edit, delete, answer callback query, webhook
management…) + 4 events (message received / message edited / button tapped / bot membership
status changed).
The user-facing manual is [docs/telegram-bot.md](docs/telegram-bot.md).

## Two design decisions

1. **One generic `call` + a set of typed convenience operations.** `call` takes `method` + `params`
   and covers the **entire** Bot API — when Telegram adds a new method, this needs zero code
   change. The dozen or so commonly used ones are also built as typed operations, because what the
   canvas needs is "fill in fields", not "hand-assemble JSON". The two aren't redundant: one is the
   fallback, the other is the convenient path.
2. **bot_token never goes into a node's inputs or outputs.** It's only spliced into the URL path
   inside the plugin — once a token shows up in an input, it spreads through run records, canvas
   variables, and logs, and that can't be undone.

## Scope: only the "send/operate" half

A full-featured TG bot splits into two halves — send/operate (this plugin, pure outbound calls to
`api.telegram.org`) and receive/trigger (a message comes in → a workflow starts). The latter relies
on the platform's **event source** mechanism (the long poll in `updates.go`), rather than making
the user stand up a public webhook.

## Files

| File | What it does |
|---|---|
| `schema/` | Contracts (source of truth) |
| `updates.go` | Event source: getUpdates long poll → 4 event types |
| `webhook_ops.go` | Set/query/delete the webhook (mutually exclusive with long polling — don't run both) |

## Development

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```
