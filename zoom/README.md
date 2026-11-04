# Zoom plugin

Independent Go module exposing 7 typed operations. Version 0.1.0 has not been tested against a live account.

- [English usage guide](docs/zoom.md)
- [中文使用指南](docs/zoom.zh-CN.md)
- [Fixture provenance](testdata/README.md)

## Development

`schema/schema.go` is the operation and credential contract. Declare changes there before implementing them; regenerate with the public SDK pinned at v0.7.7. Never edit `zz_*.go`. `manifest.yml` is a real schema export with distribution metadata added; keep its contract aligned with the schema. `plugin.version`, manifest and runtime version are 0.1.0. There are no local module replacements or private dependencies.

```sh
GOWORK=off go generate ./...
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
GOWORK=off go build -o /tmp/sokel-plugin-zoom .
GOWORK=off go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen check .
```

Use `ENDPOINT` and the platform `TOKEN` or `DEPLOY_KEY` to register the process. Upstream access tokens belong in the platform credential form, not environment variables or this repository. All HTTP origins are fixed in code. HTTP redirects and automatic retries are disabled; no user-supplied URL is fetched. Requests and responses are bounded, HTTP timeout is 50 seconds, and operation timeout is 60 seconds. Error messages omit response bodies, request URLs and secret values.

The runtime sends the English guide with `SetDoc`. SDK v0.7.7 does not upload locale dictionaries during standalone registration. Registry packaging includes `locales/zh-CN.json`; the full English guide is an exact key whose value is the entire Chinese guide. Keep both guides and that key synchronized.

## Verification evidence

The first behavior tests compiled against generated contracts and stub handlers, then failed with `not implemented` before implementation. Tests subsequently pass with implemented handlers. Tests capture method, fixed origin/path, query, authorization headers and JSON bytes at the HTTP transport boundary; exercise real generated dispatch; cover explicit false/zero values, pagination or documented absence of pagination, missing response fields, rate limits, size limits and credential-safe failures. Synthetic fixtures are adaptations of official documentation, not live recordings. See the usage guide for service-specific limitations and scopes. Live acceptance remains outstanding.
