# Confluence Cloud plugin

Seven operations cover credential checking, paginated space/page reads and published-page creation/update. Data operations use v2; identity checking uses the maintained v1 current-user endpoint. Updates compare the expected published version, then write exactly the next version without automatic retries.

- [User guide](docs/confluence.md)
- [中文使用文档](docs/confluence.zh-CN.md)

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

Tests exercise each operation with a real HTTP client and an in-memory transport, inspect outgoing JSON bytes, verify generated input binding, reject redirects, sanitize status/transport errors, enforce payload/response limits, and check cancellation and timeout propagation. Service-specific coverage includes v2 request bodies, storage/ADF format validation, numeric IDs, encoded cursors, cross-origin pagination rejection, stale-version prevention and an API 409 after a successful pre-read. Published-page version checks do not protect existing drafts from Atlassian reconciliation.

The final local gates are race tests, vet, build, real generation/check, schema export and temporary first-release catalog validation with the SDK. Temporary release fixtures are validation-only and are not published artifact evidence. No live service credentials were supplied, and no external write or live-account acceptance has occurred. Before production use, validate the intended token scopes and representative reads/writes in a dedicated account.

## Official API references

- [developer.atlassian.com — api-group-page](https://developer.atlassian.com/cloud/confluence/rest/v2/api-group-page/)
- [developer.atlassian.com — api-group-space](https://developer.atlassian.com/cloud/confluence/rest/v2/api-group-space/)
- [developer.atlassian.com — api-group-users](https://developer.atlassian.com/cloud/confluence/rest/v1/api-group-users/)
- [developer.atlassian.com — basic-auth-for-rest-apis](https://developer.atlassian.com/cloud/confluence/basic-auth-for-rest-apis/)
- [support.atlassian.com — manage-api-tokens-for-your-atlassian-account](https://support.atlassian.com/atlassian-account/docs/manage-api-tokens-for-your-atlassian-account/)
