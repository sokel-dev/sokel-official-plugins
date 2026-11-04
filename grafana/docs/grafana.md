# Grafana

Inspect dashboards, discover safe data source summaries and record annotations on a self-hosted Grafana instance. The baseline is **Grafana OSS 13.3.0**, licensed under AGPLv3. Grafana Cloud or an Enterprise license is not required.

This first version uses the established `/api` HTTP routes. Grafana 13 marks these routes as deprecated in favor of `/apis`, but official documentation states that the legacy routes remain available. The plugin has been tested against 13.3.0; it does not claim compatibility with every future release or implement the newer dashboard resource model.

## Configuration

Set the required deployment environment variable `SOKEL_GRAFANA_URL` to the **site root**, for example `http://grafana:3000` or `https://monitor.example.org/grafana`. Do not append `/api`. Internal HTTP and reverse-proxy prefixes are supported. Use HTTPS outside trusted networks. The address cannot contain credentials, query parameters, fragments, encoded path segments or dot segments. There is no public fallback.

Create a **service account token** in your instance and add it to the plugin credential form. The plugin sends `Authorization: Bearer <token>` only to the configured Grafana service. It does not create accounts/tokens, refresh tokens, manage permissions or call server administration APIs. Reads need the relevant dashboard, data source and annotation access; annotation creation requires write/create permission, normally an Editor service account with access to the target dashboard in an OSS deployment. Enterprise installations may enforce more specific RBAC actions. Use a token for the intended organization and grant only the required access.

`health_check` performs an authenticated dashboard search with limit 1 and returns `ok` and `message`. It verifies access to that read operation, not every data source or write permission. It does not rely on the public `/api/health` endpoint to claim token validity.

## Operations

| Operation | Inputs and results |
| --- | --- |
| `health_check` | Authenticated dashboard-search check. |
| `list_data_sources` | Returns summaries: UID, name, type, default flag and read-only flag. |
| `get_data_source` | Takes `uid` and returns the same safe summary for that data source. |
| `search_dashboards` | Optional `query` and `options.page`, `limit`, `tags`, `folder_uids`, `starred`. Returns dashboard summaries and `next_page`. Searches dashboards, not folders. |
| `get_dashboard` | Takes `uid`; returns UID, title, version and `model_json`. |
| `list_annotations` | Optional `filter.from`, `to`, `limit`, `dashboard_uid`, `panel_id`, `tags`. Returns user-created annotations and `limit_reached`; alert-state annotations are excluded. |
| `create_annotation` | Takes nonempty `text` and explicit epoch-millisecond `time`. Optional `options.time_end`, `dashboard_uid`, `panel_id`, `tags`. Creates one annotation and returns its decimal-string ID. |

Data source outputs deliberately omit connection URLs, usernames, passwords, `jsonData`, secure-field indicators and arbitrary configuration. The plugin does not create or update data sources, proxy requests to them, execute Prometheus queries, evaluate arbitrary plugin queries or reveal their credentials. The list endpoint has **no pagination**; Grafana's configurable server cap defaults to 5000. A returned data source list is not a guarantee that every configured data source was included. Use `get_data_source` for a known UID when necessary.

Dashboard models contain panel/plugin-specific structures, so `model_json` is JSON **text**, preserving the original model and exact integer literals. It contains user-authored dashboard content, queries and links that the token can read; inspect them before sharing outside their intended audience. The plugin does not retrieve data source secrets to fill the model. Version is returned as a string. Dashboard URLs in search results are service-provided navigation metadata and are never fetched by the plugin.

## Dashboard search

Page numbering starts at 1; default and maximum page size are 100, with page numbers up to 100000. Tags and folder UIDs are repeated query parameters, each limited to 100 entries. Keep all search options unchanged between pages. A full page returns the next page number; a short or empty page returns 0. Grafana's search endpoint has no total count or continuation token, so a full final page may require one extra empty request. Pages are not snapshots; deduplicate by dashboard UID if content changes while scanning.

`starred: false` is sent explicitly when supplied; omission leaves the server default intact. Starred status is associated with the service-account identity, not an interactive human user's account. UIDs supplied to operations must contain 1–128 ASCII letters, digits, `_` or `-`; URL/path syntax is rejected. Search queries are limited to 4096 bytes, and individual tags to 255 bytes.

## Annotation reads and writes

Example creation input:

```json
{"text":"Release deployed","time":"1507037197339","options":{"time_end":"1507037198340","dashboard_uid":"production","panel_id":"0","tags":["deploy"]}}
```

Times are decimal-string epoch milliseconds on input and output. Creation requires a time greater than zero; the supported range ends at year 9999. A region end cannot precede its start. Filters allow zero as an unbounded/default lower endpoint according to Grafana's semantics. Panel IDs are nonnegative 64-bit decimal strings; a panel option requires a dashboard UID. `panel_id: "0"` is preserved, but Grafana treats zero as unspecified rather than selecting a numbered panel. Without a dashboard UID, creation produces an organization annotation. Tags on annotation reads primarily filter organization annotations; they do not replace a dashboard filter. Multiple tags use Grafana's AND semantics.

Annotation reads return at most 100 rows, default 100. The API has no general page cursor here. If `limit_reached` is true, results may be incomplete: narrow the time range or other filters. Splitting time windows and deduplicating IDs may help, but many annotations at the same millisecond can still exceed the limit; this plugin does not promise complete export or exactly-once event delivery. It does not poll or emit events.

Annotation creation writes immediately and can affect dashboards that display matching organization tags or the specified dashboard/panel. The text limit is 1 MiB and at most 100 tags are supported. These operations do not update/delete annotations, change dashboards, run alerts or manage users.

## Transport and reliability

Only fixed API paths on the configured service are called. Redirects and cookie reuse are disabled; no arbitrary HTTP URL input is exposed. Errors do not include service error bodies or transport URLs. JSON requests are capped at 2 MiB, responses at 8 MiB. Operations allow 60 seconds, with a 45-second HTTP timeout.

Failed HTTP responses are not retried and mutation request bodies cannot be replayed automatically. A 429 response gives a bounded `Retry-After` suggestion without retrying. Annotation creation has no plugin-provided idempotency key; after a timeout, inspect existing annotations before repeating the write. Your Grafana server may itself contact configured data sources, plugins or external integrations; those dependencies remain under your deployment's control.

## Validation and sources

All seven operations passed a race-enabled acceptance run against isolated Grafana OSS 13.3.0 with a synthetic Editor service account, dashboard, noncredentialed data source and annotation. Offline tests use sanitized captured responses and synthetic boundary variants, including exact integer preservation, explicit false/zero, generated dispatch, source-configuration omission, malformed success receipts, errors and redirect refusal. This is local service acceptance, not production platform deployment.

Official sources: [13.3.0 release](https://github.com/grafana/grafana/releases/tag/v13.3.0), [legacy API status](https://grafana.com/docs/grafana/latest/developer-resources/api-reference/http-api/api-legacy/), [pinned data source API](https://github.com/grafana/grafana/blob/v13.3.0/docs/sources/developer-resources/api-reference/http-api/api-legacy/data_source.md), [dashboard search API](https://github.com/grafana/grafana/blob/v13.3.0/docs/sources/developer-resources/api-reference/http-api/api-legacy/folder_dashboard_search.md), [annotations API](https://github.com/grafana/grafana/blob/v13.3.0/docs/sources/developer-resources/api-reference/http-api/api-legacy/annotations.md), [dashboard handler](https://github.com/grafana/grafana/blob/v13.3.0/pkg/api/dashboard.go), and [AGPLv3 license](https://github.com/grafana/grafana/blob/v13.3.0/LICENSE).
