# Box plugin development

The Go module is independent and pins the public Sokel Plugin SDK v0.7.8. The authoritative operation and credential contract is `schema/schema.go`; `manifest.yml` is its exported distribution form with plugin metadata. Runtime and manifest versions are `0.1.1`. Generated `zz_*.go` files must never be edited manually.

User setup, supported operations, upstream limits and official source links are documented in [the user guide](docs/box.md). Default strings and documentation are English. `locales/zh-CN.json` translates every source label/description and the full guide using its exact English content as the key.

## Regenerate and verify

```sh
go generate ./...
go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen export yaml .
go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen check .
go test -race ./...
go vet ./...
go build ./...
```

After exporting the schema, retain the manifest plugin org/name/label/description/version/doc metadata and update translations when source strings change. Never add a local replace directive.

## Design and test evidence

All eight contracts were declared and generated first. Compilable stubs returned `not implemented`; `go test -run TestDocumentedWireRequests -count=1 ./...` failed in all eight cases before implementation, then passed after implementation. The repository tests are synthetic HTTP fixtures derived from official API contracts, not captured live service fixtures. They exercise generated binding and inspect actual request methods, paths, headers, bodies and response projection. RoundTripper fixtures avoid external network access and permit explicit redirect/credential assertions.

Transport tests cover 401/403/404/409/429/500, untrusted redirects, request/response limits, invalid JSON, cancellation, timeout and safe error handling. File tests cover empty content, oversized uploads/downloads, platform fetch/store failures and malformed receipts. No live credentials were read and no live writes were made. Images and registry entries have not been published by these checks.

The plugin intentionally uses manual access tokens and delegates refresh to an external authorization service. This avoids credential rotation races and does not invent a platform OAuth provider. File bytes are bounded at 32 MiB; the SDK fetch API materializes platform file bytes before the post-fetch bound can be checked. Deployment gateways should also enforce file limits. Errors never include raw upstream bodies or transport URLs, and writes have no automatic retries.

Folder listing uses marker pagination; search advances offset by the response limit, not entry count. Multipart attributes precede file bytes and regular quoted UTF-8 filenames avoid the filename-star interoperability failure. Download redirects are followed manually with a fresh header set, no cookie jar and no bearer token; HTTPS Box content domains are allowlisted. The size and available SHA-1 are verified before writing a platform file.
