# Telegram Bot

Connect a Telegram bot to workflows: trigger runs from incoming messages, send text, images, or files,
download attachments sent by users, or call any Bot API method directly.

## Obtain credentials

1. Find **@BotFather** in Telegram, send `/newbot`, and choose a name. It returns a **Bot Token**
   in the form `123456789:AAH...`.
2. Create a platform credential and enter the token; this is the only field.

Click **Check** on the credential row after saving. It calls `getMe` without sending a message.
The returned bot username lets you distinguish a test bot from a production bot, whose token strings otherwise look alike.

## Receive messages: event triggers

Add a **Plugin event** trigger, select this plugin, and enable **Message received**.
**No webhook configuration is needed**: the plugin uses long polling, so the platform does not need a public URL.

For multiple bots, create separate credentials. Each event includes `credential_id`, identifying the bot that received it.
Bind downstream message-sending credentials to that variable so each bot replies through the correct account.

## Notes

- **Bots cannot see ordinary group messages** unless privacy mode is disabled:
  **BotFather → /setprivacy → Disable**. This is the leading cause of a bot not responding in a group.
- A user must **message the bot first**, or add it to a group, before the bot can message that user.
  Telegram does not allow bots to initiate unsolicited conversations.
- Attachments are downloaded through the Bot API, with a **20 MB limit**. Telegram does not allow bots to download larger files.
- Telegram requires a proxy for platform deployments in mainland China; see the deployment documentation.
