# Baserow

Connect Sokel to your own Baserow Open Source Edition to discover accessible tables and fields, read rows and create, update or delete individual rows. No Baserow Cloud account, paid feature or external AI service is required. This release follows the official Baserow 2.4.1 API specification and tagged source. Its protocol tests use synthetic fixtures adapted from those sources; it has not yet been accepted against a running Baserow instance.

## Deployment and authentication

Set the required deployment environment variable `SOKEL_BASEROW_URL`, for example `http://baserow:80` or `https://data.example.internal/baserow`. Supply the server root before `/api`; the plugin appends the API routes. Internal HTTP and reverse-proxy path prefixes are supported. There is no default address or public fallback. Embedded credentials, query parameters, fragments and dot path segments are rejected.

Inside a container, `localhost` is the plugin container itself. Use a reachable container-network service name or a correctly configured host address. The plugin does not install Baserow, PostgreSQL or its other server dependencies. Use HTTPS for traffic crossing untrusted networks.

Enter a **database token** in the credential form. Requests send `Authorization: Token ...`. This plugin does not accept a login JWT, sign users in or refresh account sessions. Create and rotate the database token in Baserow, granting only the required permissions on the intended tables. Read, create, update and delete access can differ per table.

`health_check` calls the database-token-specific `/api/database/tables/all-tables/` endpoint. It returns `ok` and `message`; configuration, transport or authorization failure returns `ok=false`. An empty accessible-table list can still mean a valid token. The endpoint includes tables where the token has any supported permission, so health does not establish row-read or write access to every listed table. It has no write side effects.

## Operations

| Operation | Inputs and result |
| --- | --- |
| `health_check` | Verify configuration and database-token authentication. |
| `list_tables` | Return accessible table IDs, names and database IDs. This endpoint returns an unpaginated list, bounded by the response-size limit. |
| `list_fields` | `table_id`; return each field's ID, name, type, primary-field flag and complete `definition_json`. |
| `list_rows` | `table_id` and optional `options`; return row records, a decimal-string `total`, `next_page` and `has_more`. |
| `get_row` | `table_id`, `row_id` and optional `user_field_names`; return one row. |
| `create_row` | `table_id`, `data_json` and optional `user_field_names`; create a row and return the actual row response. |
| `update_row` | `table_id`, `row_id`, `data_json` and optional `user_field_names`; change only supplied fields and return the resulting row. |
| `delete_row` | `table_id` and `row_id`; return `deleted=true` only after the documented HTTP 204 response. This does not expose permanent trash purging. |

Table, field, database and row IDs are decimal **strings** in Sokel. Input table/row IDs must be canonical positive signed 64-bit integers: no leading zeros, decimals or exponent notation. Strings such as `"9007199254740993"` retain their exact value. Get/update responses must match the requested row ID.

## Dynamic field values

A row has application-defined fields. Each returned row contains `id` and `json`, where `json` is the complete JSON object as a string. Field definitions likewise include the complete JSON string, preserving options that differ by field type. This avoids flattening arbitrary fields or converting large integers to floating point. False, zero, null, nested values and Baserow's decimal-string row order are preserved.

By default, data keys use stable internal field identifiers such as `field_123`. Set `user_field_names=true` consistently on reads and writes to use human-readable names instead. Renaming a column can then change the keys. `list_fields` always reports actual IDs and names; inspect its definitions and your instance's generated API documentation before constructing values for select, link, file or other specialized fields.

Example creation using stable field IDs:

```json
{
  "table_id": "8",
  "data_json": "{\"field_123\":\"Example\",\"field_124\":false,\"field_125\":0}",
  "user_field_names": false
}
```

`data_json` must be a JSON object string, not an array or null. Creation accepts `{}` so Baserow can apply field defaults. Updates require at least one field and use PATCH: omitted fields remain unchanged, while an explicit null is passed to Baserow for that field's validation. Do not supply reserved top-level `id` or `order`; the server manages them. The plugin validates JSON structure, while Baserow validates field names, types and permissions. Creation and update require the documented HTTP 200 row receipt; an empty response is not success.

## Search and pagination

`list_rows.options` accepts:

- `page`: 1–1000000, default 1.
- `size`: 1–200, default 50; your server can enforce stricter limits.
- `search`: text passed to Baserow's row search.
- `order_by`: Baserow's comma-separated field ordering, such as `-field_123` or `-Name` when using field names.
- `filters_json`: a JSON object string containing Baserow's filter tree.
- `user_field_names`: whether row keys and filter/sort field references use names.

For example, a filter tree may be `{"filter_type":"AND","filters":[{"field":124,"type":"boolean","value":"false"}]}`. Field-specific filter semantics are validated by the server. Search, ordering and filter strings are limited to 16 KiB each.

Fetch one page per call. While `has_more=true`, put `next_page` into `options.page`, keeping the table, size, filters and naming mode unchanged. Completion returns `next_page=0`. Total counts are strings to preserve precision. The plugin checks count and continuation consistency and rejects repeated, missing or mismatched continuations. It never follows a URL supplied by a response: the next request is built from the configured service and validated numeric page. Returned continuation URLs must match the configured origin, path and query; configure your reverse proxy and Baserow public URL consistently if these checks fail.

Pagination is not a snapshot. Concurrent row changes can alter page contents or counts; deduplicate by row ID when assembling results across runs.

## Limits and failures

Requests are limited to 1 MiB and responses to 8 MiB. Reduce page size when rows contain large fields. HTTP calls have a 45-second timeout within a 60-second operation deadline. The plugin rejects redirects, does not retry failed HTTP responses and disables mutation-body replay. It does not echo service URLs, credentials or raw upstream error bodies.

Check table permissions after 401/403, field definitions after validation errors, and the actual row after a timeout before repeating a write. A timed-out request may have been applied. The plugin does not create or delete tables, edit schema, upload files, configure webhooks, run arbitrary API routes or use premium/enterprise endpoints. Referenced external file URLs and any integrations configured in Baserow may still have their own network dependencies.

## Official references

- [Baserow 2.4.1](https://github.com/baserow/baserow/releases/tag/2.4.1)
- [Open Source Edition and other license scopes](https://github.com/baserow/baserow/blob/2.4.1/LICENSE)
- [Database API](https://baserow.io/user-docs/database-api)
- [Database tokens](https://baserow.io/user-docs/personal-api-tokens)
- [Official API schema](https://api.baserow.io/api/schema.json)
- [Tagged token-table discovery implementation](https://github.com/baserow/baserow/blob/2.4.1/backend/src/baserow/contrib/database/api/tables/views.py)

The community edition is MIT-licensed. Premium, enterprise, documentation and third-party components have separately stated licenses; the entire repository should not be described as uniformly MIT-only.
