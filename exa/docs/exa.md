# Exa

Create an Exa API key and save it in the plugin credential. This integration supports Search, Contents, Answer and the current asynchronous Agent API.

- `search`: query the web with auto/lite/deep-lite/deep modes, domain and publication-date filters. `options.num_results` is 1–100. Results include source URLs, available metadata, page text and highlights.
- `get_contents`: supply exactly one of `urls` or search-result `ids`, with 1–100 entries. Inspect `statuses` even when the operation succeeds: individual pages may fail. URLs must be HTTP(S) without embedded credentials.
- `answer`: returns answer text and citations. An object-root `output_schema` returns `answer_data` instead; the text field is then empty. Models: exa, exa-pro, exa-research, exa-fast. Streaming is not supported.
- `agent_start`: creates an asynchronous research run and returns its ID immediately. `agent_status` returns the run state, text, object-shaped structured output and safe failure code. Failed/cancelled jobs remain typed results; branch on `run.status`. Poll with workflow delays, not a blocking loop.
- `health_check`: reads `GET /agent/runs?limit=1`. It validates API key access to this read-only endpoint, without starting research or charging a search. It does not guarantee Search/Answer credits or permissions; restricted keys may fail this check even when another product is available.

Example search input:

```json
{"query":"AI safety research","options":{"num_results":5},"contents":{"highlights":true,"text":false}}
```

`contents` on search and `options` on get_contents accept `text`, `highlights` and `max_age_hours`. Highlights default to true only when neither format is supplied. Explicit false and zero are preserved. `max_age_hours` accepts -1 for cached content only, 0 for fresh fetches, or 1–720 hours; lite search does not support it. Publication filters use RFC3339 timestamps.

Example asynchronous research input:

```json
{"query":"Compare three public research tools","effort":"auto","budget":{"max_cost_dollars":1}}
```

Effort may be auto, minimal, low, medium, high, xhigh or ultra. A USD budget of 1–100 is supported only for auto/ultra. Without a cap, Exa currently defaults to $5 for auto and $20 for ultra. `max_duration_seconds` accepts 300–10800 and requires ultra. This is a provider soft duration limit, not the submission request timeout. The plugin only supports object-root structured schemas, including when reading an existing run. No third-party Connect data sources, legacy Research endpoints, cancellation, streaming or automatic retries are implemented. A submission timeout is ambiguous: check Exa before retrying to avoid duplicate billed runs.

Deployment uses `SOKEL_EXA_BASE_URL`, default `https://api.exa.ai`. Credentials stay in the credential form. HTTPS is required; HTTP is allowed only on loopback. Target-page URLs are JSON data and never receive your API key from this plugin. All redirects are refused. HTTP timeouts are 150 seconds for search/content/answer, 45 seconds for agent submission/status and 20 seconds for health. Requests are capped at 1 MiB and responses at 16 MiB. Errors do not expose upstream bodies or keys.

Protocol tests use official-documentation-derived examples and synthetic edge cases. No live account was exercised. Official documentation: https://exa.ai/docs/reference/search
