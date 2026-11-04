# Tianyancha

Sokel connector with 2 operations. See the [user guide](docs/tianyancha.md) for credentials,
operation inputs, pagination, limits and official API references. A [Chinese guide](docs/tianyancha.zh-CN.md)
and complete `zh-CN` interface translations are included; English is the default language.

The module uses the public Sokel Plugin SDK v0.7.8 with no local replacements.
Contracts are schema-first in `schema/`; generated bindings are checked in.

```sh
GOWORK=off go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen@v0.7.8 check .
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
GOWORK=off go build -o /tmp/sokel-tianyancha .
```

Tests use synthetic protocol fixtures and exercise generated operation dispatch, request
parameters, pagination, business errors, credential redaction and bounded responses.
Live account acceptance and publication are separate; no paid service call is implied by these tests.
There is no automatic health check. Read the guide before running any quota-consuming lookup.
