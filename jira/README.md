# Jira Cloud plugin

Nine operations cover credential checking, enhanced JQL search, issue get/create/update, transitions and comments. Cloud API v3 only; Server/Data Center are excluded. API-token credentials support both the tenant origin and the required scoped-token gateway.

- [User guide](docs/jira.md)
- [中文使用文档](docs/jira.zh-CN.md)

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

Tests exercise each operation with a real HTTP client and an in-memory transport, inspect outgoing JSON bytes, verify generated input binding, reject redirects, sanitize status/transport errors, enforce payload/response limits, and check cancellation and timeout propagation. Service-specific coverage includes POST /search/jql pagination, Basic-auth origin boundaries, scoped-token routing, ADF paragraph conversion, empty/omitted updates, dynamic custom-field constraints and transition request bytes. Dynamic custom/transition objects use SDK float64 binding; large integer identifiers must be strings.

The final local gates are race tests, vet, build, real generation/check, schema export and temporary first-release catalog validation with the SDK. Temporary release fixtures are validation-only and are not published artifact evidence. No live service credentials were supplied, and no external write or live-account acceptance has occurred. Before production use, validate the intended token scopes and representative reads/writes in a dedicated account.

## Official API references

- [developer.atlassian.com — api-group-issue-search](https://developer.atlassian.com/cloud/jira/platform/rest/v3/api-group-issue-search/)
- [developer.atlassian.com — api-group-issues](https://developer.atlassian.com/cloud/jira/platform/rest/v3/api-group-issues/)
- [developer.atlassian.com — api-group-issue-comments](https://developer.atlassian.com/cloud/jira/platform/rest/v3/api-group-issue-comments/)
- [developer.atlassian.com — basic-auth-for-rest-apis](https://developer.atlassian.com/cloud/jira/platform/basic-auth-for-rest-apis/)
- [support.atlassian.com — manage-api-tokens-for-your-atlassian-account](https://support.atlassian.com/atlassian-account/docs/manage-api-tokens-for-your-atlassian-account/)
