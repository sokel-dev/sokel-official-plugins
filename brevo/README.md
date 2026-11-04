# Brevo plugin

Eight typed API v3 operations for contacts/lists and explicitly requested transactional email. API-key authentication uses the fixed `https://api.brevo.com` origin. There are no automatic sources, send loops, OAuth declarations, or real-account calls in tests. User setup: [English guide](docs/brevo.md), [中文指南](docs/brevo.zh-CN.md).

`schema/` is the authoritative contract. `manifest.yml` is its real SDK export plus distribution metadata; generated `zz_*.go` files must not be edited. Nested page/options structs retain omitted numbers and booleans. Contact creation requires an explicit blocked/allowed enum; updates preserve omitted block flags and reject EMAIL renaming because the provider documents a resubscription side effect. Membership additions are explicit; no forceMerge or updateEnabled is sent.

Sending accepts one plain text or HTML body, a configured sender, and 1–99 To recipients. A typed receipt requires HTTP 201 and messageId; acceptance never means delivery. No send idempotency or automatic retry is claimed. Redirects are rejected; errors omit upstream bodies; account health responses discard embedded account secrets. Writes have no replayable HTTP body. Exact numeric IDs are exposed as strings, and account-defined attribute output uses UseNumber. Deprecated provider subscriber/blacklist counters are excluded.

```sh
GOWORK=off go generate .
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
GOWORK=off go build -o /tmp/sokel-plugin-brevo .
sokel-gen check .
```

Use the module-pinned SDK v0.7.8 generator, for example `go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen check .`. Runtime and manifest version are 0.1.1, with no local replace directives.

A generated, compilable stub first failed behavioral tests for contact creation, the official send receipt, and explicit false updates with `not implemented`. Tests then went green and cover generated dispatch, request bytes, exact IDs, pagination, invalid input/receipts, redirects, sanitized 429/errors, and no write replay. [Fixture provenance](testdata/README.md) separates official documentation examples from synthetic boundaries. No real credential was read, no account was called, and no email was sent. Publication and live delivery acceptance are separate work.
