# Browser Use Cloud

Official Sokel plugin, version 0.1.0. Seven operations built against public `sokel-plugin-sdk v0.7.7`, with English interfaces and a complete Simplified Chinese translation.

Read the [user guide](docs/browser-use.md) for credentials, permissions, API boundaries and operation examples. [中文开发说明](README.zh-CN.md).

## Development

The module is independent. No local replacements or private Go dependencies are required.

```sh
GOWORK=off go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen generate .
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
GOWORK=off go build -o /tmp/sokel-browser-use .
GOWORK=off go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen check .
```

`manifest.yml` declares the contract. Do not edit generated `zz_*.go` files. Runtime metadata and the manifest both use version 0.1.0. Set deployment `ENDPOINT` and `TOKEN` for Sokel connectivity; service credentials belong in the platform credential form. The provider API origin is fixed.

Tests use synthetic HTTP fixtures and never require real provider credentials or perform live writes. They cover exact wire data, pagination, credential boundaries, failure responses, rate limits and generated handlers. Local validation is not live service acceptance. Any temporary first-release catalog fixture is test input only; no publication or real image digest is asserted.
