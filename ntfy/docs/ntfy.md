# ntfy

Send workflow notifications and read cached messages through your own ntfy server. No ntfy.sh account, SaaS subscription, Firebase project or cloud API key is required. This plugin always sends `Firebase: no`.

## Configure

1. Run an ntfy server reachable from the plugin. Local acceptance used the official `binwiederhier/ntfy:v2.37.1` image. The connector uses the HTTP API, not the ntfy command-line client.
2. Set the plugin deployment environment `SOKEL_NTFY_URL`, for example `http://ntfy:80` or `https://notify.example.org/ntfy`. There is no default or public-service fallback. HTTP and HTTPS, private hosts and reverse-proxy path prefixes are supported. The URL cannot contain user information, a query or a fragment.
3. Enter an optional **Access token** if your topics are protected. Create a token using your own server's account tooling. This connector supports bearer tokens, not username/password authentication. Leave it empty only when your server permits anonymous access to the chosen topics.

`localhost` inside the plugin container means that container. Put the plugin and service on a shared network or use an address reachable from the plugin. Use HTTPS across untrusted networks; custom certificates must be trusted by the runtime. Certificate verification is never disabled. Redirects are not followed.

## Operations

| Operation | Inputs and behavior |
| --- | --- |
| Check service health | Reads `/v1/health`; returns `healthy`. It does not test topic permissions. |
| Publish notification | Required `topic` and `message`; optional `options.title`, `priority` (1–5), `tags`, `click` and `cache`. Returns the server's message ID, timestamp and available metadata. |
| Poll cached notifications | Required `topic`; optional `since`, `cursor` and `limit`. Returns `messages`, `next_cursor` and `has_more`. The request ends after the available cache is read. |

Topics contain 1–64 ASCII letters, digits, underscores or hyphens. Messages are limited to 4096 UTF-8 bytes; titles to 512 bytes; at most 20 tags of 64 bytes each. `click` is an HTTP(S) link displayed by a client; the plugin never fetches it. No e-mail/SMS forwarding, attachments upload, scheduled delivery or notification action buttons are exposed. Received attachment metadata is returned without downloading files.

Example publication:

```json
{"topic":"workflow-alerts","message":"Report is ready","options":{"title":"Daily report","priority":3,"tags":["white_check_mark"],"cache":true}}
```

## Polling and delivery

For the initial request, `since` accepts a positive Unix timestamp (up to 9999999999), duration such as `10m`, or `all`. Omitted means `all`. Native message IDs are intentionally not accepted: ntfy may replay earlier messages from the same second when continuing by message ID.

`limit` defaults to 100 (1–1000; zero also selects the default). Reuse `next_cursor` as `cursor` for the same topic, and omit `since`. Continue immediately while `has_more` is true. Save the cursor after processing the messages, including when the current cache is exhausted; empty polls preserve it. The opaque cursor tracks the boundary timestamp and IDs already delivered during that second, so repeated upstream messages are deduplicated while newly visible IDs in that second remain eligible. Messages are sorted by timestamp and ID; ID order within a second is not publication order. Do not edit the cursor. At most 4096 IDs can be retained for one second; higher-volume streams need partitioning or a durable queue. Response truncation and malformed cursor/stream data fail without advancing the cursor.

Publication is an acceptance receipt, not proof of device delivery or durable persistence. This is a best-effort cache reader, not a durable event queue or an automatic trigger. Cache expiry, disabled caching, delayed messages older than an already advanced timestamp and clocks that move backwards can make messages unavailable. An empty poll should be retried with the same cursor rather than republishing. For an initial high-volume topic, use a bounded time window rather than `all`.

HTTP requests time out after 60 seconds and responses are limited to 8 MiB. No automatic write retries are performed; retrying a publication can send a second notification. Requests that receive HTTP 429 fail for the workflow to handle later.

For an entirely local setup, also leave ntfy server-side upstream forwarding and external Web Push endpoints unconfigured and use web or compatible Android clients. iOS instant push can require an external relay and Apple push infrastructure; self-hosting the server alone does not make that path offline. SearXNG or other network tools are not needed by this plugin.

## Validation

Protocol and generated-dispatch tests cover authentication, Unicode input, explicit `cache=false`, HTTP errors, response bounds, polling and cursor handling. Health, publication, same-second deduplication and continuation were also exercised against an isolated ntfy 2.37.1 container using synthetic topics. This does not establish platform installation or production device delivery.
