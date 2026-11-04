# PocketBase

Connect workflows to records in an existing self-hosted PocketBase instance. This plugin provides seven operations and does not require a hosted account. It uses the regular records REST API documented for PocketBase 0.41.1. PocketBase remains pre-1.0; review upstream migrations before upgrading your server.

## Deployment and credentials

Set the required deployment environment variable `SOKEL_POCKETBASE_URL` to the service root, for example `http://pocketbase:8090` or `https://internal.example/pocketbase`. Do not include `/api`, credentials, a query or a fragment. Internal HTTP addresses and reverse-proxy path prefixes are supported. There is no public-service fallback. `localhost` inside the plugin container refers to that container, not your host or the PocketBase container; use a reachable service name or host address.

In plugin credentials, set **Access token** to an existing auth record token, without a `Bearer` prefix. PocketBase expects the token directly in the `Authorization` header. Obtain and renew it outside this plugin. Use a regular auth record with only the required collection permissions; do not use a superuser token. This plugin does not log in, refresh tokens, administer collections, mint tokens or expose system collections beginning with `_`.

Collection rules independently control listing, viewing, creating, updating and deleting. A valid token does not grant every operation. PocketBase may deliberately return an empty list or HTTP 404 when rules hide data, so an empty result does not establish that a record is absent for every user.

**Check service availability** calls the public `/api/health` endpoint. `ok=true` confirms a recognizable healthy service response, not token validity or collection permissions. Run a read operation against an intended collection to verify its access. Configuration, network and HTTP failures return `ok=false` with a concise message.

## Operations

| Operation | Inputs and result |
| --- | --- |
| `health_check` | Checks the configured service and returns `ok` and `message`. |
| `list_records` | Collection ID or name, plus optional query settings; returns typed record wrappers, exact total count as a string, `has_more` and `next_page`. |
| `find_record` | Collection and a required filter expression, with optional sort and relation expansion; returns `found` and `records`, containing zero or one record. |
| `get_record` | Collection and record ID, with optional relation expansion; returns one record. |
| `create_record` | Collection and `data_json`; creates one record and returns the resulting record. |
| `update_record` | Collection, record ID and `data_json`; changes supplied fields and returns the resulting record. |
| `delete_record` | Collection and record ID; returns `deleted=true` only after HTTP 204 with an empty body. |

Collection names and IDs must contain only letters, digits, underscores or hyphens, start with a letter, digit or underscore, and fit within 128 characters. Collection names beginning with `_` are excluded. Record IDs remain strings and follow the same character and length bounds.

## Reading and pagination

`list_records.options` accepts `page` (default 1, maximum 1000000), `per_page` (default 30, range 1–200), `filter`, `sort` and `expand`. Filter, sort and expansion values are single-line strings of at most 8192 bytes each. The filter uses PocketBase's own expression syntax, for example `enabled=false && title~"report"`. Sort can be `-created,id`; expansion can be `author,author.team` where the server permits those relations. This plugin does not interpolate filter values: quote and escape any dynamic strings according to PocketBase syntax.

Only one page is requested per invocation. When `has_more=true`, invoke again with `next_page` and the same collection, size, filter, sort and expansion. Stop when `has_more=false`, for which `next_page=0`. Counts are requested explicitly with `skipTotal=false`. The plugin checks returned page metadata and fails on inconsistent results rather than silently claiming completion. Pagination is not a snapshot: writes and permission changes between pages may cause duplicates or gaps. Use a stable sort with an ID tie-breaker where possible and deduplicate IDs when needed.

`find_record` requests page 1 with one record and the supplied filter; set a sort when several matches are possible. An empty match is `found=false` and `records=[]`, not an invented empty record. Relation expansion remains subject to the server's view rules.

## Dynamic field values

A record wrapper exposes `id`, `collection_id`, `collection_name` and `json`. The complete record is a JSON **string** because each collection has its own dynamic schema and JSON numbers may exceed the exact range of workflow floating-point values. IDs and numeric text such as `9007199254740993` are preserved by the plugin. PocketBase's own field types and SQLite storage still determine the precision actually stored on the server.

For creation and update, supply a JSON object string, for example:

```json
{"title":"Quarterly report","enabled":false,"rank":0}
```

Explicit `false`, `0` and `null` are sent unchanged. Creation accepts `{}` when server defaults suffice, and may include a valid string `id`. Updates require at least one field and cannot change `id`. Do not set `collectionId`, `collectionName` or `expand` in either request. Other field validation is performed by your collection schema. PATCH updates only the provided fields; omission and an explicit empty value have different meanings.

This version supports JSON records only. It does not upload files, download protected files, batch mutations, subscribe to realtime events or expose collection management. Use PocketBase's own tools for those tasks.

## Limits and failure handling

Requests are limited to 1 MiB, responses to 8 MiB and each HTTP request to 45 seconds. Redirects are rejected; the token is sent only to the configured service origin and path prefix. The plugin does not retry writes or poll automatically, and mutation bodies are not marked replayable. After a timeout or connection failure, inspect the record before manually retrying a creation or update because the server may already have applied it.

HTTP errors report status and safe guidance without reflecting upstream response bodies, tokens or internal URLs. HTTP 401 usually means the token must be replaced; HTTP 403 or 404 can reflect collection rules. HTTP 429 includes a bounded retry delay. A successful HTTP response with missing or mismatched record metadata is treated as an error.
