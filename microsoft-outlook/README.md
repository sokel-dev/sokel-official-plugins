# Microsoft Outlook plugin

8 typed Microsoft Graph v1.0 operations with a manual delegated access-token credential.

Mail list/get/send, attachment metadata and binary download, calendar occurrences and event creation. Mail uses immutable IDs. Send-mail success requires HTTP 202 and only confirms acceptance; calendar timestamps require explicit RFC3339 offsets and are converted to UTC. File/item/reference attachments are distinguished, and only file attachments are downloaded into Sokel. No outgoing attachment support, recurrence, updates, deletes or triggers.

- [User guide](docs/microsoft-outlook.md)
- [中文使用文档](docs/microsoft-outlook.zh-CN.md)

## Architecture

`manifest.yml` is the authoritative declaration, including metadata and version. The public SDK v0.7.8 generates `zz_*.go`; never hand-edit generated files. All operations are registered through generated handlers and `contract.BindInput`. English is the default language. `locales/zh-CN.json` translates every interface string and the complete user guide.

Every invocation performs a readonly `/me?$select=id` check with `User.Read` before using the delegated user context. This prevents app-only writes and obtains the real user ID for canonical user-scoped list paths. The additional identity call is intentional. Resource IDs are escaped as path segments and cannot inject another path.

The global Graph origin is fixed. Pagination preserves the complete opaque nextLink query and requires the same exact resource path, HTTPS origin and v1.0 API version. No redirects or automatic retries are followed. Errors do not echo Graph response bodies, token values or URLs. There is no OAuth provider registration or token refresh; the application/tenant owner obtains and refreshes delegated tokens externally.

Requests are capped at 4 MiB, JSON/binary responses at 16 MiB, operation timeout at 60 seconds and per-request HTTP timeout at 55 seconds. Tenant policies and Graph throttling still apply. Input and output limitations, consent requirements and side effects are documented in the user guide.

## Development and validation

This independent Go module uses only the public SDK with no local `replace` or private dependency. From this directory, using Go 1.25 or later:

```sh
go generate ./...
go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen@v0.7.8 check .
go test -race -coverprofile=coverage.out ./...
go vet ./...
go build ./...
go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen@v0.7.8 export yaml .
```

The platform connection uses `ENDPOINT` with `TOKEN` or `DEPLOY_KEY`. Service credentials belong in Sokel credentials. No tenant endpoint is accepted from workflow inputs.

The first compilable stub failed `TestReadOnlyHealth` and `TestOutlookProtocol` with `not implemented`; the same acceptance tests then passed after implementation. Tests use a real HTTP client with an in-memory recording transport, inspect outbound request bytes, and exercise every generated dispatcher. Additional cases cover origin/path isolation, immutable nextLink queries, malformed collections, no-progress pages, redirects, token/status/transport sanitization, payload limits, deadline propagation and cancellation. Attachment binary bytes, filename handling, unsupported attachment types, storage errors, recipient validation, event timezone conversion and HTTP 202 semantics are covered.

Fixtures are synthetic examples based on the official API documentation, not captures from a real account. No live-account acceptance and no real mail, invitation or Teams message sending has occurred. Before production use, verify the intended delegated permissions and representative operations in a dedicated test tenant. The release owner validates the real export and locale metadata in a temporary catalog; no temporary fixture establishes publication.

The user guide links the official endpoint and permission references used for implementation.
