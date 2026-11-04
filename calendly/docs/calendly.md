# Calendly

Discover event types, inspect scheduled events and invitees, query available times and create single-use scheduling links. This plugin uses the Calendly v2 API with a manually supplied personal access token. It does not book or cancel meetings.

## Credentials and permissions

Generate a personal access token in your Calendly account and save its raw value as **Access token**. Refresh or replace it externally when needed; this plugin has no OAuth provider, authorization flow or token storage beyond the Sokel credential.

| Operations | Required scope |
| --- | --- |
| `health_check`, `get_current_user` | `users:read` |
| `list_event_types`, `get_event_type` | `event_types:read` |
| `list_events`, `get_event`, `list_invitees` | `scheduled_events:read` |
| `list_available_times` | `availability:read` |
| `create_scheduling_link` | `scheduling_links:write` |

Your account role, organization permissions, subscription and resource access still apply. Organization-wide scheduled-event reads generally require an organization owner/admin; a regular member should use their own user URI. The plugin does not elevate account permissions.

The origin is fixed to `https://api.calendly.com`. No base URL override is exposed. Credentials travel only in the Authorization header; redirects are rejected. **Check credentials** performs the read-only `/users/me` request. Invalid credentials or an unusable identity return `ok=false`; success does not guarantee the scopes or resource permissions needed by every operation.

## Discovery and pagination

Start with `get_current_user` to obtain the user and current organization URIs. `list_event_types` and `list_events` require exactly one of `user_uri` or `organization_uri`. URIs must identify the correct resource on the fixed API origin and cannot contain query strings, fragments or encoded path escapes.

`get_event_type`, `get_event`, `list_invitees`, `list_available_times` and `create_scheduling_link` take the final resource ID segment, not the entire URI. IDs may contain letters, digits, underscores and hyphens. Discovery outputs preserve the complete canonical URIs.

List operations read one page. `page.count` accepts 1–100 (default 100). Pass the returned `next_page_token` as `page.token` with the same resource/filter parameters. An empty next token means no continuation was supplied. The plugin uses the token as an encoded query value on the original endpoint; it never fetches the returned `next_page` URL. A next-page URL without a usable token or a repeated token fails explicitly. Concurrent changes can affect paging; there is no snapshot, automatic loop or stored checkpoint.

`list_event_types.filters.active` preserves an explicit false to list inactive event types. `list_events.filters` supports `status` (`active` or `canceled`), `min_start_time`, `max_start_time` and `invitee_email`. Timestamps are normalized to UTC, and the upper bound must be later than the lower bound. `list_invitees.status` accepts `active` or `canceled`.

## Returned records

Event types include identity, name, active flag, duration, kind, scheduling URL and an available plain-text description. Scheduled events include status, start/end times, event type URI and a dynamic location object. Location fields vary between physical, phone and conferencing meetings.

Invitees include identity, event URI, name, email, status, timezone, creation time, reschedule flag and typed question/answer pairs. Cancellation/reschedule action URLs, payment details, tracking fields and the entire upstream response are not exposed. Treat returned invitee details as account data and share them only where appropriate for the workflow.

## Availability and single-use links

`list_available_times` requires an event type ID and RFC3339 start/end timestamps. The start must be in the future and the requested window must be greater than zero and at most **31 days**, inclusive. This matches the current official API; older examples may mention seven days. Each result includes the slot time, remaining invitee count and scheduling URL. This endpoint has no keyset pagination. Results describe current availability and do not reserve a slot.

`create_scheduling_link` sends `owner_type=EventType` and `max_event_count=1`; it returns the booking URL for the requested event type. A link creation does not schedule an event or send an invitation. Creating a link requires HTTP 201 and a valid owner/URL receipt. This write has no plugin idempotency key or automatic retry; an ambiguous timeout may have created a link already.

## Limits and validation

HTTP requests time out after 45 seconds; operation timeouts are 60 seconds, except health at 30 seconds. JSON request bodies are limited to 1 MiB and responses to 8 MiB. A 429 reports `Retry-After` or a 60-second fallback. No request retries automatically. Errors exclude upstream response bodies, tokens and transport URLs; missing collections and invalid business receipts fail explicitly.

Tests use synthetic official API shapes, not live captures. They check serialized requests, scopes/resource paths, optional false values, page-token handling, 31-day bounds, invalid receipts, redirects, size limits and cancellation/timeouts. No real meeting booking or live account write has occurred.

## Official references

- [Current user](https://developer.calendly.com/api-docs/calendly-api/users/get-current-user)
- [Event types](https://developer.calendly.com/api-docs/calendly-api/event-types/list-event-types)
- [Scheduled events](https://developer.calendly.com/api-docs/calendly-api/scheduled-events/list-scheduled-events)
- [Event invitees](https://developer.calendly.com/api-docs/calendly-api/scheduled-events/list-event-invitees)
- [Available times](https://developer.calendly.com/api-docs/calendly-api/event-types/list-event-type-available-times)
- [Single-use links](https://developer.calendly.com/api-docs/calendly-api/scheduling-links/create-scheduling-link)
