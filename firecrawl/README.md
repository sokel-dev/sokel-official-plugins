# Firecrawl

Web scraping, crawling, URL discovery and schema-guided extraction through the official v2 API.

For users, see [the usage guide](docs/firecrawl.md). This README describes maintenance and protocol boundaries.

## Operations and deployment

| Operation | Official request | Result |
|---|---|---|
| `scrape` | `POST /v2/scrape` | Markdown, HTML, links and typed metadata |
| `crawl` | `POST /v2/crawl` | Asynchronous job ID |
| `crawl_status` | `GET /v2/crawl/{id}?skip=N` | Status, page results and next-page URL |
| `map` | `POST /v2/map` | Typed discovered links |
| `extract` | `POST /v2/extract` | Asynchronous extraction ID |
| `extract_status` | `GET /v2/extract/{id}` | Status and schema-defined object |
| `health_check` | `GET /v2/team/credit-usage` | Credential acceptance and nonzero credits |

Examples of operation inputs:

```json
{"url":"https://example.com","formats":["markdown"],"options":{"only_main_content":false,"wait_for":0}}
```

```json
{"url":"https://example.com","options":{"limit":10,"max_discovery_depth":1}}
```

```json
{"urls":["https://example.com"],"schema":{"type":"object","properties":{"title":{"type":"string"}}}}
```

The supported format subset is Markdown, HTML and links. Crawl results request Markdown. Submit operations do not automatically retry; retries can create a second billable job. Poll using a workflow delay. Pagination preserves the original job ID and an explicit `options.skip`; the client never follows the `next` URL. Failed/cancelled crawl or extraction jobs return errors.

`SOKEL_FIRECRAWL_BASE_URL` defaults to `https://api.firecrawl.dev`. API key is required. Requests use a 150-second total timeout; health checks use 20 seconds. Health checking is read-only and returns `ok: false` for unavailable credentials or zero credits.

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

- https://docs.firecrawl.dev/api-reference/endpoint/scrape
- https://docs.firecrawl.dev/api-reference/endpoint/crawl-post
- https://docs.firecrawl.dev/api-reference/endpoint/crawl-get
- https://docs.firecrawl.dev/api-reference/endpoint/map
- https://docs.firecrawl.dev/api-reference/endpoint/extract
- https://docs.firecrawl.dev/api-reference/endpoint/extract-get
- https://docs.firecrawl.dev/api-reference/endpoint/credit-usage
