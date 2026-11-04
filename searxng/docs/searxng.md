# SearXNG

Search the web through your own open-source SearXNG metasearch instance. This connector requires no SaaS search subscription or vendor API key. Your selected engines still need network access, and some optional engines have their own account requirements. Enable engines that suit your deployment.

## Configure

Set `SOKEL_SEARXNG_URL` in the plugin deployment, for example `http://searxng:8080` or `https://search.example.org/searxng`. HTTP(S), internal hosts and reverse-proxy path prefixes are supported. There is no default public endpoint. The URL must not contain credentials, a query or a fragment. Container `localhost` refers to the plugin container; use a shared service network or a reachable host address.

Enable JSON responses in SearXNG settings:

```yaml
search:
  formats:
    - html
    - json
```

Reload the instance after changing its configuration. A 403 response can mean JSON output is disabled, authentication is missing or access is denied. HTML responses commonly indicate a proxy login page. Bot protection or engine restrictions may also block automated calls.

Optional **Proxy username** and **Proxy password** support HTTP Basic authentication at your reverse proxy; supply both or neither. They are not SearXNG account credentials. Credentials are sent only to the configured service. Redirects, including external search bangs that redirect elsewhere, are not followed. TLS verification remains enabled; trust custom certificates in the runtime rather than disabling checks.

## Operations

| Operation | Behavior |
| --- | --- |
| Search the web | Required `query`; optional `options` selects engines, categories, language, page, result limit, safe search and time range. |
| List search engines | Reads instance name, version, categories and available engines with supported features. |
| Check service health | Checks `/healthz`. A healthy process does not prove JSON search or external engines work. |

Example:

```json
{"query":"self hosted document search","options":{"categories":["general"],"language":"en","page":1,"limit":20,"safe_search":1,"time_range":"month"}}
```

`options.page` starts at 1 and is bounded to 1000. `limit` is 1–100, default 20; it caps results from the current upstream page. If `clipped=true`, some results from that same page were omitted. Increase the limit to read more of that page. No total count or reliable `has_more` is invented: engines differ in their pagination support. Request subsequent pages explicitly when the selected engine supports paging.

Omitting `safe_search` preserves the instance setting; explicitly passing 0 disables the filter, 1 requests moderate and 2 strict filtering where supported. `time_range` accepts `day`, `month` or `year`. Engine/category names come from **List search engines** and are case-sensitive. Up to 30 engines or categories can be selected. Queries are limited to 4096 UTF-8 bytes.

Outputs include result title, URL, snippet, engine names, category and score when available, plus suggestions, corrections and engine failures. `partial=true` means at least one engine reported an error; valid results are still returned. Empty results with engine failures should not be interpreted as proof that nothing exists. Scores are upstream ranking data, not calibrated confidence. Rich answer cards, infoboxes and media-specific fields are outside this first version. Result links and snippet HTML are returned as data and are never fetched or executed by the plugin.

## Limits and validation

Each HTTP call times out after 60 seconds. Search declares a 90-second workflow operation timeout. Responses are bounded to 8 MiB; rate limits and proxy failures return errors without leaking raw service bodies. There are no automatic retries.

The implemented routes and response fields were checked against official SearXNG documentation and source on 2026-11-04. Tests cover generated field binding, explicit zero versus omitted safe search, partial results, page clipping, configuration, proxy authentication and transport boundaries. Search fixtures derive from upstream unit-test data and response serialization; they are not live search captures. No SearXNG instance or production search engine was contacted for acceptance.
