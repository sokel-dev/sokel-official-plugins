# Shopify plugin

Source implementation for Sokel using the public Go SDK v0.7.7. Version 0.1.0 is not released to the marketplace.

- [User guide](docs/shopify.md)
- [中文使用说明](docs/shopify.zh-CN.md)

## Development

The schema package is the contract source. Generate wrappers with the pinned SDK before changing handlers; do not edit `zz_*.go`. The source manifest is exported from that schema and supplemented with distribution metadata. English is the default UI/documentation language; `locales/zh-CN.json` contains complete UI and guide translations.

```sh
GOWORK=off go generate ./...
GOWORK=off go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen check .
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
GOWORK=off go build -o /tmp/sokel-plugin-shopify .
```

Run with platform `ENDPOINT` and `TOKEN` or deployment credentials supported by the SDK. Service credentials belong in the platform credential form. No local dependency replacements or private modules are required.

## Validation and boundaries

Behavioral tests were first run against compilable stubs and failed because no upstream request occurred. The implementation then passed protocol and generated-dispatch tests. Fixtures are synthetic examples checked against linked official API documentation, not captured live traffic. No real account credentials, live acceptance, external writes, commits or publication were performed.

Tests assert outgoing methods, URLs, authentication, payloads, pagination, false/null handling, malformed success responses, error redaction, bounded payloads and redirect rejection. HTTP calls have a 55-second timeout, operations declare 60 seconds, and writes are never automatically retried. Read the user guide for provider-specific limits and scopes. Manual live acceptance remains required before release.
