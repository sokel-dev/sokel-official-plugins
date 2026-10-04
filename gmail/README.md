# gmail — read Gmail + trigger on new messages (first-party self-hosted plugin)

Three operations (list messages / read a single message / get attachments) + one event (new
message received). The user-facing manual is [docs/gmail.md](docs/gmail.md).

## Three design decisions

1. **Read-only for the first release** (`gmail.readonly`). Marking as read and sending mail
   would need `gmail.modify` / `gmail.send`, and Gmail scopes are a Google **restricted
   scope** — the more you request, the harder the security review gets. Add them when there's
   an actual use case; don't get the whole app stuck in review for "might need it later."
2. **The credential is owned by the plugin** (belongs to an access group like any other plugin),
   but it's obtained via OAuth: the user clicks "authorize" once -> consent screen ->
   refresh_token lands on the **platform**. All the plugin gets is the access_token the platform
   just exchanged — **it never has a client_secret and never handles a refresh_token** — this
   boundary is the standard pattern for all OAuth-based plugins (see `docs/dev-playbook.md`
   §4.4).
3. **The event source uses the History API instead of polling the inbox** (`history.go`): asking
   for incremental changes since the last historyId is far cheaper than listing everything every
   round, and it doesn't miss "a message arrived again after being read."

## Files

| File | What it does |
|---|---|
| `schema/` | Operation/event/credential contracts (**source of truth**; run `go generate` after editing) |
| `api.go` | Outbound Gmail REST calls (auth, error translation, pagination) |
| `message.go` | Message object normalization (MIME parsing, body/attachment extraction) |
| `history.go` | Event source: incremental fetching by historyId |

## Development

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```

## Not done

Sending mail, marking as read, changing labels — all need broader scopes (see decision 1). If
you actually need to send mail, going through SMTP is simpler.
