# x-free — cookie-authenticated X read/write plugin

Seven operations provide bounded search and user timelines, post/user lookup, text posting/replies,
deletion, and a read-only credential probe. This is a separate identity from the official OAuth `x`
plugin: existing workflows, credentials, and API-based deployments keep their existing contract.

## Upstream and compatibility

The implementation uses [TweetKit](https://github.com/nsozturk/tweetkit-x), MIT licensed, pinned to
[`d42503a936f80eb0af2e9856a0fbd392cc4a750c`](https://github.com/nsozturk/tweetkit-x/tree/d42503a936f80eb0af2e9856a0fbd392cc4a750c).
That revision includes the September 2026 homepage transaction-ID fix absent from its PyPI 0.3.0
release. This is a small upstream project, not an availability guarantee. No official API subscription
is required; requests still require a valid, authorized account session and may be limited by X.

The adapter checks HTTP status and GraphQL errors before upstream parsers can turn failures into
empty results or a successful delete. SearchTimeline uses POST and the corresponding transaction ID,
matching the [authenticated search fix](https://github.com/the-convocation/twitter-scraper/pull/191).
Transaction-ID bootstrap requests use the same per-call session so proxy and timeout configuration
also cover homepage and JavaScript requests. The resulting anonymous transaction generator is cached
in memory for five minutes, isolated by proxy, with at most eight entries. This avoids downloading
the public homepage and JavaScript for every operation; sessions, cookies, and responses containing
account data are never shared between operations. Failed bootstraps are not cached.

Sessions are isolated to one operation and closed afterward; credentials never fall back to an
upstream environment variable or cookie file. The synchronous library runs in a worker thread.
Cancellation cannot interrupt an HTTP request already executing in that thread, so request bounds
still apply and an interrupted write may have succeeded. Neither the plugin nor its HTTP adapter
automatically retries writes. Workflow authors must leave write-node retries disabled too.

Lists intentionally expose a bounded recent batch rather than a cursor: upstream public methods
consume their own page cursors and may truncate a page. Returning that cursor with a truncated list
would silently skip records. Scheduled workflows should deduplicate by post ID. Media upload, direct
messages, account pools, and event polling are outside this initial contract.

## Files

| File | Purpose |
| --- | --- |
| `manifest.yml` | Contract source: operations, credentials, typed output fields, and `brand:x` icon |
| `sokel_gen.py` | SDK-generated typed models and registration; do not edit by hand |
| `main.py` | Upstream adapter and operation handlers |
| `test_main.py` | Behavioral and mocked upstream regression tests |
| `docs/x-free.md`, `locales/en.json` | User instructions and English catalog translations |
| `requirements.txt` | Runtime SDK and immutable upstream revision |
| `Dockerfile`, `docker-compose.yml` | Standalone local deployment |

## Develop and run

```bash
python3.12 -m venv .venv
.venv/bin/pip install -r requirements.txt
go install github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen@v0.7.7
sokel-gen generate .
sokel-gen check .
.venv/bin/python -m unittest discover -v

# Import the exported manifest into the platform, then copy the new access-group token.
sokel-gen export yaml . > /tmp/x-free.yml
SOKEL_ENDPOINT=http://localhost:8088 SOKEL_TOKEN=skp_example .venv/bin/python main.py

# Or build locally; no published image is required.
SOKEL_ENDPOINT=http://host.docker.internal:8088 SOKEL_TOKEN=skp_example docker compose up -d --build
```

Keep the X cookies in platform credentials, separate from the `SOKEL_TOKEN` used to connect the
plugin. Read [the user guide](docs/x-free.md) for extracting cookies and configuring a proxy.

Tests use synthetic sessions and mocked upstream HTTP responses; they do not log in, post, or delete
real content. A live acceptance test needs an authorized test account: run the credential probe and
read operations first, then explicitly post a test message and delete that returned ID.

## Distribution integration

The source repository's `scripts/export-public.sh` exports committed Python plugins automatically.
The public mirror's `scripts/plugin-matrix.py` discovers this manifest, entry point, and Dockerfile;
the image context is `x-free/`. Contract edits require regeneration and matching English translations,
including the exact usage-document key. The existing `x` catalog entry must not be replaced.

A first public release is `x-free/v0.1.0` on the public mirror. Its release workflow builds the real
image, obtains its digest, and creates `sokel/x-free` through `scripts/registry-entry.py`. Only after
that catalog entry is admitted and published should the platform's registry pin be updated. Do not
add placeholder digests or claim an unpublished image is available in the marketplace.
