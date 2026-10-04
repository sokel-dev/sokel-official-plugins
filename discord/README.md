# discord — publisher plugin (one of the P0 trio)

Shares the same publish contract as [bluesky](../bluesky/README.md) ([docs/social-publishing-plugins.md](../../docs/social-publishing-plugins.md) §3):
publishing returns `id` + `url`, long content splitting is the plugin's own job, media travels with the publish, and health checks use the platform's conventional `health_check`.

The user-facing manual is [docs/discord.md](docs/discord.md) (reported during handshake). Design decisions are documented at the top of `schema/schema.go`.

## Positioning and three design decisions

**Community distribution, not a public discovery channel** — what's posted is only visible to channel members.
Use it to push a note to the research team's channel once a report is out; use bluesky / mastodon / x for public exposure.

1. **Webhook by default, not a bot**. For sending messages, the two are equally capable, but a webhook only takes
   a channel admin a few clicks to create, while a bot requires creating an application, configuring intents,
   inviting it into the server, and managing permissions.
2. **The embed card is the primary form**. Financial pushes are "title + summary + link + a few numbers" — plain
   text would be unreadable when it floods the channel. Embed fields are **sorted by key** — map iteration is
   random, so without sorting the same input would produce a different layout each time.
3. **The receipt must include the message id**: a webhook returns an empty 204 body by default, so the plugin
   always adds `?wait=true`, otherwise downstream can't edit or delete the message.

## Development

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```
