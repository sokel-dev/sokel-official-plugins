# google-sheets

First-party Google connector for Sokel. The contract lives in `schema/`; `manifest.yml` is a real schema export plus distribution metadata for catalog releases. Generated `zz_*.go` files must not be edited. The module depends only on the public Sokel SDK v0.7.8 and uses platform-managed Google OAuth. No platform changes are required.

See [English usage](docs/google-sheets.md) and [Chinese usage](docs/google-sheets.zh-CN.md) for scopes, polling semantics and operation limits. API origins are fixed; redirects and automatic retries are disabled. API error bodies are intentionally omitted to prevent credential leakage.

## Development

```sh
go generate ./...
go test -race ./...
go vet ./...
go build ./...
go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen check .
```

Tests use an in-memory HTTP transport/recorder because the local sandbox does not permit listening sockets. They inspect the actual HTTP request method, path, headers and serialized body passed to `http.Client`. Fixtures are synthetic protocol examples based on the official references below, not live captures. Live authorization/resource/deployment validation remains outstanding.

## Test-first record

Before implementation, `go test ./...` compiled successfully and failed with `not implemented` in the request authentication test and service operation/polling tests. After implementing the HTTP client and operations the same suite passed. `go test -run TestSource ./...` then failed against a compilable checkpoint stub and passed after checkpoint publication was implemented. A blocked socket-listener attempt and a test-fixture compile error were environment/setup failures and were not counted as RED evidence.

Restart-throttle regressions first failed because checkpoints lacked `next_poll_at` and a recreated source polled immediately. After adding the managed deadline, the same tests pass, including cancellation while waiting. SDK event/checkpoint publication is fire-and-forget, so these tests do not establish durable delivery.

Verification: real SDK generation/check, `go test -race ./...`, `go vet ./...` and `go build ./...` pass. Main-package statement coverage is 83.4%; this is local synthetic protocol coverage, not live acceptance.

## Official protocol references

- https://developers.google.com/workspace/sheets/api/reference/rest/v4/spreadsheets.values/append
- https://developers.google.com/workspace/sheets/api/reference/rest/v4/spreadsheets.values/batchUpdate
