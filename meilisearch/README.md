# Meilisearch plugin

A standalone Sokel integration for self-hosted Meilisearch community servers. Its nine typed operations cover connection checking, index discovery and creation, full-text search, document retrieval/replacement/deletion and asynchronous task inspection. No cloud account, enterprise feature or external AI provider is required.

Read the [English user guide](docs/meilisearch.md) or [Chinese user guide](docs/meilisearch.zh-CN.md) for setup and operation semantics. The default contract and guide are English; `locales/zh-CN.json` translates the interface and includes the entire English guide as an exact source key.

## Service compatibility and configuration

Official API documentation and release information were checked on 2026-11-04. The implementation was exercised against [Meilisearch 1.56.1](https://github.com/meilisearch/meilisearch/releases/tag/v1.56.1) community edition. The upstream repository has [MIT community code and BUSL-1.1 enterprise code](https://github.com/meilisearch/meilisearch/blob/v1.56.1/LICENSE); see the [community license](https://github.com/meilisearch/meilisearch/blob/v1.56.1/LICENSE-MIT). This plugin uses community capabilities and does not require the enterprise edition. Do not describe the entire upstream repository as MIT-only. Older releases have not been exhaustively verified.

Set deployment variable `SOKEL_MEILISEARCH_URL`, for example `http://meilisearch:7700`. There is no public default or fallback. Internal HTTP and reverse-proxy path prefixes work; container `localhost` refers to that container, not a separate server. Credentials hold an optional API key, sent as a Bearer token. A regular key with suitable actions is sufficient; the master key is not required. Health uses an authenticated index-list request, so it requires `indexes.get` and may fail for a valid search-only key. The plugin does not install or manage the server.

`schema/schema.go` is authoritative. Generated `zz_*.go` files come from public SDK `v0.7.7`; do not hand-edit them. `manifest.yml` is a real schema export plus release/deployment metadata. Runtime, manifest and `plugin.version` use `0.1.0`. The independent module has no local replacements or private dependencies.

## Implementation decisions

Document schemas belong to the application. Input document arrays and output objects are JSON strings so arbitrary nested fields and integers above 2^53 survive without conversion through floating point. Metadata is typed, and upstream camelCase fields are mapped explicitly to the contract's snake_case names. Optional numbers, empty projection arrays and false values retain their intended meaning.

Create, add-or-replace and delete return a task receipt, not mutation success. HTTP 202 means enqueued. `get_task` makes one read and returns distinct pending, successful, failed and canceled states without polling or retrying. It exposes stable error codes without raw task error messages that might reveal document content. Ingestion uses full document replacement, so omitted fields are removed; it is not partial update. The live service confirmed `documentDeletion` receipts, avoiding an inconsistent example on the upstream deletion reference page.

Index-list pagination checks the actual offset/limit/total. Search exposes a possible next page based on page fullness, not on the estimated total. The server's `maxTotalHits` still limits search; this is not a full export operation. Filterable/sortable attribute settings are managed outside the plugin. Index deletion, key management, enterprise APIs and AI embedder configuration are outside this contract.

The HTTP client fixes the deployment origin, rejects redirects, removes mutation-body replay, caps request/response bytes and timeouts, and does not retry failed HTTP responses. Errors do not echo upstream bodies, service URLs or credentials. A timeout can leave a submitted task uncertain; inspect the service before repeating a write.

## Development and verification

Run from this directory. SDK generation/export/check must be sequential within a plugin:

```sh
GOWORK=off GOCACHE=/tmp/sokel-go-cache go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen generate .
GOWORK=off GOCACHE=/tmp/sokel-go-cache go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen check .
GOWORK=off GOCACHE=/tmp/sokel-go-cache go test -race ./...
GOWORK=off GOCACHE=/tmp/sokel-go-cache go vet ./...
GOWORK=off GOCACHE=/tmp/sokel-go-cache go build -o /tmp/sokel-meilisearch .
```

The initial generated contract used compilable `not implemented` handlers. `go test -run '^TestDocumentedWireRequests$' -count=1 ./...` failed for all nine operations before implementation and passed afterward. Its exact-wire fixtures adapt official request/response examples to synthetic IDs and edge values; they are not service recordings. Official sources include [authorization](https://www.meilisearch.com/docs/reference/api/authorization), [indexes](https://www.meilisearch.com/docs/reference/api/indexes/list-all-indexes), [search](https://www.meilisearch.com/docs/reference/api/search/search-with-post), [document replacement](https://www.meilisearch.com/docs/reference/api/documents/add-or-replace-documents) and [task inspection](https://www.meilisearch.com/docs/reference/api/async-task-management/get-task). Tests cover generated dispatch, camelCase mapping, optional false/zero, exact large numbers, pagination, application receipts, failed tasks, credential redaction, redirects, size bounds and timeouts. A separate mutation-body replay regression also failed before its fix.

`TestLocalServiceAcceptance` was run with the race detector against an isolated, loopback-only Meilisearch 1.56.1 community container with empty temporary storage and a public test-only key. It created and removed a disposable index, tested every operation family, and checked both successful and failed asynchronous tasks. Actual sanitized request/response captures are in `testdata/local-service-captures.json`; `TestCapturedServiceResponses` replays them offline. See [fixture provenance](testdata/README.md). No production data or external account was involved.

The live test is skipped by default. To repeat it, provision a disposable local server using master key `wave6-local-test-only-key`, then run:

```sh
SOKEL_LIVE_MEILISEARCH_URL=http://127.0.0.1:17700 GOWORK=off GOCACHE=/tmp/sokel-go-cache go test -race -run '^TestLocalServiceAcceptance$' -count=1 -v .
```

This command writes test data and removes its temporary index. It is restricted to an explicit loopback HTTP URL and must target an isolated test instance, never existing data. The test's bounded polling is test orchestration only, not production behavior. `SOKEL_LIVE_FIXTURE_DIR` may point to a directory under `/tmp` to record new captures without authentication headers or server origins.
