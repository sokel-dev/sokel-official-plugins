# Zoom

Read Zoom users and meetings, create scheduled meetings, and update meeting details.

## Credential and permissions

Create a Zoom user-managed OAuth app outside this plugin and paste a valid access token into **OAuth access token**. The plugin does not create an OAuth provider, exchange authorization codes, or refresh tokens. Replace the token externally when it expires. This release targets the global `https://api.zoom.us/v2` API and user-managed OAuth; it does not claim Server-to-Server OAuth compatibility for the `me` identity check.

`health_check` calls `GET /users/me`. It returns `ok` and `message`; success confirms user identity, not every operation's permissions. Reading users needs `user:read:user` (or its admin variant); account user listing needs `user:read:list_users:admin`. Meeting operations need `meeting:read:list_meetings`, `meeting:read:meeting`, `meeting:write:meeting`, and `meeting:update:meeting`, or their applicable `:admin` variants. Zoom also documents broader legacy `user:read`, `meeting:read`, and `meeting:write` scopes. Account roles, app approval and resource access still apply. Grant only scopes needed by your workflow.

## Operations

| Operation | Behavior |
| --- | --- |
| `health_check` | Read the current user's identity. |
| `get_user` | Read one user by ID or email; omitted `user_id` means `me`. |
| `list_users` | Return one account-user page; optional status is `active`, `inactive` or `pending`. |
| `list_meetings` | Return one host's unexpired scheduled-meeting page; optional type is `scheduled`, `live`, `upcoming`, `upcoming_meetings` or `previous_meetings`. |
| `get_meeting` | Read a meeting using its numeric meeting number. |
| `create_meeting` | Create a type-2 scheduled meeting from topic, start time and duration, with optional agenda and meeting settings. |
| `update_meeting` | PATCH only supplied changes; an HTTP 204 response returns `updated=true`. |

List operations accept `page_size` from 1 to 300 (omitted or 0 uses 30) and `next_page_token`. Pass the returned token into the next call with the same filters. Tokens expire after approximately 15 minutes. Each invocation sends one request; tokens are query values, never URLs to follow. `has_more=false` ends pagination. Instant meetings, historical instances, recurring meeting configuration, recordings, participants and meeting deletion are outside this release. Zoom limits some upcoming/previous list modes to six months.

## Creating and changing meetings

For example, set `topic` to `Weekly review`, `start_time` to `2030-01-01T10:00:00+08:00`, and `duration` to `30`. Start times require RFC 3339 with an offset and are normalized to UTC. Duration is 1–1440 minutes. Topics are limited to 200 characters and agendas to 2,000. Creation uses `timezone=UTC`; Zoom applies account defaults such as passcode requirements.

`details.settings` when creating, or `changes.settings` when updating, supports `host_video`, `participant_video`, `join_before_host`, `mute_upon_entry` and `waiting_room`. Omitted fields keep upstream defaults/current values; explicit `false` is sent. Update `agenda` with an empty string to clear it. Updates require at least one field. An updated start time must be in the future. Updates have no occurrence selector; on an existing recurring meeting, Zoom may apply changes to the series. Account policies may override settings; read the meeting or check Zoom after changing them. Meeting UUIDs are not accepted as numeric meeting IDs.

Outputs contain typed meeting summaries, including the attendee `join_url`. Treat that URL as private because it may contain a passcode. The plugin deliberately omits the privileged host `start_url` and does not expose host keys. Numeric meeting IDs are returned as strings without floating-point conversion.

## Limits and verification

Operations allow 60 seconds, with a 50-second HTTP timeout, 1 MiB request and 8 MiB response limits. Redirects are blocked. Errors do not echo token or upstream response bodies. No request is retried automatically, including writes. A timeout can leave the result uncertain; check Zoom before repeating a creation. Zoom quotas and account-plan restrictions apply; creation currently has a documented per-host daily limit.

Tests use synthetic, reduced fixtures adapted from official documentation; no real Zoom account has been tested and no meeting has been created during development. Before production, verify scopes, account policies, pagination and a controlled test meeting in your own account.

## Official references

- [Meeting endpoints](https://developers.zoom.us/docs/api/meetings/)
- [User endpoints](https://developers.zoom.us/docs/api/users/)

References were checked on 2026-11-04. Standalone Go runtime registration uploads the English guide via `SetDoc`, but does not upload locale dictionaries in SDK v0.7.7. The full Chinese translation is packaged in `locales/zh-CN.json` for registry installation; the separate Chinese guide is also readable in this repository.
