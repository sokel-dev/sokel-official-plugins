# Feishu Group Bot (webhook)

The quickest way to send messages to **one Feishu group**: no app creation or administrator assistance, with setup in 30 seconds.
It suits send-only uses such as alerts and daily reports. To receive messages as workflow triggers, message arbitrary users,
or write documents and Bitable data, use the main **Feishu** custom-app plugin.

## Setup: 30 seconds

1. Open the target group → Settings → **Group Bots** → Add Bot → **Custom Bot**.
2. Copy the **webhook URL**, in the form `https://open.feishu.cn/open-apis/bot/v2/hook/…`,
   into this plugin's credential.
3. Enable **signature verification** in the security settings and copy its secret into the credential as well.
   Without it, anyone who obtains the URL can post messages to your group.

One credential represents one bot in one group. Create a separate credential for each additional group.

## Usage

`webhook_send` accepts three content types. If multiple types are supplied, the precedence is Card > Markdown > Text:

- **Text**: optionally enable **@everyone**.
- **Markdown**: renders as a card and supports **bold**, [links](…), lists, code blocks, and an optional title.
  Use it for alerts and reports.
- **Card JSON**: build a card in the [Feishu Card Builder](https://open.feishu.cn/cardkit) and paste its JSON.
  Webhook-bot **button clicks do not return to the workflow**, because the bot cannot receive messages.
  Use the main plugin for interactions.

## Limits imposed by Feishu

- It can send only to **the group where the bot was added**;
- It cannot receive messages, send direct messages, or mention specific people; it can only mention everyone;
- The rate limit is 100 messages per minute; exceeding it returns 9499.

## Troubleshooting

| Error | Action |
|---|---|
| Signature verification failed (19021) | Signature verification is enabled on the bot, but the credential's secret is missing or incorrect |
| Rate limit reached (9499) | Reduce the sending rate to stay within 100 messages per minute |
| The URL does not look like a Feishu group-bot webhook | Copy the correct webhook URL again from the group settings |
