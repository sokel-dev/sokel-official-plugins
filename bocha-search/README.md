# Bocha Search

Search the web with freshness filters and optional summaries.

Version 0.1.1; public Sokel SDK v0.7.8. Default interface and guide language is English; complete Chinese translations are in `locales/zh-CN.json`.

Read the [user guide](docs/bocha-search.md) or [Chinese guide](docs/bocha-search.zh-CN.md) for credentials, operations, limits and protocol references.

## Development

```sh
GOWORK=off go generate ./...
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
GOWORK=off go build -o /tmp/sokel-plugin-bocha-search .
sokel-gen check .
```

Run with `ENDPOINT` and a platform deployment `TOKEN` (or SDK-supported `SOKEL_DEPLOY_KEY`). Configure the service credential in the platform, not in source files.

Tests use synthetic fixtures and do not call live services. Live account acceptance remains pending.
