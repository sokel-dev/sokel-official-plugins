# Slack plugin

An independent Go module using the public Sokel SDK v0.7.8. The English contract is declared in `manifest.yml`; `locales/zh-CN.json` supplies Chinese translations, including the entire user guide. Generated `zz_*.go` files must not be edited manually.

The plugin implements identity checks, cursor-paged history and replies, text/thread posting, external file uploads and a single-channel polling source. It intentionally uses explicit bot-token credentials rather than claiming platform OAuth support.

## Development

```sh
go generate ./...
go test -race -cover ./...
go vet ./...
go build ./...
go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen@v0.7.8 check .
```

No `replace` directives or platform-internal packages are needed. `register` sets the runtime version to `0.1.1`. The health operation returns `ok` and `message`, including `ok=false` for failed checks.

## Decisions

- Every operation performs one attempt. HTTP errors, network failures and rate limiting never cause implicit write retries. Rate-limit metadata is parsed as seconds or an HTTP date, without reflecting server text.
- Requests use Bearer headers; redirect following is disabled even for same-origin redirects. Transport errors and unrecognized API errors do not expose raw response bodies, token-bearing URLs or signed upload URLs.
- Uploads request a lease with the actual byte length, POST the exact bytes without Authorization, then complete the upload. The production upload origin is fixed to Slack's file service. Uploads are bounded to 20 MiB.
- Pagination remains explicit for operations. Polling persists a fixed time window, page cursor and high-water mark. It advances in memory after event and checkpoint publication succeed. The persisted next-poll time prevents credential-triggered restarts from bypassing the polling interval. First start establishes a baseline. SDK 0.7.8 publication is fire-and-forget; local publication does not confirm durable platform storage or downstream acknowledgment.
- Poll events intentionally exclude bot messages, thread replies and edits/deletions. The source can lag behind high-volume channels; it is not an Events API substitute. A single source process per credential is required.

## Evidence and acceptance

The initial compilable stubs failed `TestHistoryCursorAndAuth` and `TestSendBytes` with `RED: not implemented`; those behavior tests passed after implementation. Additional tests cover upload bytes and credentials, redirects, request validation, 429/no retries, malformed responses, pagination, filtering and event/checkpoint behavior.

The history message example follows Slack's published response example. Pagination, adversarial errors, Chinese text and event sequences are synthetic test cases, explicitly not live captures. No live account acceptance is claimed.

Official API references checked during implementation:

- [auth.test](https://docs.slack.dev/reference/methods/auth.test/)
- [conversations.history](https://docs.slack.dev/reference/methods/conversations.history/)
- [conversations.replies](https://docs.slack.dev/reference/methods/conversations.replies/)
- [chat.postMessage](https://docs.slack.dev/reference/methods/chat.postMessage/)
- [files.getUploadURLExternal](https://docs.slack.dev/reference/methods/files.getUploadURLExternal/)
- [files.completeUploadExternal](https://docs.slack.dev/reference/methods/files.completeUploadExternal/)
- [Rate-limit policy clarification](https://docs.slack.dev/changelog/2025/06/03/rate-limits-clarity/)

Use the current method documentation for scopes. Older Slack pages describe different bot-token restrictions for replies; real workspace acceptance must verify the configured token's access.
