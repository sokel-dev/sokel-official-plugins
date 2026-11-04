# Prometheus fixture provenance

`local-service-captures.json` contains **nine actual HTTP exchanges** captured on 2026-11-04 from isolated Prometheus **3.15.1** with synthetic recording and alert rules. The capture preserves method, API path, form request body when present, HTTP status and JSON response. It contains no credential headers. The source capture was `/tmp/sokel-wave8-live/prometheus-generated-captures.json`; the acceptance log was `/tmp/sokel-wave8-live/prometheus-go-acceptance.log`. These temporary paths are development evidence, not runtime dependencies.

`TestLocalServiceAcceptance` invoked the actual generated handlers for build information, instant query, range query, label names, label values, series lookup and active alerts. Two additional instant queries exercised scalar `NaN` (`0/0`) and a string result. `TestCapturedServiceResponses` reconstructs the operation inputs from each captured request and replays all nine real responses through generated dispatch and the actual decoder. Native-histogram boundary cases are tested synthetically and are not claimed as live captures.

Inline bodies in `wire_test.go` and `boundary_test.go` are **synthetic protocol/boundary variants**, grounded in the official API contract. They are not presented as actual server responses or verbatim official JSON examples. They include warnings/infos, non-finite sample strings, tuple/histogram variants, malformed shapes, quoted sample timestamps, authentication combinations, response limits and upstream application errors under HTTP 200. Initial wire tests failed on compiling generated stubs before implementation; the quoted-timestamp case also reproduced a behavioral failure before its fix.

Primary protocol references, inspected against the stable tag:

- [Prometheus v3.15.1 API specification](https://github.com/prometheus/prometheus/blob/v3.15.1/docs/querying/api.md): `/api/v1/query` and `/query_range` POST form requests; query timeouts/limits; vector, matrix, scalar, string and native histogram result formats; warnings/infos; metadata labels/series and active-alert endpoints.
- [HTTP API manual](https://prometheus.io/docs/prometheus/latest/querying/api/): success/error envelope, repeated `match[]` parameters, metadata time-filter caveats and service-side result limits.
- [Release](https://github.com/prometheus/prometheus/releases/tag/v3.15.1) and [Apache-2.0 license](https://github.com/prometheus/prometheus/blob/v3.15.1/LICENSE).

No production monitoring service, cloud account, marketplace publication or built-image digest is implied by these fixtures.
