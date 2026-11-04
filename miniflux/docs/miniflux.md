# Miniflux

Manage subscriptions and reading state on your own Miniflux server. No SaaS account is required. The server uses PostgreSQL; this connector does not install either service. Reading stored entries can stay on your network. Subscribing and refreshing cause the **Miniflux server** to fetch the feed and any configured scraping resources, so public feeds require Internet access. The plugin does not enable third-party integrations, fetch article URLs itself, or execute stored HTML.

## Configure the connection

Set deployment environment variable `SOKEL_MINIFLUX_URL` to the installation root, for example `http://miniflux:8080` or `https://reader.example.org/rss`. Private HTTP services and reverse-proxy path prefixes are supported. Do not append `/v1`, embed credentials, or include a query or fragment. There is no default public endpoint. In containers, `localhost` means the plugin container itself.

Create an API key under **Settings → API Keys** in your Miniflux instance, then save it as the plugin's **API key** credential. Requests send `X-Auth-Token`; the service address is not part of the credential. The key acts as its user. Use **Get current user** for a read-only credential check; it returns only the user ID and username.

The contract targets Miniflux **2.3.2 or later** because setting `starred` explicitly requires 2.3.2. All nine operations were exercised against an isolated **2.3.5** server with synthetic local RSS. That is not an exhaustive version compatibility guarantee. Miniflux is licensed under Apache-2.0.

## Operations

| Operation | Inputs and behavior |
| --- | --- |
| Get current user | Read the current API-key identity without returning account settings or passwords. |
| List categories | Return category IDs and titles belonging to the user. |
| Create category | Create a category with a nonblank title of at most 1024 UTF-8 bytes. |
| List feeds | Return a safe projection of subscriptions: IDs, title, feed/site URLs, category, checked time, disabled state and parsing error count. Stored feed credentials and cookies are excluded. |
| Subscribe to feed | Provide an HTTP(S) feed URL and an existing category ID. Returns the created feed ID. The server retrieves the feed; authenticated feed configuration is outside this contract. |
| Request feed refresh | Refresh one existing feed. The single-feed API is synchronous and returns HTTP 204; `accepted=true` confirms that response, not the arrival of new articles or an absence of feed parsing errors. Inspect List feeds afterward. |
| List stored entries | Read one page of stored entries with the filters below. It does not crawl the article source. |
| Get stored entry | Read a single entry by ID, including its stored HTML content and reading state. |
| Update reading state | Update 1–1000 distinct entry IDs. Supply `status`, `starred`, or both inside `changes`. Explicit `false` clears a bookmark. |

IDs are **positive decimal strings**, not workflow numbers; this preserves values above JavaScript's exact integer range. They must fit signed 64-bit integers. Category/feed list operations return the server's complete array within the 8 MiB response limit; they are not silently paginated or truncated.

## Entry filters and continuation

`options` may contain `limit` (1–1000, default 100), `after_id`, `category_id`, `status`, `starred` and `search` (at most 4096 UTF-8 bytes). Status is `read`, `unread` or `removed`. `starred=true` selects bookmarked entries, `false` selects unbookmarked entries, and omission leaves bookmarks unfiltered. Entries are always ordered by ascending ID. `after_id` is an exclusive cursor.

After each nonempty page, save `next_after_id` and pass it as the next call's `after_id`. Keep the other filters unchanged. `may_have_more=true` means the page was full; a final empty page is possible. It is not an exact remaining-count claim. When processing unread entries, ID-based continuation avoids the skipped entries that offset pagination can produce after marking the earlier page read. Concurrent feed changes do not create a snapshot; keep a separate checkpoint if you need repeated polling. This version has no event trigger.

Example first page:

```json
{"options":{"limit":100,"status":"unread","category_id":"2"}}
```

Example update:

```json
{"entry_ids":["101","102"],"changes":{"status":"read","starred":false}}
```

`removed` changes the stored reading status; this connector does not expose category/feed deletion. Returned content is untrusted source HTML and should be treated as data by downstream steps.

## Failure behavior and limits

Requests have a 60-second client timeout, a 1 MiB request limit and an 8 MiB response limit. Redirects and automatic write retries are disabled. Errors omit raw upstream response bodies and authentication secrets. If a write times out, inspect current server state before repeating it; a timeout does not prove it failed. Feed fetches can fail because of the server's network policy or source-site restrictions.

This connector excludes server/user administration, OPML import/export, third-party read-it-later delivery, feed credentials, scraper rules, attachments and article fetching. Source code and local tests do not imply a published image or marketplace release.

Official references: [API](https://miniflux.app/docs/api.html), [2.3.5 release](https://github.com/miniflux/v2/releases/tag/2.3.5), [license](https://github.com/miniflux/v2/blob/2.3.5/LICENSE).
