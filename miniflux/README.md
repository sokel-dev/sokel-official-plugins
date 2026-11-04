# Miniflux plugin

Nine typed Sokel operations for self-hosted subscriptions, stored articles and reading state. No SaaS account is required. Miniflux uses PostgreSQL and fetches public feeds over the Internet; reading local stored entries does not require a cloud service. The plugin uses API keys and excludes administrative and third-party-delivery endpoints.

See the [English guide](docs/miniflux.md) and [Chinese guide](docs/miniflux.zh-CN.md) for configuration, cursor semantics, limits and official references. Set `SOKEL_MINIFLUX_URL` to your instance root. Default interface text and documentation are English; `locales/zh-CN.json` contains all translated interface strings and the entire English guide as an exact translation key.

## Implementation

The contract targets API behavior available in Miniflux 2.3.2+, with actual isolated acceptance on [2.3.5](https://github.com/miniflux/v2/releases/tag/2.3.5). The server is [Apache-2.0 licensed](https://github.com/miniflux/v2/blob/2.3.5/LICENSE). Official API documentation was checked on 2026-11-04. Stored feed authentication, account settings and passwords are not exposed through response objects. Signed 64-bit IDs are strings at the workflow boundary, preserving values above 2^53.

Entry pagination uses ascending ID and exclusive `after_entry_id`, not offset. A full page reports only that more entries may exist. Explicit `starred=false` is preserved both in filters and updates. Single-feed refresh is synchronous HTTP 204, while the receipt does not assert that fresh entries were found. Requests use the configured origin, support reverse-proxy prefixes, reject redirects, avoid body replay for writes, enforce size limits and suppress raw upstream error bodies. Writes are never automatically retried.

`schema/schema.go` is the source for real SDK-generated `zz_*.go` files. SDK `v0.7.7` is pinned without local replacements. `manifest.yml` is an actual export with version `0.1.0` and deployment metadata. Its image reference is an intended release target, not proof of publication.

## Verification

Run generation, check and export sequentially within this plugin:

```sh
GOWORK=off GOCACHE=/tmp/sokel-go-cache go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen generate .
GOWORK=off GOCACHE=/tmp/sokel-go-cache go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen check .
GOWORK=off GOCACHE=/tmp/sokel-go-cache go test -race ./...
GOWORK=off GOCACHE=/tmp/sokel-go-cache go vet ./...
GOWORK=off GOCACHE=/tmp/sokel-go-cache go build -o /tmp/sokel-miniflux .
```

Behavior tests were introduced before runtime implementation and failed to compile until handlers existed; these were not compilable behavioral RED tests. The finished suite exercises all nine generated dispatch paths plus exact IDs, cursor filters, false values, mutation bodies and error sanitization. Fixture examples are synthetic adaptations of the official API. Sanitized actual service captures are identified separately under `testdata/`.

`TestLocalServiceAcceptance` passed with `-race` against a temporary, loopback-only Miniflux 2.3.5 instance, independent PostgreSQL and a local three-entry RSS feed. It creates a category and subscription, refreshes it, reads two cursor pages and updates/reads status and bookmarks. It is skipped unless all three variables below are provided:

```sh
SOKEL_TEST_MINIFLUX_URL=http://127.0.0.1:18087 \
SOKEL_TEST_MINIFLUX_TOKEN=your-disposable-key \
SOKEL_TEST_MINIFLUX_FEED_URL=http://rss:8000/feed.xml \
GOWORK=off GOCACHE=/tmp/sokel-go-cache go test -race -run '^TestLocalServiceAcceptance$' -count=1 -v .
```

This writes synthetic data and must target a disposable test stack, never existing user data. Stack removal is the cleanup mechanism. Local acceptance does not establish a production deployment or published image.
