# Discord

Send messages to a channel. **Free, with no approval process**; a channel administrator can create a webhook in a few clicks.

> **Intended use**: distribution to a community. Only channel members can see the content.
> Use it to notify a research group when a report is ready; use Bluesky, Mastodon, or X for public exposure.

## Setup: one minute

1. Open the channel's **Settings → Integrations → Webhooks → New Webhook**, and select the channel;
2. Copy the `https://discord.com/api/webhooks/<id>/<token>` URL;
3. Create a platform credential and paste it into **Webhook URL**. **The URL itself is a credential**: anyone with it can send messages, so store it as a secret;
4. Click **Test**. The check verifies that the webhook still exists and **does not send a test message to the channel**.

## Operations

| Operation | Description |
|---|---|
| Send message | Text, an embed card, and attachments. Returns the message ID and link |
| Delete message | Deletes a message sent by this webhook |
| Check credentials | Checks whether the webhook still exists |

## Using cards

Financial notifications often contain a title, summary, link, and key figures. Plain text can overwhelm a channel and be hard to scan.
Providing a title adds an embed card:

- **Card title / Card link**: clicking the title opens the original article;
- **Card body**: up to 4,096 characters, compared with 2,000 for message text; put long summaries here;
- **Card fields**: key-value pairs, for example `{"Ticker":"600519","Rating":"Buy","Target price":"1800"}`, displayed in two columns. Keys are sorted so identical input always produces the same layout;
- **Accent color**: a hexadecimal color such as `#2b6cb0`; you can use different colors for Buy, Neutral, and Sell;
- **Footer**: commonly used for the source and time.

## Limits

| Item | Limit |
|---|---|
| Message text | 2,000 characters. The plugin rejects longer text before sending and suggests moving it to the card body |
| Card body | 4,096 characters |
| Rate | At most five messages every two seconds for the same webhook |

## Troubleshooting

**404**: a channel administrator deleted the webhook. Create another and update the credential.

**429**: messages are being sent too quickly. Combine messages or increase the interval.

**Posting to a thread in a forum channel**: enter its thread ID in **Send to thread**. Supply the same thread ID when deleting a message,
or Discord cannot find it.

**Cannot connect**: discord.com is hosted outside mainland China. Configure an outbound proxy in the **credential**;
for a container, use `http://host.docker.internal:7897`.
