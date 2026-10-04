# wechat-claw — WeChat iLink Bot plugin (send and receive in one)

Built on [wechat-clawbot-client-go](https://github.com/importcjj/wechat-clawbot-client-go).
One credential = one WeChat account; multiple accounts in a single instance are naturally covered
by the platform's per-credential supervisor (configure as many credentials as accounts you want
to run).

## Connecting

```bash
SOKEL_ENDPOINT=https://<platform address> \
SOKEL_TOKEN=skp_xxx \
./wechat-claw
```

Platform side: Plugin management → create a plugin → get the group token, start this process →
Credential management → create a credential for this plugin (leave the fields empty) → click
"Login / Authorize" on the credential row → a QR code pops up → scan it with WeChat and confirm →
the platform writes the session into the credential row → within about 20s (the next heartbeat),
the event source automatically comes online with that account (visible in the instance table's
"event source" column).

## Capabilities

- **Event**: `message` (message received) — `chat_id` (the other party's wxid, flattened to the
  top level as a shared field) / text / message_id / media counts / raw.
- **Operations**: `send_text` / `send_image` / `send_file` (to = the chat_id from the event).
- **Collaborative login**: declared in the schema with `auth.QR()` + the generated
  `RegisterAuth(p, start, poll)` (a QR-code scan in the credential panel, not on the canvas).

## Design notes

- **No credential is persisted by the plugin**: the session is read from what's issued at
  registration (platformStore.Load) and written back via `sokel.credential.update` (a token
  refresh while running isn't lost); the sync cursor and context token are memory-only (restart
  cost: resumes collection from now; after a restart, the other party needs to send a message
  first before a reply can go out).
- **Sending depends on the running client** (the context token lives in its store) →
  **the WeChat group recommends a single-replica deployment**; with multiple replicas, a send
  operation might land on a replica that doesn't hold that account's event source (it will return
  a clear error rather than failing silently).
- Session expired → that account lights up "pending login" in the instance table; scanning again
  is enough (no process restart needed).

## Compliance warning

The iLink bot API is an unofficial channel and carries a ban risk — a dedicated account is
recommended; don't use your primary account.
