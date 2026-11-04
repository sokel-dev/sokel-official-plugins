# Fixture provenance

`local-service-captures.json` records actual generated operation requests and responses from an isolated linkding 1.47.0 service on 2026-11-04. The account, bookmark URLs, tags and text were disposable synthetic data; authentication headers are excluded. Captures cover connection checking, same-URL save/update, active/archived lists, pagination, get, archive/unarchive, tags and explicit deletion.

Background scraping, favicons, previews and snapshot integration were disabled in this instance. Upstream `web_archive_snapshot_url` can be a locally generated fallback link: its presence does not establish an external request or completed snapshot. The plugin's output projection excludes it.

`captured_test.go` replays routes through generated dispatch. It replaces only the recorded loopback service origin in response URLs with the fake transport origin, allowing continuation checks; other fields are preserved. Separate wire and boundary tests use synthetic examples adapted from the [1.47.0 official API](https://github.com/sissbruecker/linkding/blob/v1.47.0/docs/src/content/docs/api.md), including exact large IDs and malformed continuations. The fake transport avoids requiring listening sockets for offline tests.

Live acceptance and replay do not establish production deployment or a published image.
