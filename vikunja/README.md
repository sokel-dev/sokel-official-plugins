# Vikunja plugin

Independent Sokel module with nine typed operations for a self-hosted Vikunja server: health, project discovery/read/create, task listing/read/create/partial update, and comments. Read the [English guide](docs/vikunja.md) or [Chinese guide](docs/vikunja.zh-CN.md). No cloud account or paid feature is required.

## Contract and compatibility

`schema/schema.go` is authoritative, with actual code generation from public SDK `v0.7.7`. Never hand-edit `zz_*.go`. `manifest.yml` is the schema export plus release/deployment metadata. Runtime, manifest and `plugin.version` are `0.1.0`; there are no local replacements or private module dependencies. The container reference is an intended version tag, not evidence of image publication.

Set required deployment variable `SOKEL_VIKUNJA_URL` to the server root before `/api/v2`. Internal HTTP, container service names and reverse-proxy prefixes are supported without a public fallback. Container `localhost` points to that container. Credentials contain only the bearer token; scoped API tokens are recommended and rotation is external.

Official sources checked on 2026-11-04: [Vikunja 2.7.1](https://github.com/go-vikunja/vikunja/releases/tag/v2.7.1), its [AGPL-3.0 license](https://github.com/go-vikunja/vikunja/blob/v2.7.1/LICENSE), [API v2 documentation](https://vikunja.io/docs/api-v2/), [tagged project routes](https://github.com/go-vikunja/vikunja/blob/v2.7.1/pkg/routes/api/v2/projects.go) and [project models](https://github.com/go-vikunja/vikunja/blob/v2.7.1/pkg/models/project.go). API v2 began in 2.4.0; this release uses the released 2.7.1 instance specification, not the public demo's newer development schema. It has no API v1 fallback. Other versions have not been exhaustively tested.

## Protocol decisions

Creates use POST/201. Updates use PATCH with `application/merge-patch+json`, preserving omitted fields and explicit false/zero/empty descriptions. Descriptions and comments use native HTML. Task IDs are exact positive decimal strings; negative project IDs represent Favorites or saved filters and are allowed for project/task reads, but not task creation. Actual tasks always expose their real positive project ID.

Real 2.7.1 acceptance found that project lists append virtual projects after ordinary pagination while `total` excludes them. A recorded failure became a regression before the parser was corrected. Ordinary rows are checked against the effective page size; extra virtual entries are preserved and continuation follows the body metadata. Virtual entries may repeat across pages, so callers deduplicate by ID. Task pages do not relax normal size checks. Health uses authenticated project listing rather than public liveness.

The transport uses only the deployment origin, forbids credential-bearing redirects, removes mutation-body replay, bounds timeout/body sizes and redacts raw error bodies, URLs and secrets. Failed HTTP responses are not automatically retried. Writes with uncertain timeout outcomes require inspection before another attempt. This release does not delete tasks/projects, modify labels/assignees/recurrence or expose arbitrary API requests.

## Development and verification

Run from this directory; SDK generation, export and check must be sequential within a plugin.

```sh
GOWORK=off GOCACHE=/tmp/sokel-go-cache go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen generate .
GOWORK=off GOCACHE=/tmp/sokel-go-cache go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen check .
GOWORK=off GOCACHE=/tmp/sokel-go-cache go test -race ./...
GOWORK=off GOCACHE=/tmp/sokel-go-cache go vet ./...
GOWORK=off GOCACHE=/tmp/sokel-go-cache go build -o /tmp/sokel-vikunja .
```

All nine operations first failed `go test -run '^TestDocumentedWireRequests$' -count=1 ./...` with compilable `not implemented` handlers after contract generation. They passed after implementation. Synthetic wire fixtures test actual generated handler dispatch and exact requests, supplemented by pagination, response validation, false/zero, ID precision, timeouts, size limits, redirects and credential-redaction checks.

`TestLocalServiceAcceptance` also passed with `-race` against a disposable Vikunja 2.7.1 SQLite container, a fresh local user and a scoped test-only API token. It exercised all nine operations, task pagination, partial updates and false/zero/date clearing, then removed its temporary project. Actual captures are in `testdata/local-service-captures.json`, replayed offline by `TestCapturedServiceResponses`; `recorded-project-page.json` preserves the virtual-project case. See [fixture provenance](testdata/README.md). Synthetic fixtures are clearly separate from those recordings. No production account/data, platform-debug invocation, image publication or registry release is claimed.

To repeat the live test, provision a fresh disposable loopback instance and scoped token, then set `SOKEL_LIVE_VIKUNJA_URL=http://127.0.0.1:13456`, `SOKEL_LIVE_VIKUNJA_TOKEN` and optionally `SOKEL_LIVE_FIXTURE_DIR` to an existing directory under `/tmp` before running:

```sh
GOWORK=off GOCACHE=/tmp/sokel-go-cache go test -race -run '^TestLocalServiceAcceptance$' -count=1 -v .
```

It is skipped without the explicit URL and rejects non-loopback service URLs. It writes test data, including comments, and deletes its test project; never target an existing dataset. Recordings omit authentication headers. English is the default interface and guide; `locales/zh-CN.json` includes supported interface translations and the exact whole English guide mapped to its complete Chinese translation.
