# Supabase

Use the Data REST API for database records and functions, plus Storage for object listings and signed download URLs. Configure `SUPABASE_URL` on the plugin process, for example `https://your-project.supabase.co`. Use a project origin without a path or query. Deploy a separate instance for each project. HTTPS is required; local development may use HTTP on localhost or a loopback IP.

## Credentials

Set **Project API key** to a publishable or secret key, or a legacy anon/service-role JWT. New `sb_publishable_` and `sb_secret_` keys are sent only in `apikey`. Legacy keys are also sent as a bearer token. An optional **User access token** overrides the bearer token; it is not refreshed automatically. Leave it empty when using a server secret key without an end-user identity.

A secret/service-role key can bypass Row Level Security (RLS). Use it only for a workflow intended to have that access. Publishable/anon keys and user tokens use the project's grants and RLS policies. API keys do not automatically grant access to every table. Enable the Data API and expose the desired schemas.

## Database operations

`select_rows`, `insert_rows`, `upsert_rows`, `update_rows` and `delete_rows` work with a table or view and an exposed schema (default `public`). Table, schema, function and filter-column names use simple identifiers: letters, digits and underscores, beginning with a letter or underscore. Quoted or schema-qualified names are unsupported. Row objects contain your own database columns and JSON values; explicit false and null values are preserved. For input integers beyond JavaScript's safe range (±9,007,199,254,740,991), pass strings, because the platform/SDK JSON binding can round numeric inputs. Response decoding preserves original numeric digits; downstream consumers should use strings or arbitrary-precision parsing when needed.

Filters are ANDed. Each has `column`, `operator` and a string `value`, for example `{"column":"id","operator":"eq","value":"42"}`. Supported operators are `eq`, `neq`, `gt`, `gte`, `lt`, `lte`, `like`, `ilike`, `in`, `is`, `cs`, `cd`, `ov`; use PostgREST value syntax, such as `(1,2)` for `in` or `null` for `is`. Query-control names such as `select` and `limit` cannot be filter columns. Updates and deletes require at least one filter; all matching accessible rows can be changed. There is no automatic row cap on mutations.

Reads accept a PostgREST `select` expression and `order`, a page size of 1–1000 (default 100), and an offset. Use a stable unique ordering such as `id.asc`. Follow `next_offset` until an empty page; the server may cap rows below the requested limit. `content_range` includes the exact total when the server supplies it. Offset pagination is not a snapshot while other clients write.

Insert/upsert accepts 1–1000 nonempty row objects. Upsert uses merge-duplicates and optional comma-separated unique `on_conflict` columns; the primary key is the default. Rows in one batch must have compatible column sets. Mutations request returned rows, so SELECT permissions may also be required. Writes are not retried automatically.

`call_function` sends named JSON arguments to a database function. It may modify data depending on the function definition. The `result_json` string preserves scalar, object, array or null results; parse it in the workflow if structured fields are needed.

## Storage and credential checks

`list_objects` lists a bucket folder prefix in name order with offset pagination. Results can include folder entries with an empty object ID. `create_signed_url` creates a temporary download link for an existing object; expiry defaults to 3600 seconds and may be 1–604800. Anyone with the link can read the object until it expires. These operations do not upload or delete objects.

`health_check` needs a table name and optionally a schema. It makes a read-only HEAD request and reports `ok` and `message`. Without a table it reports unverified, including in a platform credential check that passes no inputs. Success checks authentication and table access; RLS may still hide every row. Storage access is checked by its own operations.

JSON requests and responses are limited to 16 MiB. Redirects are rejected and upstream response bodies are excluded from errors. There are no Realtime triggers in this version. Automated checks use documented API shapes and synthetic responses; live project acceptance remains separate.

References: [Data REST API](https://supabase.com/docs/guides/api), [API keys](https://supabase.com/docs/guides/getting-started/api-keys), [PostgREST tables and filters](https://docs.postgrest.org/en/stable/references/api/tables_views.html), [Storage API](https://supabase.com/docs/reference/self-hosting-storage).
