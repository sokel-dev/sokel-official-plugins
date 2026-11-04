# PocketBase plugin

Seven typed operations for self-hosted PocketBase: service health, paginated records, first-match lookup, single-record reads, creation, partial updates and deletion. No SaaS account is required. The scope is normal JSON records; authentication flows, superuser/system collections, collection administration, batch writes, realtime subscriptions and files are excluded.

## Protocol and evidence

Reviewed against the [official documentation](https://pocketbase.io/docs/) advertising **v0.41.1** on 2026-11-04. The official site now links maintenance and releases on `ganigeorgiev.com`; the archived GitHub repository's v0.40.6 release is not treated as current. The [official-site-linked v0.41.1 archive](https://ganigeorgiev.com/meta/pocketbase/pocketbase/download/v0.41.1/pocketbase_0.41.1_darwin_arm64.zip) contains a `LICENSE.md` granting the **MIT license**. License verification read the archive without executing its binary. The inspected archive SHA-256 is `e07ff212b7a7ab53c70d47b11d48ac7d011b4961b6eccbcd231bffe7a54173a0`.

Protocol sources:

- [Records API](https://pocketbase.io/docs/api-records/): list/get/create/update/delete, HTTP 200 for POST/PATCH and HTTP 204 for deletion.
- [Authentication](https://pocketbase.io/docs/authentication/): the raw token is sent in `Authorization`, without a Bearer prefix.
- [API rules and filters](https://pocketbase.io/docs/api-rules-and-filters/): record access remains subject to each collection's rules.
- [Health API](https://pocketbase.io/docs/api-health/): public service availability, not credential verification.

Set required `SOKEL_POCKETBASE_URL` to the self-hosted service root; internal HTTP and reverse-proxy prefixes are supported, with no public fallback. The credential is a preobtained ordinary auth record token. Renewal happens outside the plugin.

Dynamic collection data uses JSON strings so arbitrary schema fields and exact JSON number text survive the workflow boundary. Typed record wrappers expose string IDs and collection metadata. This does not change the upstream database's numeric precision. Explicit false, zero and null are preserved in mutation bodies. Pagination uses explicit totals and returns one page; it makes no snapshot or exactly-once guarantee. `find_record` returns an empty array when no record matches.

The source contract deliberately caps page size at 200, page number at 1000000, request size at 1 MiB, response size at 8 MiB and HTTP duration at 45 seconds. The full guide documents field constraints, permission-dependent empty results and uncertain write outcomes after transport failures.

## Development and validation

The contract lives in `schema/schema.go`. Run real SDK code generation before changing handlers; never edit `zz_*.go`. The independent module pins the public `github.com/sokel-dev/sokel-plugin-sdk v0.7.8` without local replacements. Runtime and manifest versions are both `0.1.1`. The manifest's container reference is the intended release tag, not evidence that an image has been published.

```sh
GOWORK=off GOCACHE=/tmp/sokel-go-cache go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen@v0.7.8 generate .
GOWORK=off GOCACHE=/tmp/sokel-go-cache go test -race ./...
GOWORK=off GOCACHE=/tmp/sokel-go-cache go vet ./...
GOWORK=off GOCACHE=/tmp/sokel-go-cache go build -o /tmp/sokel-pocketbase .
GOWORK=off GOCACHE=/tmp/sokel-go-cache go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen@v0.7.8 check .
```

`TestDocumentedWireRequests` invokes the actual generated dispatch and checks the HTTP method, path prefix, query, exact JSON body, authentication header and disabled mutation-body replay. These tests were first run against compiling unimplemented handlers and failed for every operation, then passed after implementation. Fixtures are synthetic adaptations of official documentation/source, **not captured live responses**. No real account, production service or browser workload was used. Full provenance is in `testdata/README.md`.

Further tests exercise application-level malformed responses, explicit false/zero values, canceled contexts, timeouts, oversized messages, content types, redirect rejection, HTTP errors and credential redaction. The plugin does not retry writes. Runtime errors do not reflect upstream bodies or request URLs. Service endpoints are deployment configuration rather than per-operation input or identity data.

User documentation is in `docs/pocketbase.md` and `docs/pocketbase.zh-CN.md`. `locales/zh-CN.json` translates all exposed UI text and maps the exact complete English guide to the complete Chinese guide. Update both guide files and that exact translation key together. The deployment environment description is English manifest metadata; SDK 0.7.8 does not localize that field, and the bilingual guides explain it fully.
