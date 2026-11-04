# Google Calendar

List, read, create, update and delete events, query free/busy time, and poll calendar changes. An empty operation calendar_id means primary; the credential watch_calendar_id must be set explicitly.

## Operations

| Operation | Description |
|---|---|
| list_events | Filter by time_min/time_max/query, expand recurring events and sort by start; page_size 1–2500 and page_token for continuation |
| get_event | Read a calendar_id/event_id pair |
| create_event | summary/start/end with optional description, location, all-day flag, IANA timezone and attendee emails |
| update_event | PATCH a known event_id using a complete form for supported fields: empty description, location or attendees clear existing values; unsupported fields are preserved |
| delete_event | Delete a known event_id |
| free_busy | RFC3339 window and 1–50 calendar_ids; inspect each calendar's busy and errors; an error is not evidence of availability |
| health_check | Validate identity authorization |

Timed values must include an offset, for example 2026-11-03T09:00:00+08:00. An IANA time_zone such as Asia/Shanghai can also be specified. All-day events use dates; end is exclusive, so a one-day event ends on the following date. End must be after start.

send_updates defaults to none; all and externalOnly are available. Explicitly request invitations when needed. none can affect attendee calendar synchronization, and Google's own notification behavior still applies. Conference links, attachments, reminders, custom recurrence, ACLs and calendar creation are not implemented. For recurring events, determine whether event_id names an instance or its master before editing.

## Change events

Set watch_calendar_id to primary or a specific ID to poll every 60 seconds. Initial full synchronization stores its final sync token without emitting history. Incremental rounds keep consistent query parameters and drain every page before checkpointing. Cancelled/deleted events are included; fields other than ID/status can be empty. The typed event includes times, attendees, etag and update time.

Incremental polling does not expand recurring series and is not an event-start reminder. HTTP 410 reports an expired cursor and retains it. Clear poll_cursor explicitly to establish a new baseline; changes during the gap may be unrecoverable. Changing the watched calendar establishes a new baseline. A round supports at most 1000 pages. Event or checkpoint failures can replay events with stable deduplication IDs.

Scopes are calendar.events and calendar.freebusy; calendar sharing permissions still control writes.

SDK v0.7.8 event and credential updates are fire-and-forget: a successful call only confirms local publish acceptance, not platform receipt or durable storage. Delivery is best-effort; disconnects may lose events or replay them. Stable event IDs support platform deduplication, but this is not an exactly-once guarantee. End-to-end delivery and persistence still need live platform acceptance. The managed next_poll_at field keeps source restarts from bypassing the 60-second interval; do not edit it.

## Setup and runtime

An administrator must register a Google OAuth app and its exact callback URL on the platform and enable the corresponding API in the same Google Cloud project. Create a plugin credential and click Authorize. The plugin only uses the short-lived access token injected by the platform. Client secrets, code exchange and refresh remain on the platform; the plugin writes no token files. The userinfo.email scope identifies the authorized account.

```sh
SOKEL_ENDPOINT=https://sokel.example.com SOKEL_TOKEN=skp_xxx ./google-calendar
```

The encrypted refresh_token field belongs to the platform authorization flow; do not fill it manually. Google API origins are fixed and HTTP redirects are refused. Request errors never include upstream response bodies, tokens or complete URLs. There are no automatic retries, including writes. A connection failure after a successful write is ambiguous: inspect the upstream before retrying.

health_check validates Google identity authorization only, not service enablement, resource access or write permission.

## Validation status

Tests use synthetic protocol examples based on official documentation to verify wire requests, pagination, failures and polling checkpoints. They are not live account captures. No live Google credentials were available, so OAuth consent, real resource operations and deployment acceptance remain unverified. Complete those checks with a test account before production use.
