# Google Sheets

Read spreadsheet metadata and cell values, update or append rows, batch-write ranges, and watch append-only rows.

## Operations

| Operation | Input and result |
|---|---|
| get_spreadsheet | spreadsheet_id → title, timezone and sheet/grid metadata |
| read_values | spreadsheet_id and A1 range → two-dimensional values; no page token |
| update_values | Write values into an explicit A1 range |
| append_values | Append after the logical table found in the range, using INSERT_ROWS |
| batch_update_values | 1–100 `{range, values}` entries per request |
| health_check | Check identity authorization |

The spreadsheet ID follows /spreadsheets/d/ in its URL. Quote sheet names with spaces, for example `'Sales 2026'!A1:D1000`. Cells accept strings, numbers, booleans and null; null skips a cell, while an empty string clears it. Writes support at most 10000 rows with 10000 cells per row and 16 MiB of JSON.

Writes default to RAW, so formula-looking text stays literal. Select USER_ENTERED explicitly to interpret formulas and dates. Reads default to FORMATTED_VALUE; UNFORMATTED_VALUE and FORMULA are also supported. Spreadsheet creation, formatting/charts and arbitrary spreadsheets.batchUpdate requests are not implemented; batch updates only change values.

## Appended row events

Set watch_spreadsheet_id and watch_range on the credential to poll every 60 seconds. A watched result may contain at most 10000 rows. Initial setup establishes a baseline without historical events. row_added.row_offset is one-based relative to the watched range, not the absolute worksheet row.

Polling is append-only. Every round verifies the old row digest. Edits, removals, reordering and inserted middle rows stop checkpoint advancement with an error. Inspect the data before clearing poll_cursor to establish a new baseline. Google omits empty trailing rows; filling an earlier blank row is not an append. Changing the watched target creates a new baseline without historical events. Choose a bounded business range.

The checkpoint is published only after all event publications succeed. Retries can replay events with stable IDs for platform deduplication. Each credential watches one range. OAuth uses the spreadsheets scope; Google resource sharing still controls access.

SDK v0.7.8 event and credential updates are fire-and-forget: a successful call only confirms local publish acceptance, not platform receipt or durable storage. Delivery is best-effort; disconnects may lose events or replay them. Stable event IDs support platform deduplication, but this is not an exactly-once guarantee. End-to-end delivery and persistence still need live platform acceptance. The managed next_poll_at field keeps source restarts from bypassing the 60-second interval; do not edit it.

## Setup and runtime

An administrator must register a Google OAuth app and its exact callback URL on the platform and enable the corresponding API in the same Google Cloud project. Create a plugin credential and click Authorize. The plugin only uses the short-lived access token injected by the platform. Client secrets, code exchange and refresh remain on the platform; the plugin writes no token files. The userinfo.email scope identifies the authorized account.

```sh
SOKEL_ENDPOINT=https://sokel.example.com SOKEL_TOKEN=skp_xxx ./google-sheets
```

The encrypted refresh_token field belongs to the platform authorization flow; do not fill it manually. Google API origins are fixed and HTTP redirects are refused. Request errors never include upstream response bodies, tokens or complete URLs. There are no automatic retries, including writes. A connection failure after a successful write is ambiguous: inspect the upstream before retrying.

health_check validates Google identity authorization only, not service enablement, resource access or write permission.

## Validation status

Tests use synthetic protocol examples based on official documentation to verify wire requests, pagination, failures and polling checkpoints. They are not live account captures. No live Google credentials were available, so OAuth consent, real resource operations and deployment acceptance remain unverified. Complete those checks with a test account before production use.
