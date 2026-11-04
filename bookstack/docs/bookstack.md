# BookStack

Read and maintain knowledge content on your own BookStack instance. The plugin targets stable **BookStack 26.09.2**, its unversioned `/api` REST endpoints and the community MIT-licensed application. No SaaS account or public endpoint is required.

## Configuration

Set the required deployment environment variable `SOKEL_BOOKSTACK_URL` to the **site root**, such as `http://bookstack:80` or `https://docs.example.org/knowledge`. Do not append `/api`. Internal HTTP and reverse-proxy path prefixes are supported; use HTTPS outside trusted networks. The address cannot contain credentials, query parameters, fragments, encoded path segments or dot segments. There is no public fallback.

Create an API token in the BookStack user's profile and put its **Token ID** and **Token secret** in the plugin credential form. The user must have the **Access System API** role permission as well as permission to view or modify the requested content. The plugin sends `Authorization: Token <token_id>:<token_secret>` only to the configured instance. It does not create tokens, manage users, change permissions or implement OAuth/token refresh. `health_check` reads one book-list result; success confirms token access to that request, not permission to every page.

## Operations

| Operation | Inputs and results |
| --- | --- |
| `health_check` | Authenticated read of the book list; returns `ok` and `message`. |
| `list_entities` | `kind` is `shelves`, `books`, `chapters` or `pages`. Optional `pagination.offset` and `count`; returns entities, decimal-string `total_count` and `next_offset`. |
| `get_shelf` | `shelf_id`; returns shelf details and its visible books. |
| `get_book` | `book_id`; returns book details and `contents`, flattened in order: each chapter followed by its pages, plus pages directly in the book. |
| `get_chapter` | `chapter_id`; returns chapter details and its visible pages. |
| `get_page` | `page_id`; returns page name, parent IDs, rendered HTML, stored HTML, available Markdown, tags and revision count. |
| `search` | Nonempty `query` uses BookStack's search syntax. Optional `pagination.page` and `count`; returns typed entity summaries and `next_page`. |
| `create_page` | `parent_type` (`book` or `chapter`), `parent_id`, `name`, `format` (`html` or `markdown`) and nonempty `content`. Optional `options.priority`, `tags` and `changelog`. Creates and publishes a page immediately. |
| `update_page` | `page_id` and nonempty `changes`: optional `name`, `html` or `markdown`, `priority`, `tags`, `changelog`. Sends only supplied properties using PUT. |

Entity result types are `bookshelf`, `book`, `chapter` and `page`. Each entity ID is meaningful together with its type; different types may share the same numeric ID. All IDs and total/revision counts are returned as decimal strings to preserve large integers. Input IDs must be positive 64-bit decimal strings without leading zeros. Nullable parent IDs are omitted when absent. The result contains selected content fields, not user records, permission configuration, attachments or every upstream property.

## Create and update pages

Example creation input:

```json
{"parent_type":"book","parent_id":"12","name":"Release notes","format":"markdown","content":"# Release notes\n\nChanges for this release.","options":{"priority":0,"tags":[{"name":"Category","value":"Release"}],"changelog":"Initial version"}}
```

Example partial update:

```json
{"page_id":"42","changes":{"html":"<p>Updated content</p>","priority":0,"tags":[],"changelog":"Refresh content"}}
```

Supply either HTML or Markdown, never both. Omitted update fields are unchanged. An explicitly empty content string is sent to clear content; an empty tag array removes tags. Explicit priority `0` is preserved. JSON null is treated as omitted. Page names must contain 1–255 UTF-8 bytes; content is limited to 1 MiB; priority to -1000000 through 1000000. Up to 100 tags are supported, with a nonempty name and name/value lengths of at most 255 bytes. A revision note must contain 1–180 bytes. The plugin does not move pages, create shelves/books/chapters or delete content.

Page writes are immediate and may create revisions or trigger BookStack's configured notifications and webhooks. For HTML, use plain single-block-depth elements compatible with BookStack's editor. BookStack may extract base64 data-URI images into its image gallery as part of writing content. `html` is rendered page content and can include page includes; `raw_html` is the stored edit source. Markdown may be empty if the last edit used a different editor. Returned HTML is content, not a trusted application template: render it with appropriate sanitization in downstream applications.

## Pagination

Ordinary lists use **offset** pagination, default count 100 and maximum 100 per call. Offset ranges from 0 to 10000000. Results are requested in ascending ID order. Continue with `next_offset` and the same kind/count until it is 0. The next offset advances by the actual returned count, so a short page is not silently treated as the end when `total` indicates more. An empty page before the reported end is an error.

Search instead uses **page** pagination, starting at 1, default count 20, maximum count 100 and maximum page 100000. Keep the query/count unchanged and continue with `next_page` until 0. The plugin sends no ordinary-list sort or filter to the search endpoint. Search does not return complete page content; use `get_page` for that. Lists and search results are not snapshots; concurrent changes may require deduplication by entity type and ID. Each operation fetches one page only.

## Transport and limitations

The plugin only contacts its configured instance using fixed API paths. It does not follow redirects, reuse cookies, fetch arbitrary URLs or read local files. Credentials and service error bodies are excluded from errors. JSON requests are capped at 2 MiB and responses at 8 MiB. Operations allow 60 seconds, with a 45-second HTTP timeout.

There is no automatic retry loop for failed HTTP responses, and mutation bodies cannot be automatically replayed. A 429 error supplies a bounded wait suggestion from `Retry-After`. After a write timeout, inspect the page before retrying; creation has no plugin-provided idempotency key and updates have no optimistic revision precondition. Concurrent editors may overwrite each other's changes. The BookStack server still requires its own PHP/database deployment; any optional email or other external integrations remain under your control.

## Validation and sources

Tests use the stable release's official API request/response examples plus explicitly synthetic boundary variants. They cover generated credential/input/output dispatch, nested book contents, offset versus search pagination, precise IDs, empty tags/content, zero priority, malformed success responses, redaction and redirect refusal. **No live BookStack instance was run for this implementation.** No production deployment is claimed.

Official sources: [26.09.2 release](https://github.com/BookStackApp/BookStack/releases/tag/v26.09.2), [API documentation](https://demo.bookstackapp.com/api/docs), [pinned API examples](https://github.com/BookStackApp/BookStack/tree/v26.09.2/dev/api), [page API controller](https://github.com/BookStackApp/BookStack/blob/v26.09.2/app/Entities/Controllers/PageApiController.php), [search API controller](https://github.com/BookStackApp/BookStack/blob/v26.09.2/app/Search/SearchApiController.php), and [MIT license](https://github.com/BookStackApp/BookStack/blob/v26.09.2/LICENSE).
