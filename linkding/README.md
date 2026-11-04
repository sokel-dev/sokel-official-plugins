# linkding plugin

Nine typed operations for self-hosted bookmark search, save/update/delete, archive state and tag discovery. No SaaS account is required. Configure `SOKEL_LINKDING_URL` and a per-user API token. See the [English guide](docs/linkding.md) and [Chinese guide](docs/linkding.zh-CN.md) for deployment, inputs and limits. English is the default; `locales/zh-CN.json` translates the interface and maps the complete English guide to its Chinese translation.

## Implementation

The implementation targets [linkding 1.47.0](https://github.com/sissbruecker/linkding/releases/tag/v1.47.0), under the [MIT license](https://github.com/sissbruecker/linkding/blob/v1.47.0/LICENSE.txt). Official API documentation, routes, serializers and bookmark services were checked on 2026-11-04. All nine operations passed against a disposable local 1.47.0 service.

Saving an existing URL follows upstream create-or-update behavior and can replace omitted fields with creation defaults. Save does not invent a newly-created flag from HTTP 201; PATCH preserves omitted fields. False, empty strings and empty tag arrays survive generated binding. Auto-tagging can add tags back. Archive/unarchive/delete require documented HTTP 204 receipts.

Save disables synchronous metadata scraping and automatic HTML snapshots. Server favicon, preview and Wayback integrations have separate settings; self-hosting alone does not imply offline operation. The connector avoids the scraping `/check` endpoint and never fetches bookmark URLs. Outputs omit asset and fallback-archive URLs.

IDs and counts are strings. Pagination validates origin, route, filters, continuation and counts. It is not a snapshot: modifying the matching set can shift offsets. Requests preserve proxy prefixes, disable redirects and mutation replay, cap sizes/time and suppress raw service errors. Timed-out writes require checking server state before retrying.

## Development and verification

`schema/schema.go` is authoritative; `zz_*.go` comes from pinned public SDK `v0.7.7`, with no local replacements. The manifest is an actual export plus deployment/release metadata, version `0.1.0`. Its tagged image is an intended release target, not proof of publication. Run SDK commands sequentially within the plugin:

```sh
GOWORK=off GOCACHE=/tmp/sokel-go-cache go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen generate .
GOWORK=off GOCACHE=/tmp/sokel-go-cache go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen check .
GOWORK=off GOCACHE=/tmp/sokel-go-cache go test -race ./...
GOWORK=off GOCACHE=/tmp/sokel-go-cache go vet ./...
GOWORK=off GOCACHE=/tmp/sokel-go-cache go build -o /tmp/sokel-linkding .
```

All nine generated-dispatch cases first failed against compilable `not implemented` handlers, then passed after implementation. Tests inspect request methods/paths/headers/bodies, optional values, exact IDs, continuation, malformed input, response limits and redacted failures. Actual local responses have separate replay coverage; see [fixture provenance](testdata/README.md).

`TestLocalServiceAcceptance` passed with `-race`: same-URL updates, two-page reads, false/empty tags, archive/unarchive and explicit deletion. It is skipped unless `SOKEL_TEST_LINKDING_URL` is a loopback test URL, `SOKEL_TEST_LINKDING_TOKEN` is a disposable token, `SOKEL_TEST_LINKDING_BOOKMARK_URL` is a synthetic local URL and `SOKEL_TEST_LINKDING_WRITE=1`. Provision an isolated instance with external background features disabled, then run:

```sh
GOWORK=off GOCACHE=/tmp/sokel-go-cache go test -race -run '^TestLocalServiceAcceptance$' -count=1 -v .
```

The test creates and deletes two bookmarks; synthetic tags can remain until the temporary instance is removed. Never target existing user data. Optional `SOKEL_TEST_CAPTURE_DIR` under `/tmp` records paths/bodies and responses without authentication headers. Acceptance services and temporary data were removed afterward. No commit, production deployment or publication is implied.
