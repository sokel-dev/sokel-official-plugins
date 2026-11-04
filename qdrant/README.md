# Qdrant plugin

A standalone Sokel integration for self-hosted Qdrant. The nine typed operations cover connection checking, collection discovery and creation, dense-vector queries, point upsert/retrieval/scrolling and explicit-ID deletion. No cloud account, paid feature or embedding service is required. This plugin accesses Qdrant directly; it does not configure the platform knowledge-base storage backend.

Read the [English user guide](docs/qdrant.md) or [Chinese user guide](docs/qdrant.zh-CN.md) for setup and operation semantics. The default contract and guide are English; `locales/zh-CN.json` translates the interface and includes the entire English guide as an exact source key.

## Service compatibility and configuration

Official API documentation and release information were checked on 2026-11-04. The implementation was exercised against [Qdrant 1.20.0](https://github.com/qdrant/qdrant/releases/tag/v1.20.0), an [Apache-2.0 licensed server](https://github.com/qdrant/qdrant/blob/v1.20.0/LICENSE). It uses the standard community REST API and makes no claim of exhaustive compatibility with older releases. See the guide's official endpoint references, including [point queries](https://api.qdrant.tech/api-reference/search/query-points), [upsert](https://api.qdrant.tech/api-reference/points/upsert-points) and [scroll](https://api.qdrant.tech/api-reference/points/scroll-points).

Set deployment variable `SOKEL_QDRANT_URL`, for example `http://qdrant:6333`. There is no default or public fallback. Internal HTTP and reverse-proxy path prefixes are supported. In a container, `localhost` identifies that container, not another service. The optional credential contains only the API key, sent in the `api-key` header. Health reads collections and does not mutate data. The plugin does not install or manage the server.

`schema/schema.go` is authoritative. Generated `zz_*.go` files come from public SDK `v0.7.7`; do not hand-edit them. `manifest.yml` is a real schema export plus release/deployment metadata. Runtime, manifest and `plugin.version` use `0.1.0`. The module has no local replacements or private dependencies.

## Implementation decisions

Point IDs are strings in Sokel and are converted to exact uint64 JSON integers or UUID strings on the wire. Payload and filter objects remain JSON strings because they have application-defined schemas; this also preserves integers above 2^53. Vectors are typed finite number arrays. Reads omit vectors. Named dense vectors are supported; sparse vectors, inference and collection deletion are outside the contract.

Point mutations request `wait=true` and require `status=completed`; an acknowledgment is not reported as completion. Reads and writes inspect the Qdrant status envelope, and scroll requires valid continuation metadata. Pagination performs one request per invocation. Approximate collection counts are not exact cardinality claims.

The HTTP client fixes the deployment origin, rejects redirects, removes mutation-body replay, caps timeout and request/response sizes, and never retries failed HTTP responses. Errors do not include raw upstream bodies, service URLs or credentials. Explicit false/zero values and optional fields are preserved. A timeout can leave a write uncertain; inspect stored data before retrying.

## Development and verification

Run from this directory. SDK generation/export/check must be sequential within a plugin:

```sh
GOWORK=off GOCACHE=/tmp/sokel-go-cache go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen generate .
GOWORK=off GOCACHE=/tmp/sokel-go-cache go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen check .
GOWORK=off GOCACHE=/tmp/sokel-go-cache go test -race ./...
GOWORK=off GOCACHE=/tmp/sokel-go-cache go vet ./...
GOWORK=off GOCACHE=/tmp/sokel-go-cache go build -o /tmp/sokel-qdrant .
```

The initial generated contract used compilable `not implemented` handlers. `go test -run '^TestDocumentedWireRequests$' -count=1 ./...` failed for all nine operations before implementation and passed afterward. These fixtures adapt official documentation examples to synthetic resource names, false/zero inputs and large IDs; they are not live recordings. Tests inspect exact request bytes and generated dispatch, malformed application receipts, pagination, status handling, redirects, credential redaction, body limits and timeouts. A separate body-replay regression also failed before the transport fix.

`TestLocalServiceAcceptance` was run with the race detector against an isolated, loopback-only Qdrant 1.20.0 container with empty temporary storage and a public test-only key. It created and removed disposable collections, verified every operation family, and preserved the maximum uint64 point ID and a payload integer above 2^53. Actual sanitized request/response captures are in `testdata/local-service-captures.json`; `TestCapturedServiceResponses` replays them offline. See [fixture provenance](testdata/README.md). No production data, external account, image publication or registry release was involved.

The live test is skipped by default. To repeat it, first provision a disposable local server with API key `wave6-local-test-only-key`, then run:

```sh
SOKEL_LIVE_QDRANT_URL=http://127.0.0.1:16333 GOWORK=off GOCACHE=/tmp/sokel-go-cache go test -race -run '^TestLocalServiceAcceptance$' -count=1 -v .
```

This command writes test data and removes its temporary collection. It is intentionally restricted to an explicit loopback HTTP URL and must target an isolated test instance, never an existing development or production dataset. Setting `SOKEL_LIVE_FIXTURE_DIR` to a directory under `/tmp` records new captures without authentication headers or server origins.
