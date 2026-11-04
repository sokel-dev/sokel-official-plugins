# linkding

Manage bookmarks and tags on your own linkding server. No SaaS account is required. The plugin supports searching, reading, saving, updating, archiving, unarchiving and deleting bookmarks, plus listing tags. The server is deployed separately; this connector does not install it or browse saved websites itself.

## Connection and authentication

Set the required deployment variable `SOKEL_LINKDING_URL` to the installation root, for example `http://linkding:9090` or `https://links.example.org/linkding`. Do not append `/api`. Internal HTTP and reverse-proxy path prefixes are supported. There is no default public endpoint. Service URLs cannot contain credentials, query parameters, fragments, escaped path segments or dot segments. Inside a container, `localhost` means that container.

Copy the API token from your own linkding Settings page into the plugin's **API token** credential. Requests send `Authorization: Token ...`. The token operates as its user; the plugin does not create users, issue tokens or manage authentication. **Check connection** reads a one-item tag page and returns `ok` and `message`. Failures return `ok=false`; no website is fetched for the check.

The implementation follows linkding **1.47.0**, and all nine generated operations passed acceptance against a disposable 1.47.0 service with synthetic local bookmark URLs. linkding is MIT licensed. This does not establish compatibility with every earlier or future version.

## Operations

| Operation | Inputs and result |
| --- | --- |
| Check connection | Check the configured service and token using the tag API. |
| List bookmarks | Optional `options` selects the active or archived list, search, dates and pagination; returns bookmarks and page metadata. |
| Get bookmark | Read one bookmark by `bookmark_id`. |
| Save bookmark | Provide `url` and optional `fields`. Creates a new bookmark or updates an existing bookmark with the same URL under linkding's save semantics. Returns the actual bookmark. |
| Update bookmark | Provide `bookmark_id` and nonempty `changes`; PATCH only the supplied fields. |
| Archive bookmark | Move a bookmark into the archive; returns `accepted=true` after HTTP 204. |
| Unarchive bookmark | Restore an archived bookmark; returns `accepted=true` after HTTP 204. |
| Delete bookmark | Delete the selected bookmark by ID; returns `deleted=true` after HTTP 204. This is a write operation, not an archive action. |
| List tags | Optional page size and offset; return tag IDs, names and creation dates. Tags can be assigned or created by name when saving/updating bookmarks. |

Bookmark and tag IDs and total counts are decimal **strings**, preserving integers above 2^53. Input IDs must be positive canonical signed 64-bit integers, without leading zeros or exponent notation. Returned bookmarks contain URL, title, description, notes, archived/unread/shared state, tag names and dates. Asset links, preview URLs and Wayback fallback links are not exposed as proof that a snapshot was created.

## Save versus partial update

**Saving an existing URL can replace existing fields, including fields omitted from the save input.** In 1.47.0 the server merges a newly constructed bookmark into the existing record: omitted title, description, notes, unread/shared state and tags can take creation defaults. The HTTP 201 receipt does not tell you whether the record was newly created. The plugin therefore reports the resulting bookmark rather than inventing a `created` flag. Use **Update bookmark** by ID when you need to preserve unspecified fields.

Save `fields` supports `title`, `description`, `notes`, `is_archived`, `unread`, `shared` and `tag_names`. Update `changes` supports these plus an optional replacement `url`. For Save, `is_archived` applies to newly created records; saving an existing URL does not reliably change its archive state. Use Archive/Unarchive or Update for an existing record's archive state.

Explicit `false` remains false, and `tag_names: []` requests replacement with an empty tag list. An empty string can clear title, description or notes on PATCH. Omitted fields stay unchanged on PATCH; JSON null is treated as omitted. Auto-tagging rules run on the server and can add matching tags back, even when the supplied list is empty. `shared=true` enables sharing according to the user's linkding settings; the plugin does not change those settings.

Example partial update:

```json
{"bookmark_id":"42","changes":{"unread":false,"notes":"Reviewed","tag_names":["research","reviewed"]}}
```

URLs must use HTTP or HTTPS, without embedded credentials, and fit within 8192 bytes. Titles are limited to 4096 UTF-8 bytes; descriptions and notes to 256 KiB each. Up to 100 distinct, nonblank tag names are accepted, each at most 200 bytes. linkding may enforce stricter field limits or normalize URLs and tag names. Updating a URL to one that another bookmark already uses is rejected by the server.

## Network behavior

Save always sends `disable_scraping=true` and `disable_html_snapshot=true`. This disables synchronous title/description scraping and automatic HTML snapshot creation for the save request. It does **not** disable the instance's separate background integrations: favicon loading, preview images, Wayback snapshots or other configured tasks may still access the network when bookmarks are created or updated. For an isolated deployment, turn off those features in linkding's server/user settings as well. The real acceptance disabled these background features.

The plugin does not call the bookmark `/check` endpoint because that endpoint can scrape the target website. It never fetches a supplied bookmark URL itself. Stored descriptions and notes remain user/site data, not executable workflow instructions. This version excludes asset transfer, snapshot generation, bundles, public/shared-list browsing and tag deletion.

## Filtering and pagination

Bookmark `options` accepts:

- `archived`: `true` selects archived bookmarks; false or omission selects the normal active list. This is not an all-bookmarks mode.
- `search`: linkding search syntax, up to 4096 UTF-8 bytes.
- `modified_since` and `added_since`: RFC3339 timestamps with a timezone.
- `limit`: 1–100, default 100.
- `offset`: 0–1000000, default 0.

List tags accepts the same `limit` and `offset`. Each call returns one page. `page.total` is a decimal string. When `page.has_more=true`, pass `page.next_offset` into the next request while preserving filters and page size. At completion, `has_more=false` and `next_offset` is absent.

Continuation URLs are checked against the configured origin, path and query, and must advance by the requested limit. The plugin never follows an arbitrary response URL. Configure the instance/proxy public URL consistently if continuation validation fails. Inconsistent counts, missing continuation metadata and repeated bookmark/tag IDs fail explicitly.

Pagination is not a snapshot. Updating, archiving or deleting items while paging can change offsets and skip entries. Collect IDs first, or restart and deduplicate when the matching set changes. Date filters alone are not an event checkpoint; this version has no event trigger or automatic polling.

## Failure handling

Requests are capped at 1 MiB and responses at 8 MiB. The HTTP timeout is 45 seconds within a 60-second operation deadline. Redirects and automatic write retries are disabled, and mutation bodies are not replayed automatically. Errors omit raw service bodies, URLs and credential values. Check server state after an uncertain write timeout before trying again; there is no plugin-provided idempotency key or optimistic concurrency condition.

Local source, tests and manifest image references do not imply image availability or marketplace publication.

Official references: [API](https://linkding.link/api/), [1.47.0 API guide](https://github.com/sissbruecker/linkding/blob/v1.47.0/docs/src/content/docs/api.md), [save/update implementation](https://github.com/sissbruecker/linkding/blob/v1.47.0/bookmarks/services/bookmarks.py), [API serializers](https://github.com/sissbruecker/linkding/blob/v1.47.0/bookmarks/api/serializers.py), [license](https://github.com/sissbruecker/linkding/blob/v1.47.0/LICENSE.txt).
