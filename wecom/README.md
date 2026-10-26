# wecom

Sokel plugin for WeCom self-built applications: 34 operations covering notifications, configurable H5 interactions, native template cards, encrypted incoming messages, automatic replies, menus, workbench data, web identity and approvals. Ordinary WeChat receives a link card and opens its buttons in an authenticated H5 page; buttons inside the message itself require the WeCom client. The public contract is `manifest.yml`; generated `zz_*.go` files must not be edited manually. User setup is documented in [docs/wecom.md](docs/wecom.md).

## Architecture

- `client.go`: fixed official API host, credential-isolated token cache, coalesced refresh, strict business-error parsing, eight-second HTTP timeout, one token-invalid retry, no redirect following or secret-bearing error URLs.
- `main.go` and `features*.go`: generated typed handlers, recipient validation, optional request deduplication, event routing and outbox publishers.
- `client_app_features.go`, `client_media.go`, `client_template_card.go`: fixed-path official APIs, explicit recipients, multipart temporary media, menu/workbench validation, partial delivery/update results.
- `client_identity_approval.go`: credential-scoped JS ticket caches, current web-login URL, separate approval-app and self-built approval-engine APIs.
- `callback_crypto.go` / `callback_features.go`: official AES/signature protocol, authenticated XML, replay checks, deduplication, persistent automatic reply rules and an independent snapshot directory.
- `webhook.go` and `web/index.html`: same-origin JSON API and mobile HTML served through the platform's credential-scoped Webhook. It does not open another HTTP listener.
- `store.go`: copy-on-write JSON snapshots, atomic replacement, fsync and exclusive directory lock. OAuth states/tickets are hashed; application credentials and access tokens are never persisted.

The namespace is a hash of CorpID and AgentId. This deliberately survives secret rotation. Every storage-access entry verifies the Secret against the official agent/get endpoint (success cached for at most five minutes); syntax-valid public IDs alone grant no access. Two credentials for the same application are the same identity, not separate tenants. A deployment is one process with one persistent volume; multiple replicas on separate disks are unsupported.

A fresh interaction link opened in WeChat starts `snsapi_base` identity verification automatically. Other browsers retain a manual entry; failed or invalid callbacks and browser history restoration require an explicit retry to avoid redirect loops. Browser detection controls this convenience only; the server still verifies every identity. A browser generates 256-bit hex OAuth state and stores it in sessionStorage. The callback page checks it before exchanging the code; the server consumes state once and resolves the member through the official `auth/getuserinfo` endpoint. It authorizes only recipients not rejected by delivery, then issues a five-minute, single-action ticket. A successful action rotates the ticket; replay of the original ticket with the same action returns the original result without another event. The interaction portal writes use JSON POST; its GET serves only the public HTML shell. The separate `/callback` route handles WeCom encrypted GET verification and POST messages. There are no cookies, external scripts, or cross-origin APIs.

A transaction persists the action and event together. Publication retries use the event's stable ID. The SDK's Trigger is fire-and-forget: submission acknowledges neither platform receipt nor downstream completion. A process failure after an in-memory broker publish may lose an event; the outbox does not promise at-least-once platform delivery. Critical integrations should reconcile the latest stored action through `get_interactive_message` and their own business records. The query is not a full action-history replay API. Revision numbers let consumers reject out-of-order updates. No claim of exactly-once upstream notification delivery is made across network/process failures.

H5 buttons have caller-defined IDs, labels and optional object values, plus shared object context. There is no built-in severity, claim ownership or terminal business state. Members may perform multiple actions; a workflow or business backend decides whether an operation is allowed, accepted only once, or changes a business state. A button called `approve` does not itself publish a release or submit a WeCom approval request. `send_template_card` sends native WeCom cards; their callbacks remain `application_event`, separate from H5 `interaction_action`.

## Develop and verify

Go 1.25+, SDK pinned to v0.7.7:

```sh
go generate ./...
go test -race -cover ./...
go vet ./...
go build -o /tmp/sokel-wecom .
go run -mod=mod github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen check .
```

`go generate` uses the module-pinned generator, not whichever older sokel-gen happens to be on PATH. Documentation is embedded during generation, so regenerate after changing `docs/wecom.md` too.

Browser tests use Playwright and mocked credential-scoped API responses:

```sh
PLAYWRIGHT_MODULE_PATH=/absolute/path/to/node_modules/@playwright/test \
node --test web/portal.test.cjs
```

Browser verification should cover automatic identity verification in WeChat, manual retry without redirect loops, OAuth state mismatch, callback cleanup, XSS-safe content and button rendering, repeat interactions, and retrying an uncertain action with the original ticket. Go verification should exercise the HTTP wire shape against httptest, token renewal and malformed responses, optional request deduplication, button validation, persisted actions, concurrent actions, namespace isolation, failed delivery, restart recovery and storage failure boundaries.

The upstream success fixtures come from official documentation. Before 0.3.0, a live tenant smoke test verified accepted text/card sends, member identity resolution in ordinary WeChat, and a persisted action in the former alert-specific flow. That result does not validate the new configurable buttons. Version 0.3.0 still needs real-client verification of the H5 flow, native card callbacks, platform event receipt and downstream business-system updates.

## Build and run

```sh
docker build --build-arg PLUGIN=wecom -t sokel-plugin-wecom:0.3.0 ..
```

The official image is `ghcr.io/sokel-dev/sokel-plugin-wecom:0.3.0` (linux/amd64 and linux/arm64). From the public repository, the command above builds it with the shared Dockerfile. The example Compose file uses the released image and mounts a named volume at `/var/lib/sokel`, with `SOKEL_DATA_DIR=/var/lib/sokel/wecom`. Fill the platform endpoint/token using your deployment's secret mechanism; configure WeCom credentials in Sokel, not Compose. Use the persistent Docker, Compose or Kubernetes examples: the platform's generic launch command does not add a volume automatically.

Snapshot storage is bounded and optimized for modest interaction volume, not a high-throughput queue. Expired messages are eligible for cleanup after 24 hours when they have no pending events; dispatched events are eligible after 24 hours. A directory fsync failure after rename is reported as uncertain durability and makes the current Store unavailable until restart, preventing stale memory from overwriting the visible committed file.

## General-purpose interactions (0.3.0)

See [message/menu/workbench examples](docs/app-features.md), [identity/approval examples](docs/identity-approval.md), and [API receive/automatic replies](docs/callbacks.md). Callback Token/AES fields are optional; existing notification credentials continue to work. No rules, menus, approval templates or toolbar URLs are installed automatically.

Callback records live in `SOKEL_DATA_DIR/features`, separately locked and persisted. Their namespace includes CorpID, AgentID and a hash of the callback AES key; API Secret rotation preserves pending events. Rotating the callback AES key starts a new isolated rules/history namespace, so drain old events and recreate rules first. The queue shares the existing SDK fire-and-forget limitation: the incoming HTTP ACK means local persistence, not completion of a downstream workflow. Dispatched callback records are eligible for cleanup after seven days, up to 5000 records total and 64 MiB per snapshot; pending records are never evicted. The XML is retained for nested/unknown event fields.

This release removes the public `send_alert`, `get_alert` and `alert_action` contracts. Migrate workflow operations to `send_interactive_message` / `get_interactive_message` and subscribe to `interaction_action`; field names and business semantics changed. There is no old-workflow compatibility promise. Version 1 snapshots remain readable, but legacy records without configured buttons are read-only and cannot acquire the new interaction semantics. Keep a backup before upgrading; existing records do not become new interactive messages automatically.

`events_common` remains empty: workflow variables belong under their event namespace, for example `{{#n_event.interaction_action.action_id#}}` and `{{#n_event.interaction_action.context.release_id#}}`. Dedicated message/application events expose their own fields and original XML. `credential_id` is supplied by the platform for credential-scoped routing; configure matching credentials or a restricted dynamic credential binding downstream.

[Kubernetes example](deploy/k8s.yaml) uses a single replica, a persistent volume and `Recreate` upgrades. Use the released image, or build/push it to your own registry, and inject the access-group bundle into a Kubernetes Secret. The repository and image build context exclude all local runtime data. A platform running in Kubernetes does not automatically make this plugin process a pod.

To use the Kubernetes example, first export/download this plugin access group's offline access JSON into a secure local file. Create the Secret in the target namespace, set the platform endpoint in the manifest, then apply:

```sh
kubectl -n YOUR_NAMESPACE create secret generic wecom-plugin-access --from-file=access.json=/secure/path/access.json
kubectl -n YOUR_NAMESPACE apply -f deploy/k8s.yaml
```

The pod needs outbound access to the broker advertised in that bundle and `qyapi.weixin.qq.com`; no new Service or Ingress is required. Set the pod's actual public egress IP in WeCom. To move the existing local runtime into Kubernetes, stop the old process, migrate the complete data directory into `/var/lib/sokel/wecom` on the PVC, then start the pod. Starting with an empty PVC does not restore prior message links/history.
