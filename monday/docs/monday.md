# Monday

Use this plugin to discover boards and their groups and columns, read items, create items, change column values and add updates. It calls Monday's official GraphQL endpoint with the explicit `API-Version: 2026-10` header. This release uses nested `boards { items_page }`, which is available in that version; it does not depend on the root-level `items_page` introduced in 2027-01.

## Credentials and access

Create a personal API token in Monday's developer settings and store it as **API token** in the Sokel credential form. The token inherits your account access: sharing a board with another user does not grant this token access. Renew or replace tokens outside this plugin. There is no OAuth authorization flow or automatic refresh.

`health_check` performs the read-only `me` query and returns `ok` and `message`. A failed credential check returns `ok=false`; it does not create or change anything. A successful check confirms user access, not write access to every board. If you supply an app token instead, its scopes and board permissions must allow the requested operation; the relevant scopes include `me:read`, `boards:read`, `boards:write` and `updates:write`.

## Operations

| Operation | Inputs and result |
| --- | --- |
| `health_check` | Check the current token without writing. |
| `list_boards` | Optional `page.number` (default 1, maximum 10000) and `page.limit` (default 100, 1–100). Returns board ID, name, state and URL, plus `next_page`. The upstream default listing targets classic boards; it is not an inventory of every board hierarchy type. |
| `get_board` | A `board_id`; returns the board plus group IDs/titles and column IDs/titles/types. Use these IDs for subsequent writes. |
| `list_items` | A `board_id`, optional `page.limit` (default 100, 1–500) and `page.cursor`. Returns items, `next_cursor` and `has_more`. |
| `get_item` | An `item_id`; returns the item, board/group references and column values. |
| `create_item` | `board_id` and `name`; optional `group_id`, `column_values_json` and `options.create_labels`. Returns the created item. |
| `change_columns` | `board_id`, `item_id` and a nonempty `column_values_json` object string; optional `options.create_labels`. Returns the updated item. |
| `add_update` | `item_id` and `body`; returns the update ID and body. The body follows Monday's update formatting rules. |

All numeric resource IDs are decimal **strings**, including IDs larger than JavaScript's exact integer range. Never substitute a board URL for an ID. Group and column IDs are different from their visible titles.

## Column values and examples

`column_values_json` is a JSON **string** whose content is an object keyed by column ID. The plugin passes that string in a GraphQL `JSON` variable, as required by Monday. It does not insert values into query text or guess column formats. Use the format documented for each column type. `get_board` exposes IDs and types, but not label definitions or column settings.

A create-item input might be:

```json
{
  "board_id": "1234567890",
  "name": "Prepare launch",
  "group_id": "topics",
  "column_values_json": "{\"numbers\":0,\"status\":{\"label\":\"Working on it\"}}",
  "options": {"create_labels": false}
}
```

The example column IDs and status label must exist on your board. Omitting `options.create_labels` leaves the upstream default; explicitly passing `false` is preserved. Enabling it may create labels and requires the relevant permissions. To change values, reuse the same string format with `board_id` and `item_id`. Column values returned as `value` remain JSON-encoded strings, and `text` may be absent when Monday returns null.

Calculated or rollup columns may reject changes or leave the value unchanged, including calculated parent columns on multilevel boards. A mutation receipt is not proof that every requested column acquired the intended value. Inspect the returned values and the board before retrying an uncertain write.

## Pagination and limits

Each operation fetches one page. For boards, continue with `next_page` until it is zero. A full page only indicates that another page may exist; the final request may return an empty page. For items, copy `next_cursor` exactly into `page.cursor` while `has_more` is true, keeping the same board. Omit the cursor on the first request. Cursors are opaque and expire upstream; restart the listing if Monday rejects one. Pagination is not a snapshot, so workflows should deduplicate by item ID when boards change during a scan.

The plugin accepts item names up to 1024 bytes, update bodies up to 64 KiB and column JSON up to 256 KiB. HTTP requests are capped at 1 MiB and responses at 8 MiB; reduce page size if a board contains large column values. Calls have a 45-second transport timeout within the operation deadline. Monday complexity, rate and account limits still apply.

## Errors and scope

HTTP failures, GraphQL `errors` (including partial-data responses), missing resources and malformed write receipts fail the operation. Upstream response bodies and credentials are not included in errors. Requests use the fixed official origin, reject redirects and are never automatically retried. After a timeout or failed write receipt, inspect Monday before repeating a create or update to avoid duplicates.

This release does not create boards or groups, upload files, delete/archive items, expose arbitrary GraphQL, subscribe to webhooks or offer a complete column editor. Only the selected typed fields are returned. Tests use synthetic fixtures adapted from official documentation, including large IDs, null fields, false values and error variants. No live account acceptance has been performed.

## Official references

- [API versioning](https://developer.monday.com/api-reference/docs/api-versioning)
- [Boards and groups/columns](https://developer.monday.com/api-reference/reference/boards)
- [Items](https://developer.monday.com/api-reference/reference/items)
- [Items page](https://developer.monday.com/api-reference/reference/items-page)
- [Changing column values](https://developer.monday.com/api-reference/docs/change-column-values)
- [Column types](https://developer.monday.com/api-reference/reference/columns)
- [Updates](https://developer.monday.com/api-reference/reference/updates)
- [Current user](https://developer.monday.com/api-reference/reference/me)
