# Prometheus plugin

Seven typed, read-only operations for a self-hosted Prometheus service: build information, instant/range queries, label names/values, series lookup and active alerts. The required deployment variable `SOKEL_PROMETHEUS_URL` accepts an installation root with an optional reverse-proxy prefix. There is no public-service fallback. Credentials support optional HTTP Basic or a reverse-proxy bearer token; an unprotected private service needs neither. See [the user guide](docs/prometheus.md) and [中文指南](docs/prometheus.zh-CN.md) for exact parameter and deployment boundaries.

The protocol baseline is **Prometheus 3.15.1**, stable `/api/v1`, Apache-2.0 licensed. Primary references are the pinned [API specification](https://github.com/prometheus/prometheus/blob/v3.15.1/docs/querying/api.md), [HTTP API manual](https://prometheus.io/docs/prometheus/latest/querying/api/), [release](https://github.com/prometheus/prometheus/releases/tag/v3.15.1) and [license](https://github.com/prometheus/prometheus/blob/v3.15.1/LICENSE). Queries use documented read-only POST form endpoints, not sample writes. The plugin does not alter rules, scrape targets, reload configuration, manage Alertmanager or provide arbitrary API routes.

Dynamic metric labels, query tuples and native histograms remain JSON text to preserve their structures and exact numeric literals. Sample strings such as `NaN`, `+Inf` and `-Inf` are retained. Warnings and information annotations are separate outputs. Limits can truncate server results and are explicitly documented; the plugin does not invent pagination. Application-level error status is rejected even under HTTP 200. Transport is bounded, redirects and automatic retries are disabled, and raw upstream error bodies/URLs are excluded from user errors.

`schema/` is authoritative and was declared before implementation. Public SDK **v0.7.8** generates the unedited `zz_*.go` files; the independent module has no local replace or private dependency. Runtime and manifest version are **0.1.1**. The tagged container in the manifest is an intended build target, not proof that an image was published. English is the default language, with every UI string and the complete guide translated through `locales/zh-CN.json`. The English guide's entire contents form its translation key. SDK v0.7.8 excludes deployment environment descriptions from locale keys; both guides explain the environment setting.

```sh
GOWORK=off GOCACHE=/tmp/sokel-go-cache go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen@v0.7.8 generate .
GOWORK=off GOCACHE=/tmp/sokel-go-cache go test -race ./...
GOWORK=off GOCACHE=/tmp/sokel-go-cache go vet ./...
GOWORK=off GOCACHE=/tmp/sokel-go-cache go build -o /tmp/sokel-plugin-prometheus .
GOWORK=off GOCACHE=/tmp/sokel-go-cache go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen@v0.7.8 check .
```

`TestDocumentedWireOperations` initially failed behaviorally against compiling generated stubs, then passed with implementation. A separate malformed-result regression first reproduced acceptance of quoted sample timestamps, and the corrected decoder now rejects them. Boundary tests cover generated dispatch, authentication choices, request bytes, tuple/histogram result variants, warnings/infos, input bounds, response limits, unsafe URLs and sanitized errors. See [fixture provenance](testdata/README.md) for the distinction between synthetic wire cases and actual captured responses.

`TestLocalServiceAcceptance` is skipped unless `SOKEL_TEST_PROMETHEUS_URL` points to a caller-created loopback fixture. It expects the synthetic `sokel_wave8_gauge` recording rule to return 7 (optionally overridden via `SOKEL_TEST_PROMETHEUS_QUERY`) and exercises all seven generated operations plus scalar `NaN` and string query results. The isolated Prometheus 3.15.1 run passed; nine actual responses are retained and replayed offline. This establishes local service acceptance, not deployment to a production Sokel platform, image availability or marketplace publication.
