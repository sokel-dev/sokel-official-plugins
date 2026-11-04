# Trello

Discover boards and lists, read cards, create and update cards, and add comments using Trello REST API v1. This version uses a manually supplied API key and user token.

## Credentials

Create a Power-Up API key in [Trello app administration](https://trello.com/apps/admin), then obtain a user token through Trello's external authorization flow. Grant `read` for discovery and reads; card and comment writes also require `write`. The user must have permission on the target board. Save the values as **API key** and **User token**. The plugin sends both in the officially supported OAuth Authorization header, never in URL query parameters. This header form does not require storing an application secret.

Tokens may expire or be revoked. Obtain and replace them outside this plugin; it has no OAuth provider or refresh flow. OAuth 2.0 bearer tokens are not accepted by this key/token credential form. The origin is fixed to `https://api.trello.com`, with no workflow or deployment URL override. All redirects are rejected.

**Check credentials** reads `/1/members/me` and validates the returned member identity. Failure returns `ok=false` with a safe message. Success does not prove write scope or access to every board.

## Operations

| Operation | Behavior |
| --- | --- |
| `health_check` | Verify the current member without writing. |
| `list_boards` | List boards visible to the member. |
| `get_board` | Read one board's name, description, archive flag and URL. |
| `list_lists` | Read the lists on a board. |
| `list_cards` | Read one page of cards on a board. |
| `get_card` | Read one card and its board/list IDs. |
| `create_card` | Create a named card in an existing list. |
| `update_card` | Change selected card fields, move it to a list, or archive/unarchive it. |
| `add_comment` | Add a text comment and return its action receipt. |

Use the full 24-character lowercase hexadecimal resource IDs returned by discovery. URLs and short links are not accepted as IDs. Board, list and card filters accept `open` (default), `closed` or `all`.

## Card pagination

`list_cards.page.limit` accepts 1–1,000 and defaults to 100. Pass a card ID as `page.before` to request older cards. The request sets `sort=-id` so the first page is ordered by descending card ID. Trello's cursor uses creation time, not card position or modification time. `oldest_card_id` is the smallest returned MongoDB-style card ID, independent of the order cards appear on the board. Use it as the next `before` value. `may_have_more=true` means the page reached the requested size; an exact full final page requires one additional empty read.

This is a creation-time scan, not a snapshot or change feed. Concurrent moves, archiving and creation can change results. Deduplicate IDs across calls, retain unchanged board/filter settings, and use an overlap or a fresh reconciliation for strict synchronization. Trello's documented timestamp cursor does not provide a transactional snapshot. No automatic loops, webhook creation or checkpoint storage are included. Board and list discovery use the upstream collection response without inventing page-number parameters.

## Card writes

`create_card` requires `list_id` and `name`; `description` and RFC3339 `due` are optional. Card names, descriptions and comments are limited to 16 KiB of UTF-8 text by this plugin.

`update_card.changes` accepts optional `name`, `description`, `list_id`, `due`, `due_complete` and `closed`. At least one change is required. Explicit `false` values are preserved; an empty `description` clears the description and an empty `due` sends JSON null to clear the due date. An empty name is rejected. Due timestamps are normalized to UTC. Moving across boards follows Trello permissions and list rules. No delete, attachment, checklist, label or member-assignment operation is included.

Writes require HTTP 200 and a valid resource/action receipt. No operation retries automatically. If a create, comment or update times out, or a receipt is invalid, inspect the target before retrying: the change may already have occurred. Comments and card creation have no plugin idempotency key.

## Limits and validation

HTTP requests time out after 45 seconds; normal operations have a 60-second platform timeout and health has 30 seconds. JSON request bodies are limited to 1 MiB and responses to 8 MiB. A 429 reports `Retry-After` or a 60-second fallback. Errors omit upstream bodies, credentials and transport URLs. Oversized or incomplete responses fail explicitly.

Tests use synthetic fixtures based on official API shapes. They check serialized headers, paths and bodies, false and null values, pagination, invalid receipts, redirects, timeout/cancellation and size limits. They are not live account captures. No live writes have been performed.

## Official references

- [Authorization header](https://developer.atlassian.com/cloud/trello/guides/rest-api/authorization/#authorization-header)
- [API introduction and paging](https://developer.atlassian.com/cloud/trello/guides/rest-api/api-introduction/#paging)
- [Boards API](https://developer.atlassian.com/cloud/trello/rest/api-group-boards/)
- [Cards API](https://developer.atlassian.com/cloud/trello/rest/api-group-cards/)
- [Members API](https://developer.atlassian.com/cloud/trello/rest/api-group-members/)
