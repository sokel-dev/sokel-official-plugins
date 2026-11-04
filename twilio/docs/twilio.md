# Twilio

Send SMS/MMS, inspect message history and delivery state, and discover the account's phone numbers and message media metadata.

## Credentials

Set **Account SID** to the target Twilio account or subaccount (`AC` plus 32 hexadecimal characters), and **Auth token** to that account's Auth Token. This version uses HTTP Basic authentication against the fixed US1 endpoint `https://api.twilio.com/2010-04-01`. It does not accept API-key SIDs, OAuth tokens, custom endpoints or other Twilio regions. Account responses exclude the upstream `auth_token` field.

## Operations

| Operation | Purpose |
| --- | --- |
| `get_account` | Read account identity and status; can be used as an explicit credential check without sending a message. |
| `send_message` | Send one SMS/MMS and return its message SID and current status. |
| `get_message` | Read a message, delivery state, error code and price when available. |
| `list_messages` | Read one page of sent/received messages, optionally filtered by sender, recipient or UTC sent date. |
| `list_incoming_numbers` | Read one page of phone numbers belonging to the account. |
| `get_incoming_number` | Read one account phone number and its SMS/MMS/voice capabilities. |
| `list_message_media` | Read one page of media metadata for a message; no binary download. |

## Sending and delivery

Set `message.to` to an E.164 phone number such as `+15558675310`. Supply `message.from` or `message.messaging_service_sid`; both may be provided to select a specific sender from a Messaging Service. The sender must belong to the account. Supported sender forms are E.164 numbers, short codes and alphanumeric IDs where allowed. This operation targets SMS/MMS, not WhatsApp or voice calls.

Provide a nonempty `message.body` (up to 1600 Unicode characters) or one to ten HTTP/HTTPS `message.media_urls`. Twilio retrieves these media URLs; this plugin does not. Media size, supported formats and carrier delivery rules are enforced by Twilio. Optional `smart_encoded=false` is preserved, and `validity_period` accepts 1–36000 seconds in the outbound queue. A message may be split into multiple billable SMS segments.

Sending can incur charges and requires an eligible sender and destination under your Twilio account. Trial accounts and destination-specific registration may restrict sending. An accepted or queued message is not proof of delivery: retain the returned SID and query `get_message`. A network timeout may occur after Twilio accepts a send; the connector does not retry automatically or claim idempotency. Inspect message history before manually retrying. No test sends occur on startup or while reading account metadata.

## Pagination and filters

Lists read one page, default 50 entries and maximum 1000. When `has_more` is true, pass `next_page_uri` into the next call's `page.next_page_uri`, keeping the same operation and message SID where applicable. Do not combine continuation with new filters or a page size. The continuation preserves the provider's opaque token and query values, and is accepted only for the same account and collection. Stop when `has_more` is false.

For the initial message-history request, optional `filters.to` and `filters.from` filter addresses. Use `filters.date_sent` for an exact UTC date, or `sent_after` / `sent_before` for the provider's strict date comparisons. Dates use `YYYY-MM-DD`; exact dates and ranges cannot be combined. Price and count fields returned by Twilio as strings remain strings, preserving decimal prices. Nullable timestamps, prices and error fields remain optional.

## Limits and errors

Responses are bounded to 8 MiB; form request payloads to 1 MiB. Redirects are rejected, and Auth Tokens and raw provider error bodies are omitted from errors. HTTP 429 errors include a suggested wait time. The plugin does not buy/delete phone numbers, change webhooks, make calls, download media or provide message-event triggers.

## Verification

Tests use Twilio's published OpenAPI response examples plus clearly identified synthetic edge cases, capturing form encoding, repeated media fields, pagination and authorization. No live Twilio account or actual SMS/MMS delivery has been verified in this development batch. Explicitly read account metadata, then send to a recipient you control when you are ready for live acceptance.
