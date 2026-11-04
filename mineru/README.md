# MinerU

Asynchronous document parsing using the public MinerU v4 precision extraction API.

For users, see [the usage guide](docs/mineru.md). This README describes maintenance and protocol boundaries.

## Operations and deployment

| Operation | Official request | Result |
|---|---|---|
| `submit` | `POST /api/v4/extract/task` | Task ID and trace ID |
| `status` | `GET /api/v4/extract/task/{id}` | Typed state, progress and result ZIP URL |
| `batch_submit` | `POST /api/v4/extract/task/batch` | Batch ID and trace ID |
| `batch_status` | `GET /api/v4/extract-results/batch/{id}` | Per-document typed results |
| `health_check` | Existing task status only | Credential verification without a parse |

Examples of operation inputs:

```json
{"url":"https://cdn-mineru.openxlab.org.cn/demo/example.pdf","model_version":"vlm","options":{"enable_formula":false,"enable_table":true}}
```

```json
{"files":[{"url":"https://cdn-mineru.openxlab.org.cn/demo/example.pdf","data_id":"document-1","is_ocr":false}],"model_version":"vlm"}
```

```json
{"task_id":"existing-task-id"}
```

Supported models are `pipeline`, `vlm` and `MinerU-HTML`; an omitted model leaves the provider default intact. Batch submission accepts 1–200 files. Optional `extra_formats` are docx/html/latex; Markdown and JSON are already included by MinerU. The result ZIP URL is exposed as `result.full_zip_url` (or an item in `results`), alongside typed progress/state. Archives are not automatically downloaded/unpacked. No compatibility with self-hosted MinerU or its anonymous lightweight API is implied.

File/page quotas remain provider/account controlled. Submission is not automatically retried, because a repeat can create another task. Poll with workflow delays. A per-document `failed` state is returned as typed data so a workflow can handle partial batches; HTTP and API-envelope errors fail the operation.

`SOKEL_MINERU_BASE_URL` defaults to `https://mineru.net`; token is required. Requests use a 45-second timeout. There is no documented general-purpose read-only token endpoint: health requires an existing `task_id`. Without it, health returns `ok: false` and explains how to verify in the debug panel. It never creates a parsing task just to check credentials.

## Contract and generation

`schema/schema.go` is the authoritative Go contract. `manifest.yml` is the exported distribution contract with plugin identity/version/document metadata. Optional numeric and boolean request fields live in named `options` structs as pointers so omitted values differ from explicit `false` or `0`. The generated registration uses SDK `contract.BindInput`, including nested `sokel` tags.

The standalone module depends only on the public SDK v0.7.7 and its public transitive dependencies. `zz_*.go` files come from the real SDK generator; never hand-edit them. English is the default contract/document language. `locales/zh-CN.json` maps source strings, including the complete usage document, to Chinese.

```sh
go mod tidy
go generate ./...
go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen check .
go test -race -timeout 60s ./...
go vet ./...
go build ./...
```

After editing `schema/`, export using `sokel-gen export yaml .` and retain `plugin.org`, `name`, `label`, `desc`, `version` and `doc` metadata in `manifest.yml`. Update translations when source strings change. Runtime registration sets version `0.1.0`; platform connection uses SDK `ENDPOINT`, `TOKEN` or supported `DEPLOY_KEY` handling.

## Transport boundaries

Deployment base URLs are environment variables, never user inputs or credential fields. HTTPS is mandatory except for loopback test servers. All redirects are rejected, including same-origin redirects. Credentials never follow target-page URLs or result URLs. Requests are bounded to 1 MiB, responses to 16 MiB, and operation contexts cancel network requests. HTTP errors, invalid JSON and service errors do not echo raw bodies or credentials. Submission endpoints have no automatic retries.

## Verification and evidence

Official documentation was reviewed on 2026-11-03. Acceptance tests assert the method, path/query, authorization and actual serialized JSON bytes received by a local HTTP server. They run through generated registration/binding and cover explicit false/zero versus omission, provider errors, malformed/oversized responses, cancellation/timeouts and redirect credential containment.

RED was established with compiling handlers that returned `not implemented`: `go test -run TestDocumentedWireRequests -count=1 ./...` failed at each documented operation before handlers were implemented. GREEN uses the implementation and local fixtures.

**These fixtures are documentation-derived examples and explicitly synthetic edge cases, not captures from a live account. No paid or credentialed live service was exercised.** Live-account verification remains a separate release check with an authorized test account. Schema-package functions are build-time declarations; runtime coverage is reported separately from those generated/export-only declarations.

Official references:

- https://mineru.net/apiManage/docs
