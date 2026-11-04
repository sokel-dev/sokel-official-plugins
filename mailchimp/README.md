# Mailchimp plugin

Eight typed Marketing API operations using a manually supplied API key. The server prefix is derived from a strictly validated key suffix; no endpoint input or cross-origin redirect can receive the credential. API keys use HTTP Basic authentication. User setup and limits: [English guide](docs/mailchimp.md), [中文指南](docs/mailchimp.zh-CN.md).

The authoritative contract is `schema/`; `manifest.yml` contains the real SDK export with distribution metadata. Generated `zz_*.go` files are never edited manually. Nested `page` and `options` structs use pointers to preserve omitted values, false, and zero. Upsert requires a nonempty `status_if_new` and never sends the existing-member `status` field. No campaigns are sent. Existing upstream automations and pending opt-in confirmation remain provider behavior.

The client uses bounded JSON, exact numeric tag IDs, UseNumber for merge-field output, fixed-origin requests, sanitized errors, no redirect following, and no application retries. Writes clear GetBody to prevent transport body replay. Offset pagination is deliberately explicit and is not snapshot synchronization.

```sh
GOWORK=off go generate .
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
GOWORK=off go build -o /tmp/sokel-plugin-mailchimp .
sokel-gen check .
```

Use the generator from the module-pinned SDK v0.7.7, for example `go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen check .`. Runtime and manifest version are 0.1.0. The module uses only the public SDK and has no local replacements.

Tests began with a generated, compilable implementation stub: authenticated ping, upsert wire, and official tags all failed with `not implemented` before the client was implemented. Tests now verify generated handler binding, actual request bytes, false/zero omission, hashing, exact IDs, pagination, errors, redirects, and no write replay. See [fixture provenance](testdata/README.md). Official examples and synthetic variants are distinct; no live acceptance, contact write or email send is claimed.
