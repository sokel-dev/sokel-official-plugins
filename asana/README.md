# Asana

Browse projects and manage tasks and comments.

Version 0.1.0; public Sokel SDK v0.7.7. Default interface and guide language is English; complete Chinese translations are in `locales/zh-CN.json`.

Read the [user guide](docs/asana.md) or [Chinese guide](docs/asana.zh-CN.md) for credentials, operations, limits and protocol references.

## Development

```sh
GOWORK=off go generate ./...
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
GOWORK=off go build -o /tmp/sokel-plugin-asana .
sokel-gen check .
```

Run with `ENDPOINT` and a platform deployment `TOKEN` (or SDK-supported `SOKEL_DEPLOY_KEY`). Configure the service credential in the platform, not in source files.

Tests use synthetic fixtures and do not call live services. Initial marketplace release and live account acceptance remain pending; no registry digest is included here.
