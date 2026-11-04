# Crawl4AI

Use the REST API of your own Crawl4AI Docker server to crawl web pages and extract Markdown. This plugin targets the self-hosted 0.9.5 protocol and provides five operations. It does not use Crawl4AI Cloud or require an LLM provider account. Browsers and the server's own dependencies must already be installed by your operator; the plugin does not download them.

## Deployment and credentials

Set the required deployment environment variable `SOKEL_CRAWL4AI_URL` to the server root, for example `http://crawl4ai:11235` or `https://internal.example/crawler`. Do not append `/crawl` or provide URL credentials, a query or a fragment. Internal HTTP addresses and reverse-proxy path prefixes are supported. There is no default public endpoint. `localhost` inside the plugin container refers to that container, not the host or the crawler container; use a service name or address the plugin can reach.

Set **Access token** in plugin credentials to the self-hosted server's configured API token or an existing accepted JWT, without the `Bearer` prefix. The plugin adds that prefix in the `Authorization` header. Obtain and renew credentials outside the plugin. With Crawl4AI 0.9.x, configure `CRAWL4AI_API_TOKEN` on the server: without it, the secure default binds to loopback inside the server container, so a published host port may remain unreachable. This server-side setting is separate from `SOKEL_CRAWL4AI_URL` on the plugin.

**Check service availability** reads public `/health`. `ok=true` confirms the expected service response; it does not prove the token is valid, the browser is ready or any particular website can be crawled. Check a permitted page to verify the full path. Health failures return `ok=false` and a concise message.

## Operations

| Operation | Behavior |
| --- | --- |
| `health_check` | Read server health and return `ok` and `message`. |
| `crawl` | Submit 1–5 distinct page URLs to `/crawl` and wait for one bounded synchronous response. Return typed page results and `all_succeeded`. |
| `submit_crawl` | Submit the same URL/options shape to `/crawl/job`. HTTP 202 returns a `task_id` and `accepted=true`; it is not a completed crawl. |
| `get_task` | Make one request to `/crawl/job/{task_id}`. Return status, completion, per-page results and a safe message. It does not poll in a loop. |
| `markdown` | Fetch one URL through `/md` with the fixed `raw` filter. Return the page URL and raw Markdown. `use_cache` defaults to false. |

For `crawl` and `submit_crawl`, `urls` must contain 1–5 different absolute HTTP or HTTPS URLs, at most 8192 bytes each, without embedded credentials. For `markdown`, the same bounds apply to its single `url`. Raw HTML, file URLs and JavaScript URLs are not accepted. Target pages can follow their own redirects inside the crawler, subject to the server's egress policy; redirects from the configured REST service itself are rejected by the plugin.

The deployment service can be on an internal network while page destinations remain governed by Crawl4AI's security policy. The current server validates page destinations and may reject private networks, loopback destinations or DNS changes. This plugin does not disable those safeguards. Self-hosting the crawler does not make external websites available offline, and websites may require authentication, block automation or refuse access.

## Crawl options and output

The optional `options` object contains only these fields:

- `page_timeout_ms`: page-navigation timeout, default 30000, accepted range 100–60000 milliseconds.
- `word_count_threshold`: minimum word count, range 0–10000; omitted means the server default. Explicit zero is preserved.
- `only_text`: whether to extract text only; omitted means the server default. Explicit false is preserved.
- `css_selector`: optional content selector, one line of at most 2048 bytes.

Requests always disable streaming. There are no arbitrary browser options, custom headers, cookies, proxies, Python or JavaScript hooks, code execution, LLM settings, webhooks, screenshots or PDF artifacts in this interface. No LLM API keys are needed for the supported paths. `markdown` fixes `f=raw`; fit, BM25 and LLM filters are deliberately outside this version's scope.

A page result exposes `url`, `success`, optional `status_code`, optional `title`, optional `markdown` and `message`. Markdown is `markdown.raw_markdown` from the upstream structured result, not HTML or an LLM answer. Empty Markdown can be a valid empty page. Failed pages have a safe message instead of raw server errors; their title and Markdown are omitted. An invalid URL on a failed upstream record is replaced with an empty string. Internal traces, HTML, response headers, extraction payloads and server links are not exposed.

A successful HTTP response or the upstream batch's `success=true` does not imply all pages succeeded. Inspect `all_succeeded` and every page's `success`. HTTP page status 400 or higher is treated as a failed page even if the upstream crawl flag is true.

## Asynchronous tasks

Store the `task_id` returned by `submit_crawl`, then invoke `get_task` after a suitable workflow delay. Supported IDs have the server's `crawl_` prefix and eight lowercase hexadecimal characters. The plugin constructs the canonical task path itself and never follows server-provided links.

| Status | Meaning |
| --- | --- |
| `processing` | `completed=false`, `all_succeeded=false`, and no page results yet. Query the same ID later. |
| `completed` | `completed=true`; inspect `all_succeeded` and the page results because individual pages can still fail. |
| `failed` | `completed=false`, `all_succeeded=false`, and a safe failure message. This is a terminal failed task, not a signal to poll forever. |

Tasks belong to the submitting principal. Query with the same identity; the server can return 404 for another principal's task, an expired task or an unknown ID. Server Redis TTL and cleanup policies control retention, and reading an old terminal task may trigger upstream cleanup. Save completed results in your workflow instead of treating task storage as a permanent archive. The plugin does not automatically resubmit failed or expired tasks.

## Limits and errors

Requests are limited to 1 MiB and full upstream responses to 8 MiB, including fields the plugin later discards. A large HTML response may exceed this limit even when the desired Markdown is small; crawl fewer pages or narrow the selector. HTTP requests have a 150-second maximum; synchronous crawl and Markdown operations declare a 180-second platform timeout. Other operations declare 60 seconds and also honor the caller's cancellation. For longer work, use asynchronous submission and later status reads.

No operation performs an application-level retry, and POST request bodies are not marked replayable. After a submission or synchronous timeout, the server may still be working; inspect known tasks before manually repeating the request. HTTP errors expose only status and safe guidance. Raw upstream errors, tokens and internal URLs are not copied into errors. HTTP 401 or 403 requires checking your server token or access policy; HTTP 429 includes a bounded retry delay.

Crawl4AI is distributed under Apache-2.0 with an additional attribution requirement. Its required attribution is reproduced here: “This product includes software developed by UncleCode (https://x.com/unclecode) as part of the Crawl4AI project (https://github.com/unclecode/crawl4ai).”
