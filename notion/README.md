# notion — read/write Notion + data-source-change triggers (first-party self-hosted plugin)

18 operations (search/get page/query database/create & update pages/markdown content/comments/file
upload...) + 2 events (row created / row updated). The user-facing manual is
[docs/notion.md](docs/notion.md).

## Three design decisions (see the top of `schema/schema.go` for details)

1. **Model around data sources, not the old `database_id` shape.** Since 2025-09-03, Notion split a
   database into a "container + data sources", and one database can hold multiple data sources;
   querying, creating rows, and relations all use `data_source_id`. A contract built on the old shape
   breaks across the board the moment a user hands it a multi-source database, and fixing it later is
   a breaking migration (n8n has already been through this once).
2. **Page content goes through markdown, the block tree is only a fallback.** `GET/PATCH
   /pages/{id}/markdown` turns "read a page for the model" / "let the model edit a page" into a
   single call; the block-level API is still kept because things like database blocks and embeds
   can't be expressed in markdown.
3. **Properties are given both ways**: `props` normalized (directly readable by downstream steps and
   models) and `properties_raw` as-is (a fallback for types like rollup/formula where normalization
   inevitably loses information).

## Two auth methods coexist

An internal integration secret (paste the `ntn_`-prefixed token into the credential — simplest for
self-hosting) **or** OAuth authorization (answered on the platform side by the notion provider). Both
are the same kind of thing — the credential that determines "which pages can this integration see" —
so they aren't split into two separate credential fields: if a token is set, use it; otherwise fall
back to the access_token obtained via authorization.

## Why the event source polls

Notion webhook subscriptions can **only be created by hand on its integration settings page** (and
you have to paste the verification_token back in to verify it); the API can't create them, and one
integration can only register one URL — "the plugin sets up the webhook for the user" simply isn't
an option here. For real-time updates, paste the canvas's webhook trigger node address into Notion
(it's in the manual).

## Files

| File | What it does |
|---|---|
| `schema/` | Contract (source of truth) |
| `client.go` | Outbound: auth, proxy, rate limiting (3 req/sec), error translation |
| `props.go` | Property normalization (the "normalized" half of giving both) |
| `read.go` / `write.go` | Read-side / write-side operations |
| `watch.go` | Event source: pulls incrementally via `last_edited_time` |

## Development

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```
