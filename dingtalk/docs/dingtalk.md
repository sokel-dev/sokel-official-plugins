Connect an internal enterprise DingTalk application bot. Send text or Markdown to groups or up to 20 users, resolve attachment download codes, and receive HTTP robot messages. Markdown requires a title.

Create and publish an internal application with a bot, then configure AppKey, AppSecret and robotCode. Enable enterprise robot messaging permission and add the bot to target groups. Health checks validate token issuance only, not recipient access.

Configure the credential-specific Sokel HTTPS webhook as the robot HTTP callback. The plugin verifies timestamp/sign within one hour and deduplicates by msgId. Generic encrypted event subscriptions and Stream mode are not supported. sessionWebhook is never emitted. Attachment events expose download codes; temporary download URLs contain short-lived authorization and should be handled carefully.

A delivery receipt acknowledges acceptance, not delivery to every user. Inspect invalid and rate-limited recipient lists. Writes are never automatically retried; verify delivery before retrying a timeout.

Official API references: [token](https://open.dingtalk.com/document/orgapp/obtain-the-access_token-of-an-internal-app.md), [group](https://open.dingtalk.com/document/development/the-robot-sends-a-group-message.md), [direct](https://open.dingtalk.com/document/orgapp/chatbots-send-one-on-one-chat-messages-in-batches.md), [receive](https://open.dingtalk.com/document/orgapp/receive-message.md), [attachments](https://open.dingtalk.com/document/orgapp/download-the-file-content-of-the-robot-receiving-message.md).
