# ntfy plugin

Independent Go module using the public Sokel SDK v0.7.7. Read [the usage guide](docs/ntfy.md) or [简体中文](docs/ntfy.zh-CN.md) for deployment and operation semantics.

## Scope and design

Three operations: `health`, `publish`, `poll`. Target: ntfy HTTP API, locally verified with 2.37.1. ntfy is self-hostable; the upstream core is dual-licensed Apache-2.0/GPL-2.0. The plugin calls an independently operated service and does not bundle its implementation.

Deployment URL comes from `SOKEL_NTFY_URL`; only the optional bearer token belongs in credentials. JSON publication goes to the service root, not the topic path. Every publication sets `Firebase: no`. Optional cache=false remains false through generated binding. No retry, forwarding, subscription stream, attachment fetch or hidden public endpoint is present. Basic authentication is not implemented in this version.

Polling reads NDJSON with a JSON decoder, not a line scanner. The full response is bounded to 8 MiB; message count limits expose continuation without advancing past undispatched messages. Cursors are topic-bound and retain at most 4096 IDs in the boundary second. Truncated replay responses, malformed cursors and repeated IDs within one response fail. Empty pages retain their cursor. Real acceptance found that ntfy intentionally replays earlier same-second messages for ID markers. Continuation therefore uses an opaque timestamp/seen-ID cursor; timestamp-window replay and per-second deduplication prevent two-message loops. It is best-effort: late messages older than the advanced boundary are outside the guarantee. Device delivery and durable event processing are separate concerns.

Both transport and protocol tests verify actual outgoing HTTP requests and the SDK-generated dispatch path. Private HTTP services are supported intentionally. Redirects are rejected, TLS checks remain enabled, responses are bounded, and raw transport errors or provider bodies are not surfaced in runtime errors.

## Validation and development

```sh
GOWORK=off go generate .
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
GOWORK=off go build -o /tmp/sokel-plugin-ntfy .
sokel-gen check .
```

Never edit generated `zz_*.go`. Update schema, regenerate with v0.7.7, implement the typed handler, and keep the source manifest aligned with the real SDK export. English is the source language. `locales/zh-CN.json` includes every supported UI string and the exact full English guide mapped to the full Chinese guide. SDK v0.7.7 does not extract deployment environment descriptions as translation keys; the same configuration is explained in both guides.

`SOKEL_TEST_NTFY_URL=http://127.0.0.1:18081 go test -run TestLocalServiceAcceptance -v .` opts into a disposable local server test and writes synthetic topics there. Leave it unset for ordinary unit tests. Real raw response fixtures came from an isolated official 2.37.1 container; see [fixture provenance](testdata/README.md). There was no connection to a production account or platform installation.


## Protocol sources

- [HTTP publication](https://docs.ntfy.sh/publish/)
- [Polling, replay limits and message format](https://docs.ntfy.sh/subscribe/api/)
- [Self-hosting configuration and iOS behavior](https://docs.ntfy.sh/config/)
- [Same-second replay semantics](https://github.com/binwiederhier/ntfy/blob/v2.37.1/message/cache.go#L287)
- [Upstream source and license](https://github.com/binwiederhier/ntfy)
