# Paperless-ngx

Use your own Paperless-ngx document archive from workflows: search extracted text, read and update document metadata, upload files for ingestion, inspect ingestion tasks, download documents and manage tags. This plugin targets the stable Paperless-ngx **3.4.1** release and explicitly requests **API version 10**. Paperless-ngx is GPLv3 licensed. A hosted account is not required.

## Configure the service

Set the required deployment environment variable `SOKEL_PAPERLESS_URL` to the **site root**, such as `http://paperless:8000` or `https://documents.example.org/archive`. Do not append `/api`. A reverse-proxy path prefix is supported. The address must be HTTP or HTTPS without embedded credentials, query parameters, fragments, encoded path segments or `.` / `..` segments. HTTP is available for trusted internal networks; use HTTPS across untrusted networks. There is no public endpoint fallback.

Put a Paperless API token in the plugin's **API token** credential field. The plugin sends `Authorization: Token <token>` and `Accept: application/json; version=10`. Obtain the token using your instance's supported token-management or API authentication flow; this plugin does not collect account passwords, create tokens, implement OAuth or refresh tokens. The user needs view permissions for reads and the appropriate add/change permissions for uploads, tags and metadata changes, together with object-level access.

`health_check` reads one page of at most one document and returns `ok` and `message`. It checks authentication, document visibility and the API 10 response shape. It does not prove that ingestion workers, OCR or write permissions are functioning. If the service reports an `X-Api-Version` other than 10, the request fails. Older API 9 task responses are not accepted.

## Operations

| Operation | Inputs and result |
| --- | --- |
| `health_check` | Checks an authenticated API 10 document-list request. |
| `list_documents` | Optional `query` uses Paperless full-text search syntax. Optional `pagination.page` and `pagination.page_size`; returns documents, `total_count` and `next_page`. |
| `get_document` | Takes a decimal-string `document_id`; returns metadata and extracted text for the current/latest version. |
| `upload_document` | Takes a platform `file` plus optional `options.title`, `tags`, `correspondent_id` and `document_type_id`. Returns `accepted` and a consumption `task_id`, not a document ID. |
| `get_ingestion_task` | Takes the upload's UUID. Returns `found` and a task with status, completion flags and `related_document_ids`. |
| `download_document` | Takes `document_id`; optional `options.original` and `version_id`. Returns a platform file. |
| `update_document` | Takes `document_id` and `metadata` containing at least one supported change: `title`, `tags`, `correspondent_id` or `document_type_id`. |
| `list_tags` | Returns a page of tags with ID, name, color and inbox flag, plus `total_count` and `next_page`. |
| `create_tag` | Takes `name` and optional `options.color` (`#RRGGBB`) or `is_inbox_tag`. Creates one tag. |

All document, version, tag, correspondent and document-type IDs are positive 64-bit decimal **strings** without leading zeros. Output IDs and total counts also use strings to preserve large integers. Dates are returned as provided by Paperless, including date-only creation values; they are not converted into local timestamps. The plugin exposes selected stable fields, not every Paperless field or administrative endpoint.

## Upload and follow ingestion

Supply a workflow/platform file object with an ID or inline data, a plain filename and an optional media type. Files must contain 1 byte to 32 MiB. Chinese filenames are supported. Local filesystem paths and URL-only file objects are rejected. Fetching a platform file uses the SDK's platform file mechanism; the plugin does not fetch arbitrary remote URLs. Declared and fetched sizes are checked.

An upload queues consumption and can trigger Paperless workflows, OCR and configured automations. HTTP 200 and `accepted: true` mean the queue accepted the request; they do **not** mean that a document has been created or indexed. Call `get_ingestion_task` with the returned UUID. In API 10, statuses are `pending`, `started`, `success`, `failure` and `revoked`. `complete` is true for the last three; `succeeded` is true only for `success`. Inspect `related_document_ids` for resulting documents. An empty ID list does not justify inventing a document ID. `found: false` means no visible matching task was returned; the accompanying task fields are not meaningful. Tasks may be delayed, removed or hidden by permissions. The plugin does not poll automatically or emit events.

An upload options example is:

```json
{"title":"Quarterly report","tags":["12","18"],"correspondent_id":"3","document_type_id":"5"}
```

The `tags` upload field is sent as repeated multipart form values, as required by Paperless. Existing correspondent, document-type and tag IDs must be accessible to the token's user.

## Metadata and downloads

Metadata updates use PATCH. Omitted fields are unchanged. An explicit empty `metadata.tags: []` removes all tags; an empty title is sent as an empty string. Correspondent and document-type IDs must be positive strings; clearing these relationships with null is not supported in this first version. JSON null is treated as omitted. At most 1000 unique tag IDs are accepted. Titles are limited to 255 UTF-8 bytes, tag names to 128 bytes, and search queries to 4096 bytes. Explicit `is_inbox_tag: false` and `original: false` are preserved.

By default, Paperless downloads an archived version when one exists, otherwise the original. Set `options.original: true` to request the original. Optional `version_id` selects a specific file version. The response must be a valid attachment with a plain filename and is limited to 32 MiB. The plugin validates the response length and stores its bytes as a platform file; platform upload failures are reported. It does not return temporary external download links or forward the API token through redirects.

## Pagination and reliability

Pages start at 1, with a default size of 50 and maximum of 100. Page numbers are limited to 1–100000. Keep the query and page size unchanged and pass `next_page` as `pagination.page` until it is 0. Pagination follows the server's `next` field even for short pages. A continuation must retain the configured origin, endpoint, filters and page size and advance exactly one page. The plugin returns only the validated page number and never fetches a server-provided URL directly. Each call returns one page. Lists are not snapshots; concurrent changes may require deduplication by document or tag ID.

JSON responses are capped at 8 MiB. Upload and download operations allow 300 seconds at the platform level, with a 240-second HTTP limit; other operations allow 60 seconds. The instance still needs its own ingestion workers, storage and any enabled OCR components. Optional external mail, AI or workflow integrations are controlled by your Paperless deployment, not by this plugin.

The plugin rejects redirects, does not reuse cookies, does not retry failed HTTP responses and prevents automatic replay of mutation bodies. Errors omit service response bodies and transport URLs. A 429 response produces a bounded `Retry-After` wait suggestion without retrying. Uploads, tag creation and metadata updates have no plugin-provided idempotency key: after a timeout, check tasks or current state before repeating a write. Duplicate-file detection is Paperless behavior, not a substitute for exactly-once delivery.

## Validation and references

Tests cover request bytes, API 10 task states, multipart metadata and Chinese filenames, precise IDs, empty-tag replacement, optional false, platform file failures, pagination, generated dispatch, HTTP errors and redirect refusal. They are protocol tests with synthetic responses grounded in stable release source; **no live Paperless service was used for this implementation**. No production deployment is claimed.

Official sources: [Paperless-ngx 3.4.1 release](https://github.com/paperless-ngx/paperless-ngx/releases/tag/v3.4.1), [stable release API guide](https://github.com/paperless-ngx/paperless-ngx/blob/v3.4.1/docs/api.md), [API views](https://github.com/paperless-ngx/paperless-ngx/blob/v3.4.1/src/documents/views.py), [serializers](https://github.com/paperless-ngx/paperless-ngx/blob/v3.4.1/src/documents/serialisers.py), [task status definitions](https://github.com/paperless-ngx/paperless-ngx/blob/v3.4.1/src/documents/models.py), and [GPLv3 license](https://github.com/paperless-ngx/paperless-ngx/blob/v3.4.1/LICENSE).
