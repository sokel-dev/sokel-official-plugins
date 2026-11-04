# Linear plugin

Seven operations cover credential checking, paginated team/issue/project reads and issue creation/update. Personal API keys use the fixed Linear GraphQL endpoint. Explicit GraphQL variables preserve user text and avoid query interpolation.

- [User guide](docs/linear.md)
- [中文使用文档](docs/linear.zh-CN.md)

English is the source language. `locales/zh-CN.json` contains every interface string and the full guide translation. `manifest.yml` combines a real SDK schema export with distribution metadata; `schema/` remains authoritative for operation and credential contracts.

## Develop and validate

This is an independent Go module pinned to the public `github.com/sokel-dev/sokel-plugin-sdk v0.7.7`, with no local replacements or private dependencies. Use Go 1.25 or later. From this directory:

```sh
go generate ./...
go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen check .
go test -race -coverprofile=coverage.out ./...
go vet ./...
go build ./...
go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen export yaml .
```

Refresh the source manifest from the actual export when contracts change, preserving `plugin.org`, `label`, `desc`, `version` and `doc` metadata. Do not hand-edit `zz_*.go`. The SDK consumes `ENDPOINT` plus `TOKEN` or `DEPLOY_KEY` for platform connectivity; service secrets belong in Sokel credentials, not environment variables or source files.

## Validation evidence

A compilable stub first failed the readonly health and operation wire-contract acceptance tests with `not implemented`; the implementation then made the same tests pass. A separate setup type mismatch was corrected before that behavior RED and is not counted as test evidence. Fixtures are synthetic examples derived from the official API documentation, never represented as captured live traffic.

Tests exercise each operation with a real HTTP client and an in-memory transport, inspect outgoing JSON bytes, verify generated input binding, reject redirects, sanitize status/transport errors, enforce payload/response limits, and check cancellation and timeout propagation. Service-specific coverage includes GraphQL HTTP-200 errors, missing connections and nodes, mutation confirmation, zero/empty/omitted updates, cursor continuation and strict date validation.

The final local gates are race tests, vet, build, real generation/check, schema export and temporary first-release catalog validation with the SDK. Temporary release fixtures are validation-only and are not published artifact evidence. No live service credentials were supplied, and no external write or live-account acceptance has occurred. Before production use, validate the intended token scopes and representative reads/writes in a dedicated account.

## Official API references

- [linear.app — graphql](https://linear.app/developers/graphql)
- [linear.app — pagination](https://linear.app/developers/pagination)
- [linear.app — filtering](https://linear.app/developers/filtering)
- [linear.app — rate-limiting](https://linear.app/developers/rate-limiting)
