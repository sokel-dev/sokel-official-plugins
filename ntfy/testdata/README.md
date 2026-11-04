# Fixture provenance

`live-health.json`, `live-publish.json` and `live-poll.ndjson` are unmodified response bodies captured from an isolated `binwiederhier/ntfy:v2.37.1` container on 2026-11-04 Asia/Shanghai. Requests used a synthetic topic `sokel-wave6-synthetic`, no authentication, no subscribers, `Firebase: no`, and no configured upstream relay. Random message IDs and timestamps are real outputs for these synthetic requests. No user account data or secret is present.

The inline `documentedMessage` in tests is the official [subscription API](https://docs.ntfy.sh/subscribe/api/) example. Malformed responses and transport/validation edge cases are deliberately synthetic. Local acceptance uses a disposable loopback server and checks same-second replay deduplication and empty tail pages. These tests do not establish real device delivery.
