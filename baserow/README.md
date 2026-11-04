# Baserow plugin

Independent Sokel module with eight typed operations for a self-hosted Baserow Open Source Edition server: token health, table and field discovery, row listing/retrieval/creation/update/deletion. Read the [English guide](docs/baserow.md) or [Chinese guide](docs/baserow.zh-CN.md). No cloud account, premium endpoint or external AI service is required.

## Contract and compatibility

`schema/schema.go` is authoritative, and public SDK `v0.7.8` generates `zz_*.go`; never edit generated files. `manifest.yml` is an actual schema export with release and deployment metadata. The module has no local replacements or private dependencies. Runtime, manifest and `plugin.version` use `0.1.1`. The container reference is the intended version tag, not a claim that an image has been published.

Set required deployment variable `SOKEL_BASEROW_URL` to the server root before `/api`. Internal HTTP and reverse-proxy path prefixes work; there is no public fallback. Container `localhost` identifies the plugin container, not a separate Baserow container. Credentials hold only the required database token.

Checked on 2026-11-04 against the [official 2.4.1 release](https://github.com/baserow/baserow/releases/tag/2.4.1), the [official API schema](https://api.baserow.io/api/schema.json), [database-token documentation](https://baserow.io/user-docs/personal-api-tokens) and [tagged table API implementation](https://github.com/baserow/baserow/blob/2.4.1/backend/src/baserow/contrib/database/api/tables/views.py). The latter confirms that `/api/database/tables/all-tables/` is specifically authenticated by database token; general account/JWT table-management endpoints are not substituted. The [license](https://github.com/baserow/baserow/blob/2.4.1/LICENSE) covers community code under MIT, with separately licensed premium, enterprise, documentation and third-party components.

## Design and limits

The token is sent only in the Authorization header. Health uses authenticated table discovery and returns `ok=false` on failure. A table appearing in discovery does not guarantee every row permission. Field metadata includes exact JSON definitions. Dynamic row inputs and complete outputs are JSON strings, preserving arbitrary field schemas, null/false/zero and integers above 2^53. Identifiers and counts are decimal strings. PATCH updates only supplied fields; a create/update requires the documented HTTP 200 row response, while deletion requires HTTP 204 without a body.

List rows performs one bounded page. It validates continuation origin/path/query and count consistency, then exposes a numeric next page. It never follows response URLs. Reverse proxies must produce continuation URLs matching the configured service. Inputs permit internal HTTP and prefixes but reject embedded credentials, fragments, queries and traversal. Transport rejects redirects, removes mutation-body replay, bounds time and bytes, redacts upstream bodies and does not retry failed HTTP responses. Schema changes, file uploads, batch mutations and arbitrary API calls are outside this release.

## Development and verification

Run commands from this directory. Do not run SDK generation, export or check concurrently for the same module.

```sh
GOWORK=off GOCACHE=/tmp/sokel-go-cache go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen generate .
GOWORK=off GOCACHE=/tmp/sokel-go-cache go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen check .
GOWORK=off GOCACHE=/tmp/sokel-go-cache go test -race ./...
GOWORK=off GOCACHE=/tmp/sokel-go-cache go vet ./...
GOWORK=off GOCACHE=/tmp/sokel-go-cache go build -o /tmp/sokel-baserow .
```

After declaration and real generation, compilable handlers returning `not implemented` made `go test -run '^TestDocumentedWireRequests$' -count=1 ./...` fail for all eight operations. Implementation then made it pass. These tests invoke the actual generated registration/binding path and inspect exact HTTP requests, headers and responses. Additional tests cover false/zero, precise IDs, arbitrary JSON, pagination failures, invalid business responses, HTTP status/media types, body limits, cancellation, timeouts and credential redaction.

`testdata/documented-wire.json` is synthetic and adapted from official endpoint schemas and examples; it is not a recording from a Baserow server. No live Baserow acceptance or platform-debug invocation has been performed. See [fixture provenance](testdata/README.md). No existing accounts, production data, commits, image publication or registry release are part of these checks. English is the default interface and guide language; the Chinese locale contains all supported interface strings and the exact whole English guide mapped to its complete Chinese translation.
