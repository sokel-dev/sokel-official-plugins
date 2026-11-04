# Bocha Search

Search the web with a Bocha API key. This connector uses `https://api.bocha.cn/v1/web-search`, the endpoint in Bocha's official provider implementation. It does not follow redirects or retry billable requests automatically.

## Credential

Create an API key in the [Bocha console](https://open.bochaai.com/) and enter it as `access_token`. Keep sufficient account credit for searches. Token provisioning and replacement happen outside this connector.

`verify_credential` requires `probe_query` and performs one billable search, requesting one result without a summary. A missing query fails locally without a request. A successful probe verifies access at that moment, not future quota. This connector does not register `health_check`: the platform's no-input credential-test button cannot perform this explicit paid probe and reports that automatic health checks are unsupported. Use the verification operation or a normal search to check access.

## Operations

- `web_search`: required `query`; optional `freshness` and `options`.
- `verify_credential`: required billable `probe_query` as described above.

`options.count` defaults to 10 and accepts 1–50. `options.summary` defaults to true; explicit false is preserved. Freshness defaults to `noLimit`; use `oneDay`, `oneWeek`, `oneMonth`, `oneYear`, a date such as `2026-01-15`, or an ascending inclusive range such as `2026-01-01..2026-01-31`.

Results expose `id`, `name`, `url`, `snippet`, `summary`, `site_name` and `published_at`. Missing optional provider values are empty strings. `query`, `total_estimated_matches` and `log_id` accompany the results; absent count/log metadata becomes zero/empty. No pagination, crawling, AI answer endpoint or domain filters are included in this version. Count bounds the requested results, not an exhaustive search.

```json
{"query":"agent workflow integrations","freshness":"oneMonth","options":{"count":5,"summary":false}}
```

## Limits and errors

JSON requests and responses are bounded to 8 MiB. Authentication, permission, rate-limit and provider business-code failures are surfaced without echoing upstream response bodies. A timeout can leave billing status uncertain; inspect account usage before repeating a search. Empty result arrays are valid. An explicit provider success code of 200 also permits absent/null data or web pages, matching the official provider. Without a result collection or explicit success code, the response fails validation.

Request fixtures and generated dispatch tests cover the public protocol; they are synthetic, not live account acceptance. No live search was performed during development. This plugin has no triggers or webhooks.

References: [official Bocha provider](https://github.com/bocha-ai/dsh-web-search-bocha), [Bocha console and API example](https://open.bochaai.com/).
