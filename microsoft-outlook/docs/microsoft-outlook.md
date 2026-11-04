# Microsoft Outlook

Read and send mail, download file attachments and work with calendar occurrences through Microsoft Graph v1.0.

## Credentials and permissions

Create an application in Microsoft Entra ID, obtain a **delegated Microsoft Graph access token** through your own approved authorization flow, and save the raw token in `access_token`. Do not include the `Bearer ` prefix. The token must target the global Microsoft Graph service. This plugin does not implement OAuth authorization or token refresh; replace the token before expiration. Do not paste an ID token, app-only token, client secret or refresh token.

All operations first perform `GET /me?$select=id` using `User.Read`. This verifies delegated identity and binds mailbox requests to that returned user ID. The plugin does not accept another mailbox owner. `health_check` makes only this readonly identity request and returns `ok` and a sanitized `message`. It does not verify mailbox licensing or every service permission.

| Capability | Delegated scopes |
| --- | --- |
| Identity check, required by all operations | `User.Read` |
| Mail list/get and attachment list/download | `Mail.Read` |
| Send mail | `Mail.Send` |
| Calendar view with selected event details | `Calendars.Read` (or `Calendars.ReadWrite`) |
| Create calendar event | `Calendars.ReadWrite` |

Request only the scopes you use. The authorization app, tenant consent policy, Conditional Access and mailbox permissions still apply; an administrator may need to approve the application. Outlook data requires an accessible mailbox. Delegated personal Microsoft and work/school accounts are supported where Microsoft Graph supports that mailbox. Sovereign/national cloud endpoints are outside this version.

## Mail operations

- `list_messages` reads one page from the whole mailbox by default. Set `folder_id` to a Graph folder ID or a well-known name such as `inbox` or `sentitems`. `filter` is a Graph OData mail `$filter` expression, for example `isRead eq false`; full-text `$search` is not included. There is no custom ordering in this version.
- `get_message` reads a message by ID. Outputs include subject, sender and To addresses, received time, body and its content type, body preview, read/attachment flags, web URL and Internet message ID. CC/BCC headers and arbitrary extended properties are not returned.
- `send_mail` sends a new message with `to`, `subject` and `body`; optional `cc`, `bcc`, and `body_type` (`text` or `html`, default `text`) are supported. Addresses must be plain email addresses; at least one To address and at most 100 total recipients are accepted. The message is saved to Sent Items by Graph's default. This version does not attach files, send drafts, reply or forward.

```json
{"to":["recipient@example.com"],"subject":"Research summary","body":"The summary is ready.","body_type":"text"}
```

A send result with `accepted: true` means Graph returned HTTP 202. Processing and delivery are asynchronous; this is not a delivery receipt and no sent-message ID is returned. Sending is an external action. Do not blindly retry after a timeout: inspect Sent Items and the destination first.

Mail requests use `Prefer: IdType="ImmutableId"`. Obtain IDs from this plugin rather than mixing ID formats from other clients. Message bodies can contain HTML; downstream rendering must treat it as untrusted content.

## Attachments

`list_attachments` returns metadata, including attachment type, name, MIME type, size and inline flag, for a message. `download_attachment` accepts a message ID and attachment ID, verifies file-attachment metadata, retrieves raw `/$value` bytes and returns a Sokel file. Binary bytes are not decoded as text. File names have path separators removed.

Only `#microsoft.graph.fileAttachment` downloads are supported. Embedded Outlook items and reference/link attachments are listed but cannot be downloaded here. The maximum download is 16 MiB; larger files and redirect-based downloads are rejected. The `has_attachments` message flag may exclude inline-only attachments, so list attachments when inline content matters.

## Calendars and time

`calendar_view` expands occurrences, exceptions and single events in the supplied `start`/`end` range. `calendar_id` is optional and defaults to the user's calendar. Inputs must be RFC3339 timestamps with explicit offsets, and end must be later than start. The plugin converts query times to UTC and requests UTC response times. Event outputs include ID, subject, body preview, start/end with timezone, location, URL, cancellation and all-day flags. It does not return recurrence rules or attendee lists.

`create_event` creates a single timed event with subject, start and end, plus optional calendar, body, body type, location, required attendee addresses and `transaction_id`. Times are normalized to UTC in Graph's `dateTimeTimeZone` shape. The transaction ID is an optional stable client value for Graph duplicate-request handling; the plugin still does not automatically retry. Recurrence, all-day creation, event update/delete and free/busy are outside this version.

```json
{"subject":"Review","start":"2026-11-03T10:00:00+08:00","end":"2026-11-03T11:00:00+08:00","attendees":["colleague@example.com"],"transaction_id":"review-20261103"}
```

Including attendees sends meeting invitations. Omit attendees for a personal appointment. At most 100 attendees are accepted.

## Pagination and limits

Mail and calendar lists accept `limit` 1–50 (default 50). Attachment pages use the service's page size. Return `next_link` unchanged in the same operation and keep resource inputs unchanged until it is empty. The next URL retains its complete opaque query; initial filter/range/page-size inputs are not reapplied to that URL. Every next link must have the exact global Graph origin and same canonical resource path. Links to another user, folder, API version or origin are rejected. Each operation performs a fresh delegated-identity check, including subsequent pages.

The platform operation timeout is 60 seconds and each HTTP request has a 55-second timeout. JSON requests are limited to 4 MiB; responses and downloaded files to 16 MiB. Redirects are refused. Errors identify status categories without echoing tokens, URLs or upstream response bodies. There are no automatic retries or event triggers. No live-account acceptance or real email sending has been performed; tests use synthetic documented API shapes.

References: [Mail list](https://learn.microsoft.com/en-us/graph/api/user-list-messages?view=graph-rest-1.0), [sendMail](https://learn.microsoft.com/en-us/graph/api/user-sendmail?view=graph-rest-1.0), [attachments](https://learn.microsoft.com/en-us/graph/api/attachment-get?view=graph-rest-1.0), [calendar view](https://learn.microsoft.com/en-us/graph/api/calendar-list-calendarview?view=graph-rest-1.0), [create event](https://learn.microsoft.com/en-us/graph/api/user-post-events?view=graph-rest-1.0), [permissions](https://learn.microsoft.com/en-us/graph/permissions-reference).
