# Firecrawl

Configure an API key from the Firecrawl dashboard. This plugin uses the official v2 API. Set the deployment endpoint with `SOKEL_FIRECRAWL_BASE_URL`, default `https://api.firecrawl.dev`. HTTPS is required; HTTP is allowed only for loopback tests.

- `scrape` returns Markdown, HTML, links and typed metadata; unrequested formats are empty.
- `crawl` returns a job ID; call `crawl_status` to poll. If `next` is present, read its `skip` query parameter and query again with the original job ID and that offset. The plugin never follows arbitrary pagination URLs.
- `map` returns discovered links.
- `extract` submits a JSON Schema or prompt; `extract_status` reads the asynchronous result.
- `health_check` reads team credit usage without creating a crawl. Zero credits return unavailable.

Requests have a 150-second total timeout and a 16 MiB response limit. Redirects are rejected. Errors never echo upstream bodies or keys. Target URLs are sent only in the Firecrawl request body; credentials are never sent to target pages. Failed jobs return an error. Poll every 2–5 seconds. Submission requests are not automatically retried to avoid duplicate billing.

Official reference: https://docs.firecrawl.dev/api-reference/endpoint/scrape
