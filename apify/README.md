# Apify

For user setup, operations and examples, see [the usage guide](docs/apify.md).

## Maintenance and protocol choices

Apify uses `Authorization: Bearer`, never a token in query parameters. `run_actor` sends the input JSON as the entire POST body to `/v2/actors/{actorId}/runs`, sets waitForFinish=0 and restartOnError=false, and never waits for completion. Each Actor owns its input schema and billing behavior. The six operations cover Actor start/status/history, dataset metadata/items and read-only token health.

Dataset items are JSON arrays of Actor-defined objects. The implementation requests raw JSON without clean/unwind/skip-empty transformations, reads the four official X-Apify-Pagination headers, checks count against the decoded array, and exposes next_offset/has_more. Missing metadata, count mismatches or an empty page before the reported total fail visibly instead of silently truncating the dataset. This is offset pagination, not a snapshot; document mutations can shift pages. Dataset metadata counts may lag behind writes. Actor and dataset resource IDs accept `owner~name` but never arbitrary paths or URLs.

## Contract and code generation

`schema/schema.go` is authoritative. `manifest.yml` is its real SDK YAML export plus plugin identity, version, label, description and document metadata. Keep those metadata fields when exporting: the Go exporter alone emits only plugin.name. Optional input numerics/booleans use pointers in named option structs. Generated binding uses `contract.BindInput`; handwritten nested types carry `sokel` tags. Never edit `zz_*.go`.

This standalone Go module pins public `github.com/sokel-dev/sokel-plugin-sdk v0.7.8` and has no private dependency or local replace. English is the default interface and documentation language; `locales/zh-CN.json` contains exact English-source keys and the complete usage document. Source-only runtime labels are not catalog locale keys.

```sh
go mod tidy
go generate ./...
go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen check .
go test -race -timeout 60s ./...
go vet ./...
go build ./...
```

Export with the pinned generator, preserve distribution metadata, and validate first-release output using `public/scripts/registry-entry.py` in a temporary catalog followed by SDK manifest loading. Do not invent a production image digest or treat the temporary fixture image as released.

## Transport and deployment

Set the deployment base with `SOKEL_APIFY_BASE_URL`; credentials remain in the platform credential form. Runtime uses SDK ENDPOINT/TOKEN or its supported DEPLOY_KEY flow, and registers version 0.1.1. HTTPS is required except loopback fixtures. All redirects are rejected. Request and response bodies are bounded to 1 MiB and 16 MiB. Context cancellation and HTTP deadlines stop requests. Errors expose neither raw upstream bodies nor credentials. Mutation submissions have no automatic retries; a connection failure after upstream acceptance is ambiguous.

## Test evidence and release limits

`TestDocumentedWireRequests` went RED against compiling handlers returning `not implemented` before the implementation was written. All six operation cases failed under `go test -run TestDocumentedWireRequests -count=1 ./...`. GREEN tests run through generated registration and inspect actual HTTP method, path/query, authorization and serialized JSON bytes received by a local server. Boundary tests cover missing credentials, malformed and oversized payloads, HTTP errors, cancellation, timeouts, redirects and omission semantics.

Examples are derived from the official API documentation reviewed on 2026-11-03, with explicitly synthetic edge cases. They are **not live-account captures**. No paid search, research run or Actor execution was performed. Live credentials, billing semantics, real Actor compatibility and platform deployment remain unverified. Runtime coverage is reported separately from the build-time schema package.

Official references:

- https://docs.apify.com/api/v2/actors-runs-post
- https://docs.apify.com/api/v2/actor-run-get
- https://docs.apify.com/api/v2/actors-runs-get
- https://docs.apify.com/api/v2/dataset-get
- https://docs.apify.com/api/v2/dataset-items-get
- https://docs.apify.com/api/v2/users-me-get
