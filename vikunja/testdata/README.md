# Fixture provenance

`documented-wire.json` is synthetic, adapted from official API v2 examples and the OpenAPI schema served by a released Vikunja 2.7.1 instance. Artificial IDs, HTML, false/zero fields and IDs above 2^53 exercise request bytes and generated Sokel input binding. These examples are not live recordings.

`local-service-captures.json` contains actual request/response captures from a disposable local Vikunja 2.7.1 community container on 2026-11-04. It used an empty SQLite database, a newly created test-only user and scoped token. The live test created a unique project, exercised all nine operations, created two tasks and a comment, verified PATCH false/zero/date clearing and removed its project. Authentication headers are not captured; response JSON may contain the isolated server's loopback schema URL and synthetic account metadata.

`recorded-project-page.json` is an actual response from the first live run: it contains the ordinary Inbox and the negative-ID My Open Tasks saved filter while total=1. It exposed the incorrect assumption that every project entry counts toward pagination. `TestRecordedVirtualProjectPagination` failed before the production correction and passes afterward. Tagged upstream source confirms that virtual projects are appended separately: https://github.com/go-vikunja/vikunja/blob/v2.7.1/pkg/models/project.go.

`TestLocalServiceAcceptance` passed with the race detector on the isolated server. `TestCapturedServiceResponses` replays the actual captures offline. No production service, account or user data was used. The gated live test is skipped by default and must only target a fresh disposable loopback instance.
