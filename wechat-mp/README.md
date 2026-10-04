# wechat-mp — WeChat Official Account publisher plugin (the first P1 one)

Uses the same publish contract as [bluesky](../bluesky/README.md) / mastodon / discord.
The user-facing manual is [docs/wechat-mp.md](docs/wechat-mp.md).

## Why it's the first P1 plugin

**The only mainstream channel in China with "a legitimate API, financial content is allowed, and
reach is good enough"** — Xueqiu/Guba/10jqka/Futu/Zhihu/Toutiao all lack an official publishing
API, Weibo is down to share links only, and Douyin/Channels/Xiaohongshu are blocked on financial
content qualifications (see
[docs/social-publishing-plugins.md](../../docs/social-publishing-plugins.md)).

## Four quirks of Official Accounts (each one causes "it's configured but won't publish")

1. **Two-step publishing**: create draft → publish draft, with a media_id in between. This is built
   as two operations rather than one "post article" because a draft can be eyeballed in the backend
   once created — financial content should have this checkpoint, and whether to wire up a human
   review node on the canvas is up to you.
2. **A cover image is mandatory**, and it must be a permanent material's media_id. Without one,
   WeChat returns a cryptic 41005, so the plugin catches this and explains clearly before even
   sending the request.
3. **Hotlinked images in the body are always blocked** (anti-hotlinking). The plugin scans the body
   before creating the draft, and catches any `<img src>` not on a WeChat domain — otherwise you'd
   publish an article with no images, with no way to fix it afterward.
4. **IP allowlist**: if the calling server's public egress IP isn't allowlisted, every endpoint
   returns 40164. This is easiest to miss in a container deployment, so the error message tells you
   directly how to check the real egress IP.

Two more boundaries that are easy to get wrong: **as of 2025-07, personal accounts and unverified
enterprise accounts have had publishing permission revoked** (48001); an article sent via
`freepublish` **doesn't enter the message history feed** — it's a permanent link, not the same
thing as a mass send.

## Two implementation details

- **Business errors are packed inside an HTTP 200** (`errcode != 0`). Checking only the status code
  would treat "IP not in the allowlist" as success, and it would then fail in some mysterious way
  at the next step.
- **access_token is globally unique**: refreshing it again for the same AppID immediately
  invalidates the previous one. It's cached in-process by appid (renewed 5 minutes early), and on a
  40001 the cache is cleared and retried once; **multiple replicas would keep invalidating each
  other's token**, so run a single replica.

## Development

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```

The tests (against a fake WeChat) pin down: business errors inside a 200, retrying once on an
invalid token, catching a missing cover/hotlinked images early, waiting for a terminal state since
publishing is asynchronous, and a single health check validating "credentials/allowlist/permission".
