# Google Docs plugin

Public SDK: `github.com/sokel-dev/sokel-plugin-sdk v0.7.8`. Contract source: `manifest.yml`. Do not edit generated `zz_*.go` files. No local SDK replacements are used. Default labels, documentation and errors are English; `locales/zh-CN.json` contains Chinese UI and full-guide translations.

See [user guide](docs/google-docs.md) and [Chinese guide](docs/google-docs.zh-CN.md) for setup, operation semantics, permissions and limitations. API references in the guide were reviewed on 2026-11-03. HTTP tests assert wire method, path, headers and payloads using documented response shapes; fixtures are synthetic, not captures from a live account. Live acceptance remains pending.

## Development

```sh
GOWORK=off go generate ./...
GOWORK=off go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen check .
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
GOWORK=off go build ./...
```

The HTTP client has bounded responses, context cancellation and timeouts, no redirects, no write retries, and sanitized errors. Test transports are injected without changing production API origins. Generated operation registration is exercised separately from direct handler tests so contract binding is covered.
