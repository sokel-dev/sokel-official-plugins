# Slack

Connect a Slack workspace to Sokel with a manually supplied bot token. This plugin does not register an OAuth provider or perform an OAuth consent flow.

## Credential

Create a Slack app, add the scopes required by your operations, install it in your workspace, and copy its Bot User OAuth Token into `token`. Invite the bot to each channel it needs to access.

- Sending messages: `chat:write`.
- Reading messages and threads: the relevant `channels:history`, `groups:history`, `im:history` or `mpim:history` scope. Slack controls token eligibility and workspace permissions for each conversation.
- Uploading files: `files:write`.

Run `health_check` to verify the token. It returns `ok` and `message`; success also includes `team_id` and `user_id`. This check verifies identity, not permission to every channel or operation.

## Operations

| Operation | Behavior |
| --- | --- |
| `health_check` | Calls `auth.test`; reports whether the token is valid. |
| `slack_history` | Returns one page of up to 15 messages and `next_cursor` / `has_more`. |
| `slack_replies` | Returns one page of a thread, including its parent message. Supply the original `ts`. |
| `slack_send` | Sends text to `channel`; optional `thread_ts` posts a reply. Returns channel and message timestamp. |
| `slack_upload` | Uploads one platform file to a channel, optionally to a thread, and returns `file_id`. |

Use `next_cursor` unchanged for the next page and keep the same filters. Preserve timestamps as strings, including all six fractional digits. `oldest` and `latest` are exclusive boundaries. Optional message attributes can be empty when Slack omits them.

Message text and upload comments must contain no more than 40,000 characters. Files must contain 1 byte to 20 MiB and have a filename. File upload uses Slack's current three-stage external-upload API, not the retired `files.upload` method. Files are read into memory; run large concurrent uploads with an appropriate memory limit.

## New-message events

Set `watch_channel` to enable `message_received`. Leave it empty to disable polling. One credential watches one channel. The first successful poll records a baseline and does not emit history. Subsequent polls emit ordinary human messages; bot messages, thread replies, edits, deletions and message subtypes are excluded.

The source reads one page per interval and stores its fixed time window and pagination cursor in the credential's internal `_slack_poll_state`. Busy channels can develop a backlog. An event ID contains the channel and timestamp; retries after a checkpoint failure can replay an event, which Sokel deduplicates. Changing the watched channel establishes a new baseline. Keep one active source process per credential to avoid competing checkpoints.

This is a polling integration, not the Slack Events API. Messages deleted before polling are not observable, and Slack retention and permissions limit accessible history. Some app distribution types have stricter limits for history and replies; check the current Slack policy for your app.

## Deployment

Set `SOKEL_ENDPOINT` and `SOKEL_TOKEN`, then run the built executable. A managed deployment may use the SDK's `SOKEL_DEPLOY_KEY` registration flow.

- `SLACK_API_BASE_URL`: trusted deployment override; default `https://slack.com/api`. It is not a credential field or operation input.
- `SLACK_POLL_SECONDS`: polling interval, 60–86,400 seconds; default 60.
- Standard `HTTPS_PROXY` / `HTTP_PROXY` / `NO_PROXY` variables configure outbound networking.

API redirects are rejected. The file upload URL must have the exact `https://files.slack.com` origin; the bot token is never sent to the upload endpoint. HTTP 429 reports a safe retry delay and the source waits at least that long. Operations are not automatically retried, including writes. If a write times out or lacks a receipt, inspect Slack before repeating it.

## Validation status

The implementation has local HTTP contract and event-source tests. No real Slack credentials were available during development, so workspace authorization, actual uploads and live event delivery still require acceptance testing with your app.

The internal checkpoint includes the next polling time, so credential-triggered source restarts preserve the configured interval. In SDK 0.7.8, event delivery and credential updates use fire-and-forget publishing: a successful call confirms local publication, not durable platform storage or downstream acknowledgment. Stable event IDs reduce duplicate processing, but this source does not promise exactly-once delivery. End-to-end delivery and checkpoint durability require deployment acceptance testing.
