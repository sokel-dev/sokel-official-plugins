# WeChat ClawBot (wechat-claw)

Connect to WeChat's official ClawBot through the iLink Bot API. After linking by QR code, messages you send to ClawBot in WeChat
can trigger workflows, which can send text, images, and files back to you.

> **Understand what it is first:** ClawBot is WeChat's official entry point for personal AI assistants such as OpenClaw.
> After linking, a contact named ClawBot appears in your WeChat contacts. WeChat's iLink service relays your conversation to this plugin.
>
> - **Only the linked account owner can converse with it.** Other people cannot add your ClawBot, and it cannot join groups.
>   It suits a personal assistant for notes, data lookups, or running a workflow on your behalf; it cannot serve customers or reply in groups.
> - **Receive before replying.** Replies require the conversation context from your latest incoming message.
>   The plugin keeps this context only in memory. After the plugin restarts, send it a message before a workflow can reply.
> - WeChat reserves the right to limit message frequency, filter content, or terminate the service. Follow WeChat's usage rules.

## Link by QR code: no values to enter

1. Create a credential under **Credentials** and **leave the session field blank**.
2. Save it and click **Log in** on the credential row. The platform displays a QR code.
3. Scan and confirm it in WeChat on your phone. The credential becomes logged in, and ClawBot appears in WeChat.

The platform stores and maintains the session. If it expires, scan again; restarting the plugin is unnecessary.

## Multiple WeChat accounts

One credential represents one linked WeChat account. Events include `credential_id` to identify which account received the message.
Bind the credential for downstream replies to this variable to keep accounts separate.

## Notes

- Set the reply recipient to the event's `chat_id`, such as `xxx@im.wechat`, rather than a display name.
- Images and files are transferred through the platform file layer; large files are not embedded in a single call.
- Sending depends on the plugin replica that is receiving messages. A single replica is recommended;
  see the plugin repository's README.
