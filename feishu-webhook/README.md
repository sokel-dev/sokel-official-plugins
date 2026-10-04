# feishu-webhook — Feishu group custom bot (first-party self-hosted)

A single-operation plugin: `webhook_send` (text / Markdown / card) + `health_check`.
The user-facing manual is [docs/feishu-webhook.md](docs/feishu-webhook.md).

## Scope: a separate plugin from the feishu main plugin

A group webhook's authorization scope (can only post to one group) and an enterprise app (can post
to the whole company) **shouldn't be mixed into one credential pool** — the credential type is a
security boundary. Making it an optional field on the main plugin would mean picking the wrong
credential on the canvas doesn't blow up until runtime. The discord plugin is a structurally
identical precedent. See the top comment in `schema/schema.go` for the design tradeoffs.

## Gotchas (read before touching the code)

- **The signature algorithm is the opposite of intuition**:
  `HmacSHA256(key = timestamp+"\n"+secret, data = empty string)`. The secret goes in the key, and
  the data being signed is an empty string — that's what Feishu's docs specify. `TestSignShape`
  pins down a fixed value cross-checked against an independent implementation; "fixing" it based
  on normal HMAC intuition turns the test red.
- **The response comes in two shells**: the new version uses `{code,msg}`, the old version uses
  `{StatusCode,StatusMessage}`, and both must be recognized.
- **health_check never posts a real message**: it sends a validation request with empty content,
  and judges liveness from the difference between "parameter error" and "URL is dead". 19021 means
  the secret is wrong, 9499 means rate-limited (100 messages/minute).
- The webhook URL itself is the key (whoever has it can post to the group), so the credential
  field is stored as a Secret.

## Development

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```
