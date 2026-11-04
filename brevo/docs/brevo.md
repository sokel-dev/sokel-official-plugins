# Brevo

Manage contacts and contact lists and explicitly send a transactional email through Brevo API v3. This plugin provides eight operations and uses an API key. There is no automatic source, scheduled sender, marketing-campaign sender, or OAuth provider declaration.

## Credentials and readiness

Create a Brevo API v3 key and save it in API key; an SMTP password is not an API key. Requests go only to `https://api.brevo.com/v3`, with the key in the `api-key` header. Workflows cannot override the API origin. Rotate keys outside this plugin.

`health_check` performs a read-only `GET /account` and returns `ok` and `message`. It discards the account's relay and marketing-automation data, including any embedded keys. A successful check proves account authentication, not sender approval, sufficient credits, recipient eligibility, or delivery. Configure your sending domain and sender in Brevo before explicitly invoking `send_email`.

## Operations

| Operation | Purpose |
|---|---|
| `health_check` | Verify account authentication without sending email. |
| `list_contacts` | List contacts, optionally modified after an RFC 3339 `modified_since` timestamp. |
| `get_contact` | Read one contact using its numeric `contact_id` represented as a string. |
| `create_contact` | Create an email contact with an explicit initial email permission choice; returns a contact ID. |
| `update_contact` | Change attributes, add existing-list memberships, or explicitly change email blocking. |
| `list_lists` | Page through contact lists. |
| `get_list` | Read a contact list by its numeric `list_id` string. |
| `send_email` | Explicitly submit one text or HTML transactional message to 1–99 recipients. |

Contact, folder, and list IDs are exact positive decimal strings within signed 64-bit range. Contact outputs include email/SMS block flags, list IDs, account-defined attributes, and creation/modification times. Contacts created through phone-only mechanisms may have no email address. List output uses `unique_subscribers` for the provider's `uniqueSubscribers`; it does not expose `totalSubscribers` or `totalBlacklisted`, which Brevo is deprecating to constant zero. Counts describe the provider response, not proven deliverable recipients.

## Contact changes

Create requires `email_status` equal to `blocked` or `allowed`. This maps explicitly to Brevo's `emailBlacklisted`; it is not inferred from an omitted checkbox. Creation does not enable update-on-conflict or forced merging. If the contact already exists, read it and deliberately choose an update.

Update accepts optional `options.email_blacklisted`. Omit it to preserve the current state; `false` explicitly allows email and `true` blocks it. `list_ids` adds memberships to existing lists; it does not replace all memberships or remove a contact from other lists. At most 150 list IDs are accepted, and duplicate IDs are rejected.

`attributes` uses existing uppercase account attribute names and values of the configured type. The plugin accepts strings, numbers, booleans, and arrays of strings, with up to 200 attributes. Numeric inputs are limited to the exact JSON integer range (absolute value at most 9007199254740991); use text attributes for larger identifiers. Brevo can ignore values that do not match the configured attribute type; an accepted update is not proof that every field changed. Read the contact back when verification matters. `EMAIL` is deliberately rejected: Brevo documents that changing a blocked contact's email can remove its block and resubscribe it. Email renaming, force merging, DOI creation, deletion, SMS sending, and list creation/removal are outside this version.

Contact changes may activate automations already configured in your Brevo account. The plugin itself sends a transactional email only when `send_email` is invoked and does not control those external automations.

## Explicit transactional email

Provide a plain `sender_email`, optional `sender_name`, a `to` array of objects containing `email` and optional `name`, a `subject`, `content_type` (`text` or `html`), and `body`. Exactly one body format is sent. Subject must be 1–998 bytes without line breaks; body must be 1–524288 bytes. Duplicate recipients and addresses containing display-name syntax are rejected. All recipients are placed in To and may see the other addresses; use separate calls for private delivery.

Example input, for review only; running this operation sends an email and can incur charges:

```json
{
  "sender_email": "sender@example.com",
  "sender_name": "Example service",
  "to": [{"email": "person@example.com", "name": "Alex"}],
  "subject": "Your requested update",
  "content_type": "text",
  "body": "Your request has been processed."
}
```

A valid HTTP 201 response with `messageId` becomes `accepted=true` and `message_id`. It confirms API acceptance, not delivery or reading. Use Brevo's logs to investigate delivery. Templates, template parameters, attachments, CC/BCC, message versions, scheduling, custom headers, and idempotency keys are not exposed. This plugin does not provide exactly-once sending.

## Pagination

Use nested `page.limit` and `page.offset`. Contacts accept 1–1000 items; lists accept 1–50. Both default to 50. Offset defaults to 0 and accepts up to 10000000. Omitted numeric inputs remain omitted until the plugin applies its documented defaults; explicit zero is preserved for offset and rejected for page size.

Lists are requested in ascending creation order. Continue only when `has_more=true`, using `next_offset` with the same filters. Short pages advance by their actual item count; inconsistent totals or an empty page before the reported total fail explicitly. Ordinary offset pagination is not a snapshot; concurrent changes can cause duplicates or omissions. A scheduled contact sync should use overlapping `modified_since` windows and deduplicate by contact ID. There is no built-in polling checkpoint or delivery guarantee.

## Failure behavior

HTTP timeout is 45 seconds within a 60-second operation timeout. Requests are limited to 1 MiB and responses to 4 MiB; reduce page size for oversized reads. Redirects are rejected. The plugin never follows response pagination URLs. Only fixed API paths and locally encoded parameters receive the key. Upstream error bodies and account secrets are not echoed in errors.

No application-level retries are performed, and write bodies are not replayable by the HTTP client. For 429, wait according to a sanitized numeric Retry-After hint if present; Brevo may also apply account-specific limits. A timeout or invalid receipt after a write may mean that the provider already accepted it. Inspect the contact or delivery logs before retrying, especially for email sends, which can be duplicated.

Validation used official published response examples and synthetic HTTP boundary cases. No real credentials were read, no real account was called, and no email was sent during development. Sender configuration and delivery still require separate live acceptance.

## Official references

- [Account details](https://developers.brevo.com/reference/get-account)
- [List contacts](https://developers.brevo.com/reference/get-contacts) and [get a contact](https://developers.brevo.com/reference/get-contact-info)
- [Create a contact](https://developers.brevo.com/reference/create-contact) and [update a contact](https://developers.brevo.com/reference/update-contact)
- [List contact lists](https://developers.brevo.com/reference/get-lists) and [get a list](https://developers.brevo.com/reference/get-list)
- [Send transactional email](https://developers.brevo.com/reference/send-transac-email) and [sending guide](https://developers.brevo.com/docs/send-a-transactional-email)
- [Pagination concepts](https://developers.brevo.com/docs/how-it-works) and [rate limits](https://developers.brevo.com/docs/api-limits)
