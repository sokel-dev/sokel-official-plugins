# Mailchimp

Use the Marketing API to read audiences, members, member tags, and campaigns, and to explicitly add or update a member. This plugin has eight operations. It does not create, schedule, or send campaigns, and has no automatic event source.

## Credentials

Create a Mailchimp Marketing API key in your account and save the complete value, including its data-center suffix, in API key. For a key ending in `-us6`, the plugin sends requests only to `https://us6.api.mailchimp.com/3.0`. Supported suffixes are `us` followed by a positive integer of up to four digits. The endpoint is derived from the key, not supplied by a workflow. OAuth tokens and Mailchimp Transactional/Mandrill keys are not supported.

The API key inherits its owner's account access and role. Check credentials makes a read-only authenticated `GET /ping`; success does not prove permission to update a specific audience. Rotate or replace keys outside this plugin.

## Operations

| Operation | Purpose |
|---|---|
| `health_check` | Authenticated ping; returns `ok` and `message`. |
| `list_audiences` | List audiences with their ID, name, creation time, and double-opt-in setting. |
| `get_audience` | Read one audience by `list_id`. |
| `list_members` | Read one audience's members, optionally filtering by status, `since_last_changed`, and `options.vip_only`. |
| `get_member` | Read one member using audience ID and plain email address. |
| `upsert_member` | Explicitly create or update the member at that email address. |
| `list_member_tags` | Page through all of a member's tags. Numeric tag IDs are returned as exact decimal strings. |
| `list_campaigns` | Read campaign identity, state, title, subject, audience, and timestamps; optionally filter by audience or campaign status. |

Member addresses are validated as plain addresses, without display names. The member URL uses the MD5 hash of the lowercase email address. The input address itself is not used as a URL path or query parameter. The upsert body includes it as required by Mailchimp.

## Explicit subscription choices

`upsert_member` requires `status_if_new` on every call. Choose `subscribed`, `unsubscribed`, `cleaned`, `pending`, or `transactional` deliberately. `subscribed` asserts that you have the appropriate consent; `pending` requests double opt-in and can cause Mailchimp to send a confirmation email. The plugin does not infer consent from list membership, tags, or an existing CRM record.

For an existing member, the plugin never sends the `status` field, so it does not resubscribe an unsubscribed member as a side effect of a data update. `status_if_new` applies only when Mailchimp creates the member. Existing account automations may react to contact changes; this plugin does not disable or control those automations.

Optional `merge_fields` maps the audience's merge tags to values. Use tags configured for that audience; required merge-field rules remain enforced by Mailchimp. Omitted tags are not changed. Numeric input merge values must stay within the exact JSON integer range (absolute value at most 9007199254740991); use a text merge field for larger identifiers. Nesting is limited to 16 levels. `options.vip` is optional: omit it to leave VIP unchanged, or pass `false` to clear it explicitly. There is no email-address change, archive/delete, tag-write, or existing-subscription-status update operation in this version.

Example input, for review only; invoking this operation writes a contact:

```json
{
  "list_id": "your_audience_id",
  "email_address": "person@example.com",
  "status_if_new": "transactional",
  "merge_fields": {"FNAME": "Alex"},
  "options": {"vip": false}
}
```

## Pagination and synchronization

Lists accept `page.limit` from 1 to 1000, default 50, and `page.offset` from 0 to 10000000, default 0. Explicit zero for the offset is preserved; zero is not a valid page size. Read `has_more` and use `next_offset` for the next request with the same filters. The next offset advances by the actual returned item count. Missing totals, impossible counts, or an empty page before the reported total fail explicitly.

Audiences are sorted by creation time ascending, campaigns by creation time ascending, and members by last-changed time ascending. Tags use the provider's ordering. These are ordinary offset pages, not a consistent snapshot. Inserts, deletes, updates, and ties can cause repeated or missed items across pages. For scheduled synchronization, use overlapping RFC 3339 `since_last_changed` windows and deduplicate by audience/member ID; retain checkpoints only after processing succeeds. The plugin does not implement durable polling, delivery guarantees, or background synchronization.

`options.vip_only=false` is sent explicitly and returns all members; omitting it leaves the provider default. Merge-field outputs use account-defined JSON and preserve JSON integer precision when decoded. Empty collections are returned as arrays. Campaign reads do not imply a message was delivered to a recipient.

## Errors and limits

Requests have a 45-second HTTP timeout within a 60-second operation timeout, a 1 MiB request limit, and a 4 MiB response limit. Reduce page size if a response is too large. Redirects are rejected, and response `_links` are never followed. API keys, arbitrary upstream error bodies, and response link URLs are not included in error messages.

The plugin performs no application-level automatic retries. Writes have no replayable request body. On 429, wait according to the sanitized numeric Retry-After hint when present. After a timeout or malformed write receipt, read the member first: the upstream write may already have succeeded. Do not interpret a failed response as proof that nothing changed.

This version was verified with official documentation examples and separately labeled synthetic HTTP fixtures. No real account was accessed, no contact was changed, and no email was sent during development. Live acceptance must be performed separately with the intended account and audience.

## Official references

- [Authentication and data centers](https://mailchimp.com/developer/marketing/docs/fundamentals/)
- [Authenticated ping example](https://mailchimp.com/developer/marketing/build/start-developing/send-your-first-api-request)
- [List audiences](https://mailchimp.com/developer/marketing/api/lists/list) and [get an audience](https://mailchimp.com/developer/marketing/api/lists/get)
- [List members](https://mailchimp.com/developer/marketing/api/lists/members/list-members) and [upsert a member](https://mailchimp.com/developer/marketing/api/lists/members/upsert-member)
- [Import/export contacts and subscriber hashes](https://mailchimp.com/developer/marketing/build/quickstarts/import-and-export-contacts)
- [List member tags](https://mailchimp.com/developer/marketing/api/lists/list-member-tags) and [tag examples](https://mailchimp.com/developer/marketing/build/quickstarts/organize-contacts-with-tags)
- [List campaigns](https://mailchimp.com/developer/marketing/api/campaigns/list)
