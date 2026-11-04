# Intercom

Manage a focused set of contacts and conversations through Intercom's REST API. Requests explicitly use `Intercom-Version: 2.15`, a published supported API version; this plugin does not silently select the newest version. It supports US, EU and Australian regional hosting with a fixed origin for each region.

## Credentials and permissions

Create a private app for your Intercom workspace and obtain an access token. Store it as **Access token** in Sokel. Grant the contact and conversation read/write permissions needed by the operations you intend to use; replying as an admin also depends on that admin's access. A token does not bypass workspace permissions or plan limits. Token renewal and rotation happen outside this plugin; there is no OAuth authorization or refresh flow.

Set **Workspace region** to `us`, `eu` or `au`, matching your workspace's hosting region. Blank means US. The plugin uses `api.intercom.io`, `api.eu.intercom.io` or `api.au.intercom.io` respectively. It rejects arbitrary URLs and does not follow redirects to discover another region. If authentication fails, confirm the token and region before retrying.

Run `health_check`. It performs the read-only `GET /me` request to verify the current admin and returns `ok` and `message`. Unusable credentials return `ok=false` without a handler error. A successful check confirms identity access, not all contact or conversation permissions. The operation does not send a message or change a contact.

## Operations

| Operation | Inputs and result |
| --- | --- |
| `health_check` | Read the current admin to check the token. |
| `list_contacts` | Optional `page`; returns contact IDs, roles, selected nullable profile fields and email subscription state, plus continuation fields. |
| `get_contact` | `contact_id`; returns the selected contact fields. |
| `create_contact` | A plain `email`, optional `role` (`user` by default, or `lead`) and optional `fields`. Returns the created contact. |
| `update_contact` | `contact_id` and a nonempty `fields` object. Returns the updated contact. |
| `search_contacts` | A plain `email` and optional `page`; performs an exact `email = value` query and returns contacts and continuation fields. It does not expose arbitrary search expressions. |
| `list_conversations` | Optional `page`; returns conversation ID, title, state, open/read flags and creation/update timestamps, plus continuation fields. |
| `get_conversation` | `conversation_id`; requests `display_as=plaintext` and returns conversation metadata, the opening-message body, selected conversation parts, the reported part count and a truncation flag. |
| `reply_conversation` | `conversation_id`, `admin_id`, `body` and optional `message_type` (`comment` by default, or `note`). Returns conversation metadata as the write receipt. |

Contact IDs are opaque ID strings, not emails or external IDs. Conversation and replying-admin IDs are positive decimal strings, preserving values larger than JavaScript's exact integer range. Obtain the authorized replying admin's ID from Intercom's admin API or your own configuration; it is not the contact ID. The health check reports status only and does not expose an admin ID output. Timestamps are Unix seconds.

## Contact fields and examples

The supported writable `fields` are `name`, `phone` and `unsubscribed_from_emails`. Omitting a field leaves it unspecified. Explicit `false` is sent unchanged; an empty name or phone is sent as an empty string. The plugin does not expose changes to a contact's email, role or external ID after creation.

Create a contact:

```json
{
  "email": "alex@example.com",
  "role": "user",
  "fields": {"name": "Alex Example", "unsubscribed_from_emails": false}
}
```

Update the email subscription flag without changing other fields:

```json
{"contact_id": "6630b37a0000000000000001", "fields": {"unsubscribed_from_emails": false}}
```

The subscription flag controls Intercom's email subscription state. Changing it is not a substitute for obtaining the appropriate consent in your own workflow. Nullable profile fields may be omitted from outputs; null is not replaced with an invented value.

## Conversation replies and history

`comment` sends a reply visible to the conversation participant; `note` adds an internal note. Check the destination and admin before invoking either operation. The body follows Intercom's formatting rules and is limited to 64 KiB by this plugin. Attachments and replies on behalf of a contact are not supported.

```json
{
  "conversation_id": "123456789",
  "admin_id": "12345",
  "message_type": "note",
  "body": "The support team is reviewing this request."
}
```

`get_conversation` exposes the opening message separately as `source.body`; a source or part body can be absent. The API returns at most 500 conversation parts. `parts_may_be_truncated` is true when 500 parts are returned or fewer parts are returned than the reported `parts_total`. A false flag describes the returned response; it is not a guarantee of a complete historical archive. This release does not page older parts, retrieve attachments or provide an export endpoint. `list_conversations` returns metadata rather than message history.

## Cursor pagination

List and search operations accept `page.limit` (default 50, range 1–150) and optional `page.cursor`. Start without a cursor, then copy `next_cursor` exactly into `page.cursor` while `has_more` is true. Keep the same operation, region and search email. The plugin sends list cursors as the `starting_after` query parameter, and search cursors inside the JSON `pagination` object. It does not visit a URL from the response.

Each call retrieves one page. Missing or repeated continuation metadata causes an error instead of a silent partial result. Intercom pagination is stateless; changes during traversal can affect ordering and membership. Deduplicate by resource ID and do not treat a cursor as an exactly-once checkpoint or a snapshot token.

## Limits, failures and scope

Names and phone strings are limited to 1024 bytes, contact IDs to 128 characters, requests to 1 MiB and responses to 8 MiB. Calls have a 45-second transport timeout within the operation deadline. Intercom's API rate limits, account permissions and plan limits still apply. Reduce page size if a response exceeds the size limit.

HTTP errors, Intercom error envelopes, missing identity fields and invalid write receipts fail clearly. Upstream response bodies and credentials are excluded from error messages. Requests reject redirects, use only the selected official region and are never automatically retried. If a create or reply times out, inspect Intercom before repeating it; the write may already have succeeded.

This release does not delete contacts, close/reopen/assign conversations, manage tickets, send outbound campaigns, register webhooks, expose custom attributes or provide arbitrary search filters. Tests use synthetic fixtures adapted from official v2.15 examples, including null, false, large IDs, cursor and truncation variants. No live account acceptance or real reply was performed.

## Official references

- [Authentication, regions and REST API](https://developers.intercom.com/docs/build-an-integration/learn-more/rest-apis)
- [Pagination](https://developers.intercom.com/docs/build-an-integration/learn-more/rest-apis/pagination)
- [Current admin](https://developers.intercom.com/docs/references/2.15/rest-api/api.intercom.io/admins/identifyadmin)
- [List contacts](https://developers.intercom.com/docs/references/2.15/rest-api/api.intercom.io/contacts/listcontacts)
- [Create contact](https://developers.intercom.com/docs/references/2.15/rest-api/api.intercom.io/contacts/createcontact)
- [Update contact](https://developers.intercom.com/docs/references/2.15/rest-api/api.intercom.io/contacts/updatecontact)
- [Search contacts](https://developers.intercom.com/docs/references/2.15/rest-api/api.intercom.io/contacts/searchcontacts)
- [List conversations](https://developers.intercom.com/docs/references/2.15/rest-api/api.intercom.io/conversations/listconversations)
- [Retrieve conversation](https://developers.intercom.com/docs/references/2.15/rest-api/api.intercom.io/conversations/retrieveconversation)
- [Reply to conversation](https://developers.intercom.com/docs/references/2.15/rest-api/api.intercom.io/conversations/replyconversation)
