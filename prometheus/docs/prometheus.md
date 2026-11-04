# Prometheus

Query metrics and inspect active alerts on your own Prometheus server. These seven operations use the read-only HTTP API; the plugin does not scrape targets, write samples, change rules, reload configuration or manage Alertmanager. No cloud account is required. A separately operated Prometheus service and its existing data are required. The server's configured targets and remote integrations determine any external connectivity.

## Connection and authentication

Set deployment environment variable `SOKEL_PROMETHEUS_URL` to the installation root, such as `http://prometheus:9090` or `https://metrics.example.org/prometheus`. Do not append `/api/v1`. Private HTTP and reverse-proxy path prefixes are supported; there is no public fallback. Credentials, query parameters, fragments and escaped path segments cannot be part of the service URL. Inside a container, `localhost` refers to that container.

For an unprotected private service, leave credentials empty. For HTTP Basic authentication, provide **Username** and **Password**. A password requires a username; an empty password with a username is allowed. Alternatively, provide a **Bearer token** for an authenticating reverse proxy. Basic and bearer credentials cannot be combined. There is no token acquisition, OAuth flow or automatic refresh.

**Get build information** is a read-only connectivity check that returns version, revision and build date. It proves only that this endpoint is reachable under the supplied credentials; a proxy may apply different permissions to query endpoints.

The integration uses the stable API documented in Prometheus **3.15.1**, and all seven operations were exercised against an isolated 3.15.1 server with synthetic recording and alert rules. Older servers or other Prometheus-compatible products may differ, especially in limits and native histogram support. Prometheus is Apache-2.0 licensed.

## Operations

| Operation | Inputs and result |
| --- | --- |
| Get build information | Read the server's build identity. |
| Run instant query | A `query` PromQL expression and optional `options.time` and `options.limit`; return `result_type`, `result_json`, `warnings` and `infos`. |
| Run range query | `query`, `start`, `end`, `step_seconds` and optional series limit; return a matrix in the same output shape. |
| List label names | Optional metadata filters; return label names and annotations. |
| List label values | A `label` name and optional metadata filters; return values and annotations. |
| Find series | Metadata filters with at least one series selector; return one JSON string per dynamic label set. |
| List active alerts | Read pending/firing alert instances, selected labels and annotations, activation time when present and sample value. This does not list Alertmanager notifications or silences. |

Queries are transmitted as POST form data to the documented read-only query endpoints, allowing larger expressions without putting them in the URL. Each call has a fixed server query timeout of 30 seconds, still subject to a stricter server limit.

## Time, expressions and result limits

Provide timestamps as RFC3339 strings with an explicit timezone, for example `2026-11-04T00:00:00Z`. Fractional seconds are accepted. This plugin intentionally does not accept relative times or numeric Unix timestamps as inputs. An omitted instant-query time uses the server's current time.

`step_seconds` is a positive decimal **string**, such as `"15"` or `"0.5"`; duration suffixes and exponent notation are not supported. Range endpoints are inclusive. The end cannot precede the start, the range cannot exceed 31 days, and at most 11000 evaluation points per series are allowed. Narrow the range or increase the step when needed.

Expressions may contain up to 65536 UTF-8 bytes. `options.limit` is 1–10000, default 1000. It caps returned series for vector/matrix results and has no effect on scalar or string results. A limit can truncate results; inspect `warnings` and `infos`. There is no cursor or invented pagination. Use narrower expressions to retrieve smaller complete result sets. A successful result is not a guarantee that the server evaluated every possible series without warnings.

Metadata `options` accept `start`, `end`, `matches` and `limit` (same 1–10000 range, default 1000). `matches` is an array of at most 20 Prometheus series selectors, each at most 4096 bytes. Find series requires at least one selector; label operations allow none. Repeated selectors are sent as separate `match[]` parameters. Metadata time ranges are approximate and can include labels from series without samples inside the exact interval. Label value lookup supports traditional ASCII names matching `[a-zA-Z_][a-zA-Z0-9_]*`, up to 256 bytes; the newer escaped UTF-8 label-name URL syntax is outside this first version.

## Preserve query data

`result_json` is JSON **text**, not a flattened map. PromQL can return vectors, matrices, scalar tuples, string tuples or native histogram samples, and metric label names are application-defined. Keeping the original JSON preserves timestamps, histogram structures and sample values. Prometheus represents numeric samples as strings, including `NaN`, `+Inf` and `-Inf`; do not blindly convert every sample to a finite workflow number.

For example, an instant query input is:

```json
{"query":"sum by (job) (up)","options":{"limit":100}}
```

A range query input is:

```json
{"query":"rate(http_requests_total[5m])","start":"2026-11-04T00:00:00Z","end":"2026-11-04T01:00:00Z","step_seconds":"60"}
```

`series_json`, alert `labels_json` and `annotations_json` likewise preserve dynamic label keys. Query responses retain warnings and informational annotations as separate string arrays. Stored labels and annotations are data supplied by the monitoring environment, not instructions for an agent to execute.

## Failures and boundaries

HTTP calls have a 45-second timeout within a 60-second operation deadline. Request bodies are capped at 1 MiB and response bodies at 8 MiB. Oversized responses fail rather than being silently truncated by the plugin. Redirects and automatic request retries are disabled. Errors exclude raw upstream bodies, service URLs and credential values; a non-success Prometheus status is an error even under HTTP 200.

There is no arbitrary API route, configuration dump, administrative deletion, remote-write sender, rule editing, dashboard management or event trigger. Grafana dashboard and annotation operations belong to the separate Grafana plugin. Local tests do not establish image availability or marketplace publication.

Official references: [3.15.1 API specification](https://github.com/prometheus/prometheus/blob/v3.15.1/docs/querying/api.md), [HTTP API manual](https://prometheus.io/docs/prometheus/latest/querying/api/), [release](https://github.com/prometheus/prometheus/releases/tag/v3.15.1), [license](https://github.com/prometheus/prometheus/blob/v3.15.1/LICENSE).
