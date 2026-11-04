# Airtable

Connect Airtable bases and tables using a personal access token (PAT). This plugin does not register an OAuth provider or perform an OAuth consent flow.

## Credential

Create a personal access token in Airtable's developer hub. Grant access to the required bases and select the scopes needed by your workflow:

- `schema.bases:read`: base listing, table schemas and the credential health check.
- `data.records:read`: record reads and polling.
- `data.records:write`: record creation, updates and deletion.

Paste the PAT into `token`. `health_check` calls the base-listing API and returns `ok` and `message`. A successful check does not prove write access to every table.

## Operations

| Operation | Behavior |
| --- | --- |
| `health_check` | Checks PAT access to the metadata API. |
| `airtable_bases` | Lists one page of accessible bases; returns the next `offset`. |
| `airtable_tables` | Returns the tables and fields in `base_id`. |
| `airtable_records` | Lists up to 100 records; supports `offset`, `view`, `filter_formula` and selected `fields`. |
| `airtable_get` | Reads a record by `record_id`. |
| `airtable_create` | Creates one record using the supplied `fields` object. |
| `airtable_update` | PATCHes one record; omitted fields remain unchanged. |
| `airtable_delete` | Deletes one record and returns its ID and `deleted` receipt. |

Use IDs beginning with `app`, `tbl` and `rec`; table names are not accepted. Pass pagination offsets unchanged and retain the same filters between pages. An empty offset means the last page. Operations return one page rather than silently truncating an automatically accumulated list.

Field names and value types depend on your table schema. Use `airtable_tables` to inspect them. `typecast=true` opts into Airtable's automatic type conversion; it is omitted by default. Explicit zero, false, empty string and null values are preserved when writing. Airtable may omit empty values from read responses; absent fields are not invented by the plugin. JSON numbers are decoded without losing integer precision.

## Record-change events

Set both `watch_base` and `watch_table` to enable `record_changed`; leave both empty to disable it. One credential watches one table. The source scans the complete table, up to 500 records, and establishes a baseline on first success without emitting history.

Later complete scans emit `change=created` for newly observed IDs and `change=updated` when a record's visible content changes. The event includes `base_id`, `table_id` and the structured `record`. Deletions are not emitted. Intermediate edits, transient rows and changes hidden by access permissions may not be observed. These events describe changes between snapshots, not Airtable's exact mutation history.

Hashes and a revision counter are saved in `_airtable_poll_state`. Incomplete pages, repeated cursors, duplicate IDs, oversized tables and delivery failures do not advance the baseline. Checkpoint failures may cause replay; stable event IDs allow Sokel to deduplicate. Changing the watched table establishes a new baseline. Keep one active source process per credential.

Polling consumes API calls. A 500-record table requires five requests per scan, so consider your Airtable plan's monthly allowance when enabling it. A table exceeding 500 records causes the source to report an error and emit no events; the record-listing operation remains available for explicit pagination.

## Deployment and limits

Set `SOKEL_ENDPOINT` and `SOKEL_TOKEN`, then run the executable. Managed deployments can use the SDK's `SOKEL_DEPLOY_KEY` flow.

- `AIRTABLE_API_BASE_URL`: trusted deployment override; default `https://api.airtable.com/v0`. It is not an operation input or credential field.
- `AIRTABLE_POLL_SECONDS`: 60–86,400 seconds; default 300.
- Standard proxy environment variables configure outbound networking.

Requests are not automatically retried, including writes. HTTP 429 reports a retry delay, defaulting to Airtable's documented 30 seconds; the event source waits at least that long. Redirects are rejected and raw error bodies are not included in errors. Requests are limited to 1 MiB and responses to 4 MiB. After an ambiguous write failure, inspect the table before repeating it.

## Validation status

Local HTTP contract and polling tests cover the implementation. No real Airtable credentials were available during development; real PAT scopes, base permissions and live event delivery still require acceptance testing.

The internal checkpoint includes the next polling time, so credential-triggered source restarts preserve the configured interval. In SDK 0.7.7, event delivery and credential updates use fire-and-forget publishing: a successful call confirms local publication, not durable platform storage or downstream acknowledgment. Stable event IDs reduce duplicate processing, but this source does not promise exactly-once delivery. End-to-end delivery and checkpoint durability require deployment acceptance testing.
