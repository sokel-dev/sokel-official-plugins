# WeCom plugin

Send text, link cards, and interactive messages to members of the enterprise directory. In ordinary WeChat, recipients receive a link card and tap “View message” to open an in-WeChat webpage, where they can tap buttons configured by the caller. These are not buttons directly inside an ordinary WeChat message. The WeCom client also supports buttons inside native template-card messages.

Version 0.3.0 provides 34 operations. Interactive messages display content, identify members, and emit action events for release confirmations, task feedback, notification receipts, and similar scenarios. Workflows or internal systems define the business rules.

## Capabilities

| Administration feature | Plugin support | Additional configuration or limitations |
|---|---|---|
| Send messages | Eleven native message types, custom H5 interactions, dedicated native template-card operations, temporary media uploads, and message recall. | Ordinary WeChat does not support Markdown, mini-program notifications, native template cards, or recall. |
| Receive messages | Encrypted verification, ordinary-message and application-event triggers, and recent-record queries. | Configure API receiving in the administration console with a callback URL, Token, and EncodingAESKey. |
| Automatic replies | Persistent rules for exact/substring text matches and menu clicks. | Configure API receiving first. Only enabled, matching rules reply. |
| Custom menus | Get, set, and delete. | WeChat and WeCom support different menu actions. |
| Chat attachment panel and chat toolbar | Web identity and JS-SDK signatures. | Configure entry points in the administration console. An existing business webpage and frontend JS-SDK integration are required; this is not an ordinary WeChat feature. |
| Custom workbench display | Templates, individual member data, and batch member data. | Enable in the administration console first; supported only by the WeCom client. |
| Web authorization and JS-SDK | Automatic H5 member identification, member code exchange, and dual signatures. | Trusted domains and ownership verification must still be configured. |
| WeCom authorized sign-in | Current web sign-in URL and code-to-member identity exchange. | Your business login callback must verify state and establish a session. |
| Approval APIs | Self-built application engine queries/status callbacks, plus Approval application templates and submission APIs. | These are separate systems. Self-built application flows start through frontend JSAPI; the Approval application requires authorization. |
| Enterprise trusted IP | Guidance through API errors. | Add the plugin's outbound IP in the WeCom console; the plugin cannot configure it for you. |

Detailed guides: [Messages, menus, and workbench](https://github.com/sokel-dev/sokel-official-plugins/blob/wecom/v0.3.0/wecom/docs/app-features.md) · [Incoming messages and automatic replies](https://github.com/sokel-dev/sokel-official-plugins/blob/wecom/v0.3.0/wecom/docs/callbacks.md) · [Identity, JS-SDK, and approvals](https://github.com/sokel-dev/sokel-official-plugins/blob/wecom/v0.3.0/wecom/docs/identity-approval.md). These integrations do not automatically change existing menus, create approval applications, or install business pages.

## Prerequisites

1. Create a self-built application in WeCom. Record the enterprise ID, AgentId, and application Secret. Add recipients to the directory and the application's visibility scope.
2. Add **the plugin process's public outbound IP** to the application's enterprise trusted IP list.
3. Members use ordinary WeChat to scan the invitation QR code under My Enterprise → WeChat Plugin, complete member binding, and enable application messages.
4. Disable “Members must use the WeCom client when using the WeChat plugin.” Do not enable “Always open the homepage in the micro workbench,” which otherwise truncates messages or replaces them with a prompt to view them in WeCom.
5. Install the plugin in Sokel and create the credential above. After starting the plugin, copy the complete HTTPS URL from that credential's Webhook panel.
6. Configure that URL's domain as the application's **trusted domain for web authorization**, then complete domain ownership verification as required by the console. Serve the verification file from the domain's website root or reverse proxy; putting it only under `/hooks/…` is insufficient.
7. If you do not own the platform domain and cannot verify ownership, reverse-proxy the platform webhook through a domain you own and can verify. Keep the public URL consistent with `portal_url`.

Only enterprise member UserIDs are supported: not phone numbers, WeChat IDs, external-contact external_userid values, or `@all`. Credentials with the same CorpID + AgentId share the interactive-message namespace. Do not use one self-built application to isolate mutually untrusted tenants.

## Multiple applications and credentials

One plugin process can serve multiple applications. In Sokel, create a credential for each self-built application with its CorpID, AgentId, and Secret. For incoming callbacks, also provide that application's Token and EncodingAESKey, and configure its own credential Webhook URL plus `/callback` in the corresponding application. Select the appropriate credential in workflow operations and event triggers. Different enterprises can use credentials with different CorpIDs.

Access tokens and JS-SDK tickets are isolated by the complete application credential. Interactive messages are isolated by CorpID + AgentId; callback rules and history additionally include a hash of EncodingAESKey. One application's credential cannot read another application's interactive messages or dispatch its pending events. Duplicate credentials with the same CorpID + AgentId are aliases of the same application and share interactive-message data. If their callback keys also match, they share callback rules and history too; they are not separate tenants.

A separate container per application is unnecessary. Keep one replica and one persistent volume for the entire plugin. All applications using the same outbound IP must allowlist that IP individually and configure their own visibility scope and trusted web domains.

## Installation and operation

Developers first run `go build -o wecom .` in the plugin directory. In Sokel, open Plugins → Import manifest and import this directory's `manifest.yml` to obtain a connection-group token, then run:

```sh
SOKEL_ENDPOINT=https://sokel.example.com \
SOKEL_TOKEN=skp_replace_me \
SOKEL_DATA_DIR=/var/lib/sokel-wecom \
SOKEL_INSTANCE_ID=wecom-1 \
./wecom
```

Configure credentials only on Sokel's Credentials page, never in environment variables or data files. The plugin connects outbound to the platform; H5 uses the platform Webhook and needs no additional listening port. The data directory must be writable and backed by a persistent volume. **Run only one replica**; a second process using the same directory refuses to start. Do not run replicas of one connection group with separate data volumes. Linux/macOS are supported; the storage lock does not currently support Windows.

The official image is `ghcr.io/sokel-dev/sokel-plugin-wecom:0.3.0`, supporting Linux amd64/arm64. The official catalog pins it to a release digest. Use the persistent startup configuration below; the platform's generic generated startup command does not create a persistent volume automatically:

```sh
docker run -d --name sokel-wecom --restart unless-stopped \
  -e SOKEL_ENDPOINT=https://sokel.example.com \
  -e SOKEL_TOKEN="$SOKEL_PLUGIN_ACCESS_TOKEN" \
  -e SOKEL_INSTANCE_ID=wecom-1 \
  -e SOKEL_DATA_DIR=/var/lib/sokel/wecom \
  -v sokel-wecom-data:/var/lib/sokel \
  ghcr.io/sokel-dev/sokel-plugin-wecom:0.3.0
```

`SOKEL_PLUGIN_ACCESS_TOKEN` is the connection-group token injected by the deployment environment. Start only one replica and reuse the same named volume when updating or recreating the container. Alternatively, use the [Compose example](https://github.com/sokel-dev/sokel-official-plugins/blob/wecom/v0.3.0/wecom/docker-compose.yml) or the [single-replica Kubernetes example with persistence](https://github.com/sokel-dev/sokel-official-plugins/blob/wecom/v0.3.0/wecom/deploy/k8s.yaml). Kubernetes uses a Secret for connection information and a PVC for data.

## Sending options

| Operation | Purpose |
|---|---|
| `send_text` | Ordinary text messages, up to 2048 bytes. |
| `send_card` | A card with title, description, and link. The description is escaped as plain text. Opens a business page. |
| `send_interactive_message` | Saves content and custom buttons, sends a link card, and returns H5 actions as events. |
| `get_interactive_message` | Queries content, buttons, business context, the latest action, and send results. |
| `send_template_card` | Sends an official native card object. In-message interactions work only in the WeCom client. |
| `send_message` | Advanced sending API for eleven native message types and their official parameters. |
| `health_check` | Verifies the credential's application and local storage. Does not send messages or verify recipients or WeChat rendering. |

## Ordinary WeChat: link cards and custom H5 buttons

Example `send_interactive_message` input:

```json
{
  "users": ["zhangsan", "lisi"],
  "title": "Please confirm the release plan",
  "content": "Version rel-001 is scheduled for tonight. Please review the change notes and provide feedback. This example collects feedback only and does not perform a release.",
  "buttons": [
    {"id": "approve", "text": "Agree", "value": {"decision": "approved"}},
    {"id": "reject", "text": "Not yet", "value": {"decision": "rejected"}}
  ],
  "context": {"scene": "release_review", "release_id": "rel-001"},
  "request_id": "release-review-rel-001",
  "portal_url": "https://sokel.example.com/hooks/this-credential-token",
  "button": "View",
  "expires_hours": 72
}
```

Field rules:

- `users` is an explicit array of enterprise member UserIDs. Only recipients accepted by the sending API for this message can open it and act.
- `title` contains 1–128 characters; `content` contains 1–16384 UTF-8 bytes. The message summary is shortened, while H5 retains the full body.
- `buttons` contains 1–8 buttons. Each unique `id` uses 1–64 ASCII letters, digits, underscores, or hyphens; `text` contains 1–40 characters. Optional `value` must be a JSON object.
- Optional `context` is a JSON object shared by all actions. The combined encoded size of `context` and all button `value` objects must not exceed 16 KiB. They appear in callbacks; do not put passwords or credentials in them.
- `request_id` is an optional send idempotency key. Without it, every invocation creates a new interactive message. With it, the same key and content within the same application return the existing message.
- `portal_url` must be the complete Webhook URL of the sending credential, without `/callback`. `button` is the link text that opens H5 from the message card, containing 1–4 characters and defaulting to “查看消息” (“View message”). It differs from action buttons in `buttons`.
- `expires_hours` defaults to 72 and accepts 1–720. Actions are disabled after expiration.

These buttons all trigger action callbacks. For a simple link, use `send_card`. IDs such as `approve` and `reject` are business definitions supplied by the caller. The plugin does not define severity, claiming permissions, terminal confirmation states, or an approval state machine.

The operation returns `interaction_id`, `interaction_url`, `reused`, `msg_id`, `invalid_user`, `unlicensed_user`, and `all_accepted`. Reusing an idempotency key does not send again or extend the original expiration. Use a new key when changing recipients, content, buttons, context, the entry URL, or other configuration. Deduplication applies only during record retention; reuse is no longer guaranteed after cleanup. Duplicate requests do not resend to previously invalid recipients. After correcting member configuration, send again with a new key.

Check `all_accepted`, `invalid_user`, and `unlicensed_user`. A successful WeCom API response does not mean every recipient was accepted, that a phone received a push, or that a user read the message.

## Handle interaction callbacks

Opening the card inside WeChat automatically identifies the member through silent web authorization, then displays the body and buttons. Reopening or refreshing identifies the member again. If verification fails, the authorization callback is abnormal, or browser back navigation restores the page, a manual retry entry is shown to prevent repeated redirects. Other browsers retain a manual entry; opening in WeChat is recommended.

The platform accepts only member IDs returned by official authorization, not usernames supplied through URLs or forms. A successful action consumes its ticket and issues a new one, allowing the member to click again. Retrying the same ticket and action returns the original result without emitting another event. The workflow or business system must enforce single-choice rules, conflicts between members, and whether actions are allowed in a given state.

Every new action emits `interaction_action`, including:

- `event_id`: idempotency key for this action.
- `interaction_id`, `msg_id`: plugin interactive-message ID and WeCom message ID.
- `request_id`: optional idempotency key supplied when sending.
- `action_id`, `action_text`, `action_value`: clicked button's ID, text, and object value.
- `context`: business context supplied when sending, such as `scene` and `release_id`.
- `actor_user_id`, `occurred_at`: actor and action time.
- `revision`: increasing action revision for the same interactive message.

Configure Sokel as follows: **Interaction action event → Filter by `context.scene` → Deduplicate by `event_id` → Verify business permissions and current state → Branch by `action_id` → Save the processing result or send a receipt**. The example's `approve` / `reject` values are feedback choices only. Neither creating the message nor receiving its action automatically releases a version or starts a WeCom approval application.

Event fields are namespaced, for example `{{#n_event.interaction_action.action_id#}}` and `{{#n_event.interaction_action.context.release_id#}}`. Select the same credential used for sending. To reply using the event credential, bind the downstream credential to `{{#n_event.credential_id#}}` and restrict the allowed credential list. Workflows driven by real events must be published with the corresponding trigger enabled.

Deduplicate by `event_id`, and use `revision` to reject stale updates that would overwrite newer state. The plugin records a member's action, not completion of business approval. For example, if an internal system rejects a second approval, record the reason yourself and send a receipt if needed.

Call `get_interactive_message` with `{"interaction_id":"ID from the send result"}` to query content, `buttons`, `context`, `request_id`, `last_action_id`, `actor_user_id`, `revision`, `created_at`, `updated_at`, `expires_at`, `sent`, and `msg_id`. This query returns the latest action; it is not a full action-history replay API.

The action and its pending event are persisted in the same snapshot. Failed submissions to the platform are retried; after restart, the credential's event source resumes submission using the same event ID. **“Notification submitted” means only that the SDK accepted the send, not that the platform received it or that the workflow finished updating the business system**. The current SDK event protocol has no receipt acknowledgment, so a crash during network buffering can still lose an event. The plugin's outbox retries failures before local publication and does not promise at-least-once delivery. For important business processes, reconcile the latest saved action using `get_interactive_message`, inspect your business records and workflow results, and design compensation. The background process submits at most 16 events every five seconds. Saving an action on the webpage does not wait for downstream workflow completion.

## WeCom: native template cards

Use `send_template_card` with `users` and the official native `card` object. This route targets the WeCom client, follows the official API's card types and content, and requires API receiving configuration. Native actions arrive through `application_event`; handle `event_type=template_card_event`, `event_key`, `task_id`, and `response_code`, then call `update_template_card` as needed. It does not create an H5 interactive message or emit `interaction_action`. See the [application features guide](https://github.com/sokel-dev/sokel-official-plugins/blob/wecom/v0.3.0/wecom/docs/app-features.md) for complete examples.

## Deployment and acceptance boundaries

- Uses official WeCom APIs, not personal-WeChat extensions, hooks, or client automation.
- WeCom application sending is rate-limited. Aggregate large notification volumes upstream. Network interruptions, or an upstream success before the plugin saves the result, prevent exactly-once guarantees; retries can produce duplicate messages.
- OAuth state and action tickets expire after five minutes. A ticket is valid for one interactive message and one member; a successful action consumes it and issues another. Sokel Webhook does not forward cookies. The plugin binds authorization state through browser sessionStorage without relying on cookies.
- Platform Webhook logs record requests and responses and may contain short-lived authorization material and message content. Restrict log access and retention. Never place Secrets or business passwords in bodies, button values, or context. Long-term Secrets and access_token values are not written to plugin data files.
- Interactive messages are retained for 24 hours after expiration, then cleaned up during subsequent writes. Unsubmitted events are not automatically discarded. One instance retains at most 10000 interactive messages, 20000 pending or retained events, and 20000 records per authentication-record type. Capacity limits fail explicitly. Back up the persistent volume and monitor disk usage and event-source status.
- A single data snapshot must be smaller than 128 MiB; larger writes are explicitly rejected. This storage targets small and medium interaction workloads, not high-throughput message queues.
- Earlier versions were tested against a real enterprise account for API acceptance of text/link cards, ordinary-WeChat member identification, and persistence of actions from the old flow. This does not replace acceptance testing for 0.3.0: custom buttons, repeated clicks, rejection of nonrecipients, native template-card callbacks, Sokel event receipt, and internal-system updates must each be verified on the target clients.

Official references: [WeChat plugin](https://open.work.weixin.qq.com/help2/pc/14797), [Send application messages](https://developer.work.weixin.qq.com/document/path/90236), [Web authorization links](https://developer.work.weixin.qq.com/document/path/91022), and [Get user identity](https://developer.work.weixin.qq.com/document/path/91023).

## Upgrade notes

Version 0.3.0 makes interactions generic through messages and custom buttons, removing the public `send_alert`, `get_alert`, and `alert_action` interfaces. Existing workflows must migrate operations, events, field references, and business logic; they cannot be reused unchanged:

| Previous interface | New interface | Migration |
|---|---|---|
| `send_alert` | `send_interactive_message` | Replace `description` with `content`, configure `buttons` explicitly, put business identifiers in `context`, and optionally provide `request_id` for send deduplication. |
| `get_alert` | `get_interactive_message` | Supply `interaction_id` and read the latest action. Workflows or internal systems maintain business state. |
| `alert_action` | `interaction_action` | Use new fields such as `action_id`, `action_value`, and `context`; built-in claimed/confirmed states are no longer returned. |

V1 storage remains readable. Old messages without `buttons` remain read-only and are not automatically converted into actionable messages. Back up the persistent volume before upgrading, resend messages that need interactions, and migrate workflows. The new operations and events do not promise compatibility with old flows.

API receiving remains optional. Leaving it unconfigured does not affect text, link cards, or H5 interactions. Rotating the application Secret preserves interactive messages and pending callbacks. Rotating EncodingAESKey switches the official API callback history/rule namespace; process pending events and recreate rules first.

Only APIs with configured permissions can be called against a real enterprise. API receiving, automatic replies, native-card interactions, business pages in toolbars, and approvals each need their own console configuration and client acceptance tests.
