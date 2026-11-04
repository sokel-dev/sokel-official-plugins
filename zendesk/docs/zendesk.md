# Zendesk

Read and manage Zendesk Support tickets through the agent-facing Ticketing API v2.

## Credentials and access

Set `subdomain` to the account name before `.zendesk.com`, without a URL or suffix. Set `email` to an agent or administrator's email, and `api_token` to an API token created in Zendesk Admin Center. API token access must be enabled for the account. Authentication uses Basic auth with `email/token` as the username and the API token as the password.

Tokens act with the supplied user's permissions, role and ticket visibility. This connector does not implement OAuth, token refresh, password authentication, end-user Requests API, custom proxy domains or Zendesk Help Center articles. `health_check` calls `/users/me.json` and returns `ok` plus a sanitized `message`; an end-user account is not considered suitable for these agent operations.

## Read tickets and comments

`list_tickets` reads one cursor page. `list_comments` additionally requires `ticket_id`. Both accept `limit` (1–100, default 50) and an optional `cursor`. Keep the ticket ID and operation unchanged and pass `next_cursor` while `has_more=true`. The connector uses opaque cursors and never follows provider pagination URLs. An empty final page is valid.

`get_ticket` reads one ticket. Ticket outputs include subject, description, status, priority, requester/assignee IDs, tags and timestamps. IDs are strings to preserve large values. Missing optional strings become empty strings. Comments expose body, author ID, public flag and creation time; attachments and HTML bodies are not returned.

`search_tickets` accepts Zendesk search syntax in `query` and a `page` number from 1 to 10 (default 1). The plugin adds `type:ticket`; do not add conflicting resource types. It requests 100 results per page. Continue with `next_page` while `has_more=true`. Search only exposes the first 1,000 matches. `total_count` is the provider's full match count and `truncated=true` means you must narrow the query to retrieve everything. Search indexing can lag new updates by several minutes.

## Write tickets and comments

`create_ticket` requires `subject`, initial comment `body`, an existing `requester_id`, and an explicit `public` boolean. `public=false` creates an internal note; `true` creates a public reply. Account triggers and notification rules still apply. Optional `details` supports status, priority, assignee ID and tags. The initial comment is sent as `ticket.comment`, not as the read-only description field.

`update_ticket` requires `ticket_id` and a nonempty `changes` object. Supported fields are subject, status, priority, assignee ID and tags. Omitted fields stay unchanged. Empty priority or assignee ID sends JSON null; empty tags replaces all tags with an empty list. Nonempty tags replaces the entire existing list. Status is limited to `new`, `open`, `pending`, `hold` or `solved`; closing a ticket and custom status IDs are outside this version.

`add_comment` requires ticket ID, body and explicit public flag. It updates `ticket.comment` using the Tickets API and returns the updated ticket, not a fabricated comment ID. Existing comments cannot be edited with this operation. Ticket and comment creation are not idempotent.

Both update and add-comment operations accept `updated_stamp`, the exact RFC3339 `updated_at` from a recent ticket read. When supplied, the connector sends `safe_update=true`; concurrent changes fail. Without it, the update is unconditional. Read the latest ticket before resolving a conflict.

## Limits and failures

Every operation has a 60-second platform timeout and a 55-second HTTP timeout. JSON requests/responses are limited to 8 MiB. Requests stay on the configured account's HTTPS `.zendesk.com` origin, reject redirects and never automatically retry writes. HTTP errors and invalid receipts fail without echoing sensitive upstream bodies. After an uncertain create or comment result, inspect the ticket before retrying to avoid duplicate records or messages. Pagination is not a transactional snapshot.

No live account writes were performed. Tests use synthetic protocol fixtures, not captured live traffic. This version has no triggers, webhooks, custom-field editing, uploads, bulk jobs or incremental export.

## Official references

- [Tickets API](https://developer.zendesk.com/api-reference/ticketing/tickets/tickets/)
- [Ticket comments](https://developer.zendesk.com/api-reference/ticketing/tickets/ticket_comments/)
- [Search and its 1,000-result limit](https://developer.zendesk.com/api-reference/ticketing/ticket-management/search/)
- [Cursor pagination](https://developer.zendesk.com/documentation/api-basics/pagination/paginating-through-lists-using-cursor-pagination/)
