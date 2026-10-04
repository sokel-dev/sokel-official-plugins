# mastodon — publisher plugin (one of the P0 three)

Uses the same publish contract as [bluesky](../bluesky/README.md)
([docs/social-publishing-plugins.md](../../docs/social-publishing-plugins.md) §3): publishing
returns `id` + `url`, chunking long content is the plugin's job, media travels with publishing,
and the health check uses the platform-mandated `health_check`.

The user-facing manual is [docs/mastodon.md](docs/mastodon.md) (reported with the handshake). The
design decisions are written at the top of `schema/schema.go`.

## Four things that differ from other platforms

1. **The character limit is asked from the instance** (`client.go::maxChars`). Mastodon is a
   federated network, and 500 is just the official default; many Chinese-language instances use
   5000, some go up to 11000. Hardcoding it would mean the plugin itself rejects a long post that
   the user could clearly send fine on the web UI. Asked once and cached.
2. **Publishing carries an idempotency key** (`Idempotency-Key`, same key within an hour lands
   only one post). Workflows retry, and without this, a single timeout-and-retry turns into two
   identical posts on the timeline. The key is a digest of "body + reply target + visibility +
   CW" — none of which change when the same node reruns.
3. **CW and visibility are inherited across a whole thread.** Mixing public and unlisted in one
   thread leaves readers seeing only a disjointed half.
4. **Video has to wait for transcoding.** The v2 media endpoint returns synchronously for images
   but returns 202 with an empty url for video, and posting at that point gets a 422 (with the
   error only saying "media unavailable"). The plugin polls until processing finishes before
   posting.

## Development

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```
