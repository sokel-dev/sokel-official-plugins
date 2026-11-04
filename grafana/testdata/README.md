# Grafana fixture provenance

`local-grafana-13.3.0.json` contains seven read responses captured on 2026-11-04 from an isolated loopback Grafana OSS **13.3.0** with synthetic service account, dashboard, data source and annotation. Tokens and request authentication headers are absent. Data source responses were intentionally reduced to `id`, `uid`, `name`, `type`, `access`, `isDefault` and `readOnly`; each capture records that adaptation. The data source had no credentials. This file is a sanitized real-service capture, not a full raw request/response recording.

The public `/api/health` capture records server readiness only and is not used as plugin credential validation. The plugin's health operation uses authenticated search. Offline tests exercise the remaining captures through actual clients; the separate race-enabled gated local test covered all seven operations, including annotation creation and dashboard-filter read-back. Other inline fixtures are explicitly synthetic boundary cases, not claimed service captures.

Official protocol sources pinned to v13.3.0:

- [Data source API](https://github.com/grafana/grafana/blob/v13.3.0/docs/sources/developer-resources/api-reference/http-api/api-legacy/data_source.md): list/get by UID, Bearer authentication, configurable 5000 default cap and absence of pagination.
- [Dashboard search API](https://github.com/grafana/grafana/blob/v13.3.0/docs/sources/developer-resources/api-reference/http-api/api-legacy/folder_dashboard_search.md): page/limit, repeated tag/folderUIDs, starred flag and dash-db results. The documentation's illustrative JSON includes comments and unquoted tokens; it is not presented here as a valid unmodified JSON fixture.
- [Annotations API](https://github.com/grafana/grafana/blob/v13.3.0/docs/sources/developer-resources/api-reference/http-api/api-legacy/annotations.md) and [handler](https://github.com/grafana/grafana/blob/v13.3.0/pkg/api/annotations.go): millisecond time/timeEnd, dashboardUID/panelId, repeated tags, bounded list, 200 annotation receipt.
- [Dashboard handler](https://github.com/grafana/grafana/blob/v13.3.0/pkg/api/dashboard.go): GET `/api/dashboards/uid/{uid}` and `{dashboard,meta}` response.
- [Legacy API status](https://grafana.com/docs/grafana/latest/developer-resources/api-reference/http-api/api-legacy/), [release](https://github.com/grafana/grafana/releases/tag/v13.3.0) and [AGPLv3 license](https://github.com/grafana/grafana/blob/v13.3.0/LICENSE).

No production service, Cloud account, published plugin or deployed platform was used.
