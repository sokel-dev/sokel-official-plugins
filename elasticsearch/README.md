# elasticsearch — Elasticsearch plugin (first-party built-in directory)

18 operations cover three domains: search (search/count/aggregate), document read/write
(single-document CRUD + bulk + delete-by-query), index management
(list/create-delete/mapping/alias/reindex) + a `call` fallback + `health_check`.
User guide: [docs/elasticsearch.md](docs/elasticsearch.md).

**Uses plain HTTP instead of the official SDK**: ES's REST interface has barely changed
across 7/8/9, so plain HTTP lets one codebase cover all three major versions, and it even
works with **OpenSearch** (a fork of ES 7); the official SDK, on the other hand, locks the
major version to the client library. Same reasoning as `../kbstore-es`.

## Gotchas (read before touching the code)

- **`_bulk` is NDJSON, not JSON**: one action line, one document line, with a
  **required trailing newline**, and Content-Type must be `application/x-ndjson`
  (sending a JSON array gets a 400/406 from ES). Pinned by tests.
- **There are two kinds of 404**: a missing document (response has no `error` body) is a
  normal branch; a missing index (has an `error` body) is a config error that must be
  reported. Conflating them makes "couldn't find it" look like "no data". Pinned by tests.
- **Switching an alias must be remove+add inside a single `_aliases` request**: in the
  instant between two separate calls the alias would point to nothing or to both indices,
  and a query that lands right then reads the wrong data. Pinned by a test on call count.
- **`total` is a lower bound by default**: ES only counts exactly up to 10000; when
  `relation=gte`, the output sets `total_is_lower_bound=true`. Without surfacing this,
  "there are 10000 in total" would propagate straight into reports.
- **size=0 is meaningful** (want aggregations but not documents), while an empty numeric
  field also arrives as 0 — intent is inferred from "was aggs given": an explicit size>0
  wins, otherwise size=0 if aggs are present, and ES's default of 10 if neither is given.
- **Dangerous operations are blocked on the plugin side**: `_delete_by_query` with no
  query = wipe the whole index; `DELETE /*` = wipe the whole cluster. Both are rejected
  before the request is sent, pointing to the correct operation instead. Pinned by a
  "must not send the request" test.
- **`resource_already_exists` is an idempotent result, not an error**: index-create flows
  get rerun.
- Auth: API Key (`Authorization: ApiKey`) takes priority over basic; leaving both empty is
  also allowed (common when a self-hosted cluster has security disabled, or with
  OpenSearch's demo config).
- `InsecureSkipVerify` swaps out the Transport; **keep one http.Client for each of the two
  modes**, don't build a new one on every call — that would stop the connection pool from
  being reused and force a fresh TLS handshake on every request.

## No event source

ES has no push mechanism, so "is there a new document" can only be polled. If an
event-driven flow is needed, the upstream usually has a better-suited channel (a message
queue / Redis Stream / webhook). Add polling only if it's genuinely needed — don't bake
one in by default.

## Development

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```

Tests use an httptest fake ES and don't depend on an external cluster.
