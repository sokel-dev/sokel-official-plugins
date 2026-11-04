# Paperless-ngx plugin

Nine typed operations for a self-hosted document archive: authenticated health check, document search/get/upload/download/metadata update, ingestion-task lookup and tag list/create. `SOKEL_PAPERLESS_URL` is a required site-root deployment setting with internal HTTP and reverse-proxy prefixes supported. Credentials contain the service API token. There is no public fallback. See [the user guide](docs/paperless-ngx.md) and [中文指南](docs/paperless-ngx.zh-CN.md).

The baseline is the stable **Paperless-ngx 3.4.1** release (GPLv3), explicitly requesting **API 10**. This is based on the pinned release's [API guide](https://github.com/paperless-ngx/paperless-ngx/blob/v3.4.1/docs/api.md), [views](https://github.com/paperless-ngx/paperless-ngx/blob/v3.4.1/src/documents/views.py), [serializers](https://github.com/paperless-ngx/paperless-ngx/blob/v3.4.1/src/documents/serialisers.py) and [task model](https://github.com/paperless-ngx/paperless-ngx/blob/v3.4.1/src/documents/models.py), not assumptions about the development branch. API 10 tasks are paginated, use lowercase statuses and expose `related_document_ids`; API 9 task lists are incompatible and rejected.

Upload is multipart with repeated `tags`, a regular quoted UTF-8 filename and a 200 JSON-string UUID receipt. It queues consumption rather than returning a completed document. Files use SDK platform fetch/upload, 32 MiB bounds and no filesystem or arbitrary-URL reads. Downloads require an attachment response and never follow redirects. All numeric object IDs are parsed exactly as int64 and output as decimal strings. Metadata PATCH preserves omission and supports explicit empty-tag replacement; null relationship clearing is intentionally unsupported. Requests do not retry failed HTTP responses, replay mutation bodies, expose service error bodies or forward authentication to redirects.

`schema/` is authoritative; public SDK v0.7.7 generates the unedited `zz_*.go` files. This module has no local replace directives. Runtime and manifest version 0.1.0 agree. The container tag is an intended build target, not a publication claim. Both complete guides and every UI source string are translated through `locales/zh-CN.json`. The entire English guide is the document translation key. SDK v0.7.7 does not expose deployment environment descriptions as locale keys; both guides cover that configuration.

```sh
GOWORK=off GOCACHE=/tmp/sokel-go-cache go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen generate .
GOWORK=off GOCACHE=/tmp/sokel-go-cache go test -race ./...
GOWORK=off GOCACHE=/tmp/sokel-go-cache go vet ./...
GOWORK=off GOCACHE=/tmp/sokel-go-cache go build -o /tmp/sokel-plugin-paperless-ngx .
GOWORK=off GOCACHE=/tmp/sokel-go-cache go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen check .
```

`TestREDUploadAsyncUnicode` and `TestREDTaskAPI10` first failed behaviorally against compiling generated stubs. They passed after implementation. Synthetic wire and generated-dispatch tests cover large IDs, optional false, empty arrays, Unicode multipart headers, task lifecycle and missing tasks, paging, malformed receipts, bounded downloads, platform storage failures, authentication and HTTP errors. See [test provenance](testdata/README.md). **No live Paperless instance was run**; source-grounded protocol tests are not live acceptance. No deployment was performed.
