# Exa

For user setup, operations and examples, see [the usage guide](docs/exa.md).

## Maintenance and protocol choices

Exa uses the documented `x-api-key` header. Current asynchronous research uses `/agent/runs`, not legacy `/research/v1`. Search/content highlights are enabled only if neither text nor highlights was specified. Numeric and boolean option pointers preserve omission versus false/zero. Answer and Agent structured output is restricted to object-root schemas; the output maps have explicit schema-dependent reasons. Individual Contents failures and failed Agent runs are data for workflow branching. Health reads Agent runs without starting a job; product-scoped keys may fail this health endpoint even when search works.

The official Agent budget is effort-dependent: maxCostDollars $1-100 for auto/ultra, maxDurationSeconds 300-10800 only for ultra. No beta header, Connect third-party data sources, streaming, cancel or automatic retries are implemented. Search filters use RFC3339 dates and the supported public result cap is 100. The module exposes six operations.

## Contract and code generation

`schema/schema.go` is authoritative. `manifest.yml` is its real SDK YAML export plus plugin identity, version, label, description and document metadata. Keep those metadata fields when exporting: the Go exporter alone emits only plugin.name. Optional input numerics/booleans use pointers in named option structs. Generated binding uses `contract.BindInput`; handwritten nested types carry `sokel` tags. Never edit `zz_*.go`.

This standalone Go module pins public `github.com/sokel-dev/sokel-plugin-sdk v0.7.7` and has no private dependency or local replace. English is the default interface and documentation language; `locales/zh-CN.json` contains exact English-source keys and the complete usage document. Source-only runtime labels are not catalog locale keys.

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

Set the deployment base with `SOKEL_EXA_BASE_URL`; credentials remain in the platform credential form. Runtime uses SDK ENDPOINT/TOKEN or its supported DEPLOY_KEY flow, and registers version 0.1.0. HTTPS is required except loopback fixtures. All redirects are rejected. Request and response bodies are bounded to 1 MiB and 16 MiB. Context cancellation and HTTP deadlines stop requests. Errors expose neither raw upstream bodies nor credentials. Mutation submissions have no automatic retries; a connection failure after upstream acceptance is ambiguous.

## Test evidence and release limits

`TestDocumentedWireRequests` went RED against compiling handlers returning `not implemented` before the implementation was written. All six operation cases failed under `go test -run TestDocumentedWireRequests -count=1 ./...`. GREEN tests run through generated registration and inspect actual HTTP method, path/query, authorization and serialized JSON bytes received by a local server. Boundary tests cover missing credentials, malformed and oversized payloads, HTTP errors, cancellation, timeouts, redirects and omission semantics.

Examples are derived from the official API documentation reviewed on 2026-11-03, with explicitly synthetic edge cases. They are **not live-account captures**. No paid search, research run or Actor execution was performed. Live credentials, billing semantics, real Actor compatibility and platform deployment remain unverified. Runtime coverage is reported separately from the build-time schema package.

Official references:

- https://exa.ai/docs/reference/search
- https://exa.ai/docs/reference/get-contents
- https://exa.ai/docs/reference/answer
- https://exa.ai/docs/reference/agent-api/create-a-run
- https://exa.ai/docs/reference/agent-api/get-a-run
- https://exa.ai/docs/reference/agent-api/list-runs
