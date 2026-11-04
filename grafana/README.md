# Grafana plugin

Seven typed operations for self-hosted **Grafana OSS 13.3.0** (AGPLv3): authenticated health, safe data source list/get, dashboard search/get, annotation list/create. Required deployment setting: `SOKEL_GRAFANA_URL`; credentials contain a service-account bearer token. See [the user guide](docs/grafana.md) and [中文指南](docs/grafana.zh-CN.md).

The first version intentionally uses legacy `/api` routes, which Grafana 13 deprecates but keeps operational. It was validated against 13.3.0, not asserted to support every future release. API definitions and handlers are pinned in [fixture provenance](testdata/README.md). Data source wire objects decode only a safe field allowlist: no URL, username/password, jsonData or secure-field metadata is returned. The unpaginated datasource API can be server-capped; the guide makes that limitation explicit. Search uses documented page/limit semantics and may need an extra empty page. Annotation lists have a limit flag rather than an invented cursor.

Dashboard plugin-specific models are preserved as JSON strings using json.RawMessage, avoiding float conversion of large integers. Typed annotations and input IDs/times use exact decimal strings; request encoding uses int64. There is no generic data-source proxy/query input, arbitrary URL fetch, dashboard write, alert runner or account/permission administration.

`schema/` precedes implementation and generates the unedited `zz_*.go` using public SDK v0.7.7. No local replace/private module is used. Runtime/manifest version is 0.1.0; the tagged container is an intended build target, not publication evidence. English defaults and full zh-CN UI/guide translations are included; the entire English guide is the document translation key.

```sh
GOWORK=off GOCACHE=/tmp/sokel-go-cache go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen generate .
GOWORK=off GOCACHE=/tmp/sokel-go-cache go test -race ./...
GOWORK=off GOCACHE=/tmp/sokel-go-cache go vet ./...
GOWORK=off GOCACHE=/tmp/sokel-go-cache go build -o /tmp/sokel-plugin-grafana .
GOWORK=off GOCACHE=/tmp/sokel-go-cache go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen check .
```

`TestREDDataSourceRedaction` and `TestREDCreateAnnotationPrecise` first failed behaviorally on compiling generated stubs and passed after implementation. Offline tests cover captured responses, generated dispatch, exact model/ID preservation, camelCase wire fields, false/zero, paging, bad receipts, redirects, limits and redacted errors.

`TestLocalServiceAcceptance` is skipped unless `SOKEL_TEST_GRAFANA_URL`, `SOKEL_TEST_GRAFANA_TOKEN`, `SOKEL_TEST_GRAFANA_DASHBOARD_UID` and `SOKEL_TEST_GRAFANA_DATASOURCE_UID` configure a caller-created loopback fixture. `SOKEL_TEST_GRAFANA_WRITE=1` enables one annotation creation with read-back; the caller owns cleanup. All seven operations passed this gate with `-race` on isolated OSS 13.3.0 using synthetic resources, not a production account. No platform deployment was performed.
