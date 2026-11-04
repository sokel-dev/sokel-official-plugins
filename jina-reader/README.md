# Jina Reader

Page reading with normalized URLs, response formats and structured metadata through Jina Reader.

For users, see [the usage guide](docs/jina-reader.md). This README describes maintenance and protocol boundaries.

## Operations and deployment

| Operation | Official request | Result |
|---|---|---|
| `read` | `POST https://r.jina.ai/` with JSON `url` | Content and normalized metadata fields |
| `health_check` | `GET https://api.jina.ai/v1/batches?limit=1` | API key authentication only |

Example input:

```json
{"url":"https://example.com/article#section","format":"markdown","target_selector":"main","remove_selector":"nav","options":{"no_cache":false}}
```

The input URL must be absolute HTTP(S), cannot include user information, and has its fragment removed. It is sent in the JSON body; it is never an authenticated request destination. Reader format, selectors and cache controls use the documented headers. `metadata` is deliberately an opaque map because each website defines its own metadata keys. Missing content, missing URL, non-success envelopes and HTTP errors from the target page are rejected.

API key is optional. Anonymous `read` requests omit Authorization entirely. Anonymous health checks return `ok: false` with an explanation because there is no credential to verify. Authenticated health uses the read-only, HTTPBearer-protected batches listing from Jina's OpenAPI with `limit=1`; it does not create jobs or return job data. This does not guarantee Reader quota or every product's authorization.

`SOKEL_JINA_READER_BASE_URL` defaults to `https://r.jina.ai` and `SOKEL_JINA_API_BASE_URL` to `https://api.jina.ai`. Reader requests use a 150-second timeout; health uses 20 seconds.

## Contract and generation

`schema/schema.go` is the authoritative Go contract. `manifest.yml` is the exported distribution contract with plugin identity/version/document metadata. Optional numeric and boolean request fields live in named `options` structs as pointers so omitted values differ from explicit `false` or `0`. The generated registration uses SDK `contract.BindInput`, including nested `sokel` tags.

The standalone module depends only on the public SDK v0.7.8 and its public transitive dependencies. `zz_*.go` files come from the real SDK generator; never hand-edit them. English is the default contract/document language. `locales/zh-CN.json` maps source strings, including the complete usage document, to Chinese.

```sh
go mod tidy
go generate ./...
go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen check .
go test -race -timeout 60s ./...
go vet ./...
go build ./...
```

After editing `schema/`, export using `sokel-gen export yaml .` and retain `plugin.org`, `name`, `label`, `desc`, `version` and `doc` metadata in `manifest.yml`. Update translations when source strings change. Runtime registration sets version `0.1.1`; platform connection uses SDK `ENDPOINT`, `TOKEN` or supported `DEPLOY_KEY` handling.

## Transport boundaries

Deployment base URLs are environment variables, never user inputs or credential fields. HTTPS is mandatory except for loopback test servers. All redirects are rejected, including same-origin redirects. Credentials never follow target-page URLs or result URLs. Requests are bounded to 1 MiB, responses to 16 MiB, and operation contexts cancel network requests. HTTP errors, invalid JSON and service errors do not echo raw bodies or credentials. Submission endpoints have no automatic retries.

## Verification and evidence

Official documentation was reviewed on 2026-11-03. Acceptance tests assert the method, path/query, authorization and actual serialized JSON bytes received by a local HTTP server. They run through generated registration/binding and cover explicit false/zero versus omission, provider errors, malformed/oversized responses, cancellation/timeouts and redirect credential containment.

RED was established with compiling handlers that returned `not implemented`: `go test -run TestDocumentedWireRequests -count=1 ./...` failed at each documented operation before handlers were implemented. GREEN uses the implementation and local fixtures.

**These fixtures are documentation-derived examples and explicitly synthetic edge cases, not captures from a live account. No paid or credentialed live service was exercised.** Live-account verification remains a separate release check with an authorized test account. Schema-package functions are build-time declarations; runtime coverage is reported separately from those generated/export-only declarations.

Official references:

- https://docs.jina.ai/openapi.json
