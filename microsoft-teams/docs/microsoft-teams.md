# Microsoft Teams

Browse teams, channels and existing chats; read messages and replies; send channel messages, channel replies and chat messages through Microsoft Graph v1.0.

## Delegated credential and tenant consent

Use an application registered in Microsoft Entra ID and your own approved delegated authorization flow to obtain a global Microsoft Graph access token. Save the raw token in `access_token`, without a `Bearer ` prefix. This plugin has no OAuth provider or automatic token refresh. Replace the token externally when it expires. Never use a client secret, ID token or refresh token in this field.

A signed-in work or school user is required. Personal Microsoft accounts and app-only tokens are not supported. Every operation first calls readonly `GET /me?$select=id` with `User.Read`; an app-only token fails before a message write is attempted. This plugin does not implement the application-only migration/import API. Sending acts as the delegated user, who must have access to the target conversation.

| Operation | Additional delegated scope |
| --- | --- |
| `health_check` | None beyond `User.Read` |
| `list_teams` | `Team.ReadBasic.All` |
| `list_channels` | `Channel.ReadBasic.All` |
| `list_chats` | `Chat.ReadBasic` (or `Chat.Read`) |
| `list_channel_messages`, `list_replies` | `ChannelMessage.Read.All` |
| `list_chat_messages` | `Chat.Read` |
| `send_channel_message`, including a reply | `ChannelMessage.Send` |
| `send_chat_message` | `ChatMessage.Send` |

Channel-message reads require administrator consent for `ChannelMessage.Read.All`. Tenant policies may also require administrator approval for other requested scopes, application consent, membership or Conditional Access. Consent does not grant membership in every team/channel/chat. Request only the operations you need. The identity health check returns `ok` and a sanitized `message`; success does not prove each Teams scope or conversation permission.

## Discover and read

`list_teams` returns directly joined teams with ID, name and description. Shared-channel host teams that the user has not directly joined are not included. `list_channels` returns visible channels owned by the selected team; this is not the allChannels/associated-teams API, and incoming shared channels can be absent. Private/shared channel visibility follows the user's membership.

`list_chats` returns chats the user already participates in, with ID, topic, type, URL and timestamps. One-to-one chats may have an empty topic. This plugin does not create chats or resolve display names to IDs. Use the IDs from the list operations.

`list_channel_messages` reads root channel messages. Replies are separate: call `list_replies` with the team, channel and root message IDs. `list_chat_messages` reads an existing chat. Outputs project message ID, body and content type, author ID/name when supplied, created/updated/deleted timestamps, parent message ID, web URL and message type. System or deleted messages can have empty author/body fields. HTML is returned as content and is not rendered or sanitized by this plugin.

Attachments, mentions, reactions, hosted content, membership expansion and complete raw message JSON are not returned. Channel order follows Graph's reply-chain modification order; chat message order follows Graph defaults. This version provides no custom filter or ordering.

## Send messages and replies

`send_channel_message` requires `team_id`, `channel_id` and `content`. Set `reply_to_id` to send a reply under a root channel message; omit it for a new root message. `send_chat_message` requires an existing `chat_id` and `content`.

```json
{"team_id":"<team-id>","channel_id":"<channel-id>","reply_to_id":"<message-id>","content":"The analysis is ready.","content_type":"text"}
```

`content_type` accepts `text` (default) or `html`; the Teams client restricts HTML and does not support arbitrary styling. No Markdown conversion, mentions, attachments or adaptive cards are included. The plugin accepts at most 64 KiB of content; Microsoft may impose a lower effective limit based on encoding and conversation policies.

A successful send requires HTTP 201 and a returned message ID. There is no automatic retry or idempotency key. After an ambiguous timeout, inspect the conversation before retrying to avoid duplicates. Send messages intended for people to read; this API is not intended for log ingestion or bulk migration.

## Pagination and runtime limits

Chat and message list page sizes are 1–50 (default 50). Joined-team and channel lists use service-controlled page sizes because those endpoints do not support `$top`. All list operations return `next_link`. Pass it unchanged back to the same operation with unchanged resource IDs until empty. The plugin preserves the entire opaque query and validates HTTPS, exact `graph.microsoft.com` origin, API version and resource path. Cross-user, cross-channel, cross-chat and external links are rejected.

Delegated user identity is checked on every invocation. User-scoped lists use the verified `/users/{id}` path so pagination is bound to the same identity. Other operations keep the exact team/channel/chat path. No national/sovereign cloud endpoint or user-supplied base URL is accepted.

Operations have a 60-second platform timeout and 55-second timeout per HTTP request. JSON requests are limited to 4 MiB and responses to 16 MiB. Redirects are rejected and errors exclude upstream response bodies, URLs and credentials. Rate limits require waiting before a later attempt. This version has no automatic triggers, subscriptions, edits or deletes. Tests use synthetic official API shapes; no live-account acceptance or real message sending has occurred.

References: [Joined teams](https://learn.microsoft.com/en-us/graph/api/user-list-joinedteams?view=graph-rest-1.0), [channels](https://learn.microsoft.com/en-us/graph/api/channel-list?view=graph-rest-1.0), [chats](https://learn.microsoft.com/en-us/graph/api/chat-list?view=graph-rest-1.0), [channel messages](https://learn.microsoft.com/en-us/graph/api/channel-list-messages?view=graph-rest-1.0), [replies](https://learn.microsoft.com/en-us/graph/api/chatmessage-list-replies?view=graph-rest-1.0), [send to a channel](https://learn.microsoft.com/en-us/graph/api/channel-post-messages?view=graph-rest-1.0), [send to a chat](https://learn.microsoft.com/en-us/graph/api/chat-post-messages?view=graph-rest-1.0), [permissions](https://learn.microsoft.com/en-us/graph/permissions-reference).
