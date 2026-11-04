# Feishu (custom app)

Connect Feishu to workflows: send messages and cards, trigger runs from incoming messages, write cloud documents, and read or write Bitable data.
If you only need notifications in **one group**, use the simpler **Feishu Group Bot** plugin; it does not require creating an app.

## 1. Create a custom app: about five minutes

1. Open the [Feishu developer platform](https://open.feishu.cn/app), or open.larksuite.com for international Lark,
   and select **Create Custom App**.
2. On **Credentials & Basic Information**, copy the **App ID**, which starts with `cli_`, and **App Secret** into this plugin's credential.
3. Under **App Features**, add a **Bot**.
4. Enable the required permissions under **Permissions**, using the table below, then **create and publish a version**.
   Permission changes do not take effect until the new version is published; this is the most common setup mistake.

| Operation | Required permission |
|---|---|
| Send, reply to, or recall messages | `im:message`, for reading and sending direct and group messages |
| Upload images or files | `im:resource` |
| Look up users by email or phone number to obtain open_id | `contact:user.employee_id:readonly`, or the permission to obtain user IDs by phone number or email |
| List or create chats, or add members | `im:chat` |
| Cloud documents | `docx:document` |
| Bitable | `bitable:app` |
| Drive uploads | `drive:drive` |

5. After publishing the version, have an administrator approve it. Internal custom apps are usually approved quickly.

## 2. Trigger workflows from messages or card buttons: optional

Events use a **persistent connection**. No public callback URL is needed; the plugin connects to Feishu:

1. Under **Events & Callbacks**, select **Receive events through a persistent connection** as the subscription method.
2. Under **Add Events**, select **Receive message: im.message.receive_v1** and, if needed,
   **Bot added to group: im.chat.member.bot.added_v1**.
3. For card buttons, also choose the persistent connection method for **Card Interactions** callbacks.
4. Once the app_id and secret are configured in the platform credential, the event source comes online automatically.
   Its status is visible on the plugin instance page.

Messages sent directly to the bot can trigger a workflow. In a **group, the user must @mention the bot** for the message to be received.
This is Feishu's permission model, rather than filtering performed by the plugin.

## 3. Common operations

- **Send Markdown**, recommended for reports and alerts: supports **bold**, [links](…), lists, and code blocks.
  It renders as a card, with optional title and title color.
- **Send text**: use `<at user_id="ou_xxx"></at>` to mention someone or `<at user_id="all"></at>` to mention everyone.
- **Look up user**: exchange an email address or phone number for an open_id—**a required step before messaging an individual**.
- **Reply to message**: bind `message_id` directly to the output of the **Message received** event.
- **Bitable**: `app_token` accepts a pasted table URL. **Share the table with the app first**
  through the table's upper-right **… → Add document app** menu, or the service returns 1254050.
- **Create cloud document**: supplying `folder_token` is recommended. Documents placed in the app's own space will not appear in your Drive.
- **Generic API call (`call`)**: for endpoints not covered above, enter the method, path, and body from the Feishu documentation.
  The plugin manages the token.

## 4. Troubleshooting

| Error | Action |
|---|---|
| App lacks permission (99991672) | **Publish a new version** after enabling the permission |
| Bot is not in this conversation (230002) | Add the bot to the group or verify chat_id |
| Bot feature is not enabled (230013) | Add the Bot feature and publish a version |
| Bitable access denied (1254050) | Share the table with the app using Add document app |
| Events are not received | The subscription method must use a persistent connection, and the events must be enabled in the **published** version |

## 5. Security

App Secret is used only inside the plugin to obtain tenant_access_token. It does not appear in node inputs, outputs, or logs.
One credential represents one app. Create separate credentials for multiple apps; the plugin opens a separate persistent event connection for each credential.
