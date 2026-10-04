# kbstore-es — Elasticsearch knowledge-base storage plugin

Connects knowledge-base vector/full-text storage to a self-hosted Elasticsearch. The user-facing
manual is [docs/kbstore-es.md](docs/kbstore-es.md).

## Scope

The knowledge base's **second storage backend**: the platform defaults to built-in storage, and
once this plugin is attached, a given knowledge base can switch to ES instead
(`plugin-builtin/kbstore-*` and `dev-plugins/kbstore-pgvector` are the same kind of thing —
same contract, so switching one for another just means switching a credential; the canvas and
the retrieval pipeline don't need to change).

## One deployment gotcha

When running in a container, the credential's `es_url` should be a **service name** (e.g.
`http://elasticsearch:9200`), not `localhost` — the plugin and ES share the same compose network;
running as a host process, it's the other way around.
This has bitten us before: after switching to a service name, forgetting to switch the host-process
credential back, with the symptom being "the plugin is online but retrieval returns nothing".

## Development

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```
