# Fixture provenance

`local-service-captures.json` contains actual sanitized HTTP request paths and response bodies from a disposable Miniflux 2.3.5 container on 2026-11-04, with a separate PostgreSQL instance and a private synthetic three-entry RSS feed. Authentication headers are not recorded. Account identity, feed URLs, article content and category names are disposable test values. Empty password/cookie fields in upstream responses are preserved to verify that the plugin's output projection excludes them; no real credentials or user content are present.

The captures include omitted/true/false bookmark filters, status updates, user identity, categories, feeds and a single stored entry. `TestCapturedServiceResponses` replays supported GET response shapes offline and checks private fields are absent from plugin output. The gated live test exercises all nine generated operation dispatchers, including cursor continuation and mutation semantics; the capture replay is not a replacement for live acceptance.

Other examples in the test source are synthetic adaptations of the [official API](https://miniflux.app/docs/api.html), including IDs above 2^53 and malformed responses. Local acceptance does not establish production deployment or image publication.
