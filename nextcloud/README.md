# Nextcloud plugin

Six typed Sokel operations for file metadata, immediate directory listing, folder creation, upload, download and moving resources through authenticated WebDAV. No SaaS account is required. The server and any external-storage mounts are operated separately.

See the [English guide](docs/nextcloud.md) and [Chinese guide](docs/nextcloud.zh-CN.md) for path syntax, overwrite policy and limits. Configure the installation root in `SOKEL_NEXTCLOUD_URL`; the credential contains the canonical user ID and app password. Private HTTP and reverse-proxy installation prefixes are supported, with no public fallback. English is the default language. `locales/zh-CN.json` translates the interface and embeds the complete English-to-Chinese guide mapping.

## Protocol and implementation

Official [server 33 WebDAV documentation](https://docs.nextcloud.com/server/33/developer_manual/client_apis/WebDAV/basic.html) was reviewed on 2026-11-04 alongside the current manual. Nextcloud Server uses [AGPL-3.0](https://github.com/nextcloud/server/blob/v33.0.10/COPYING). This connector has protocol-fixture coverage only; no live Nextcloud acceptance or exhaustive version matrix is claimed.

XML parsing checks DAV namespaces, per-property status and resource type. It accepts only the configured origin and authenticated user's file tree, validates requested depth and rejects incomplete or oversized results. Resource hrefs are never used as arbitrary fetch targets. Optional failed properties stay absent. File IDs and ETags stay strings. Listings have no fictitious pagination: 2000 direct children and 8 MiB XML are the hard limits.

Native platform file transfer is limited to 32 MiB. Upload requires an explicit overwrite flag to replace a destination, with optional strong ETag protection. Moves always prohibit overwrite. Downloads check metadata and use an available strong ETag to avoid racing an edit. Root writes, path traversal, percent-escaped paths, redirect following and automatic write retries are excluded. Runtime errors omit credentials and raw upstream bodies. Timeout outcomes for writes remain uncertain until server state is checked.

`schema/schema.go` defines the contract; `zz_*.go` files are generated with pinned public SDK `v0.7.8`. There are no local replacements. `manifest.yml` is a real export with version `0.1.1` and release/deployment metadata. Image references describe future release targets, not published artifacts.

## Verification

Run generation, check and export sequentially within this plugin:

```sh
GOWORK=off GOCACHE=/tmp/sokel-go-cache go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen@v0.7.8 generate .
GOWORK=off GOCACHE=/tmp/sokel-go-cache go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen@v0.7.8 check .
GOWORK=off GOCACHE=/tmp/sokel-go-cache go test -race ./...
GOWORK=off GOCACHE=/tmp/sokel-go-cache go vet ./...
GOWORK=off GOCACHE=/tmp/sokel-go-cache go build -o /tmp/sokel-nextcloud .
```

Tests were introduced before runtime implementation and initially failed compilation because handlers were absent; this is not claimed as compilable behavioral RED. The final suite exercises all six actual generated dispatch handlers, platform file fetch/store, exact WebDAV methods and headers, overwrite/ETag conditions, large string IDs, unsafe paths, foreign hrefs, failed properties and malformed XML. These are synthetic protocol fixtures adapted from official documentation, not recordings from a deployed server. See [fixture provenance](testdata/README.md).

Protocol tests and registry admission fixtures do not establish live deployment or marketplace publication.
