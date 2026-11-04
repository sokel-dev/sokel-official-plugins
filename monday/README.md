# Monday plugin

Independent Go module for the Sokel official Monday integration. Read [the user guide](docs/monday.md) for credentials, operation limits and service behavior. Default interface and guide text is English; `locales/zh-CN.json` contains exact English source keys and the full translated guide.

## Contract and implementation

`schema/schema.go` is authoritative. The manifest is a real public-SDK YAML export plus release metadata. Generated `zz_*.go` files must be regenerated, never edited by hand. The runtime and manifest version are `0.1.0`; the module depends only on public SDK `v0.7.7` with no local replacements.

The HTTP client fixes the official origin, rejects redirects, bounds request/response bytes, redacts upstream bodies and URLs from errors, and never retries writes. Authentication is carried in request headers. Each list handler performs one request and exposes service-specific pagination explicitly. The guide documents intentionally unsupported operations and account/region limitations.

## Development checks

```sh
GOWORK=off GOCACHE=/tmp/sokel-go-cache go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen generate .
GOWORK=off GOCACHE=/tmp/sokel-go-cache go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen check .
GOWORK=off GOCACHE=/tmp/sokel-go-cache go test -race ./...
GOWORK=off GOCACHE=/tmp/sokel-go-cache go vet ./...
GOWORK=off GOCACHE=/tmp/sokel-go-cache go build -o /tmp/sokel-monday .
```

The initial contract and generated code were exercised with compilable handlers returning `not implemented`: `go test -run TestDocumentedWireRequests -count=1 ./...` failed for each operation before implementation, then passed after implementation. These are synthetic, documentation-derived fixtures, not live service recordings. Tests inspect actual HTTP request bytes and generated input binding, then cover invalid business receipts, credentials, pagination, timeouts, redirects and size bounds. No real account writes, container publication or registry release were performed. Root-level release-fixture checks are managed separately.

## Upstream fixture basis

Official documentation links are recorded in the user guide, checked on 2026-11-04. The pinned API version is 2026-10. Acceptance fixtures retain the documented endpoint, request envelope and response field shapes, then adapt identifiers and account data to synthetic values. Boundary cases deliberately alter optional values, IDs above 2^53, page cursors, error envelopes and receipt completeness. They are not service-captured recordings and cannot replace live account acceptance. HTTP transport fakes keep credentials and writes entirely local.
