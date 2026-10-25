# x-free — cookie-authenticated X read/write plugin

Eleven operations provide bounded search and user timelines, single-page X List discovery and timelines,
List member management, post/user lookup, text posting/replies, deletion, and a read-only credential probe.
The v0.2.0 contract adds four List operations: `my_lists`, `list_timeline`, `add_list_member`, and
`remove_list_member`. This plugin is a separate identity from the official OAuth `x` plugin: existing
workflows, credentials, and API-based deployments keep their existing contract.

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

Search and user timelines expose bounded recent batches without pagination cursors: their upstream
public methods consume their own cursors and may truncate a page. Scheduled workflows should
deduplicate those results by post ID.

`my_lists` reads one page of Lists shown on the authenticated account's X List management page. These
can include created and followed Lists; the result is not restricted to Lists owned by the account.
Select a List by name and use its returned `id` in `list_timeline`, so knowing the ID in advance is not
required. Each List includes its name, description, visibility when available, member and subscriber
counts, owner, and URL. Use `next_cursor` and `has_more` to browse additional pages.

`list_timeline` reads one page from an X List, accepting a numeric List ID or an
`https://x.com/i/lists/<id>` link (`twitter.com` links are also accepted). `limit` is a requested page
size of 1–100, default 20; X can return more or fewer posts. The adapter returns the whole parsed page
with `next_cursor` and `has_more`, without truncating it to `limit`, so a page boundary does not skip
posts. An empty page with a cursor can still have more pages. `my_lists` and `list_timeline` are
read-only; use an account authorized to read any private List.

`add_list_member` and `remove_list_member` add or remove one account by username, optionally prefixed
with `@`, in a List the authenticated account can manage. These operations change List membership;
they do not follow or unfollow a List. Select a List from `my_lists` or supply its link or ID directly.
The returned `changed` flag means X accepted the operation, not that membership necessarily changed
for the first time. Failed writes are not automatically retried, and there is no automatic bulk member
management. After a timeout or dropped connection, check List membership on X before retrying.

For scheduled collection, start each run with an empty cursor and use returned cursors only to page
backward during that run. Upsert posts with a composite `(list_id, post_id)` key, keeping both IDs as
strings. Check stored IDs and allow a small overlap after reaching the last completed run's boundary;
reposts and out-of-order results make comparison with only the largest ID insufficient. Set a maximum
page count and failure alerts. If that bound is reached or a request fails (including 401 or 429), preserve
already stored rows but do not advance the completed checkpoint or treat partial writes as a completed
boundary. On the first run, establish a baseline or choose an explicit lookback range. A 10–15 minute
timer is a configuration starting point,
not a guarantee against rate limits. See [the user guide](docs/x-free.md) for the workflow sequence.

X web endpoints can change; this contract does not guarantee zero missed posts or complete historical
coverage. Media upload, direct messages, account pools, and built-in event polling are outside it.

## Files

| File | Purpose |
| --- | --- |
| `manifest.yml` | Contract source: operations, credentials, typed output fields, and `brand:x` icon |
| `sokel_gen.py` | SDK-generated typed models and registration; do not edit by hand |
| `main.py` | Upstream adapter and operation handlers |
| `test_*.py` | Behavioral and mocked upstream regression tests |
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

Tests use synthetic sessions and mocked upstream HTTP responses; they do not log in, post, delete real
content, or change real List members. A live acceptance test needs an authorized test account: run the
credential probe and read operations first. Posting, deletion, and List member changes require an
explicitly authorized write test.

## Distribution integration

The source repository's `scripts/export-public.sh` exports committed Python plugins automatically.
The public mirror's `scripts/plugin-matrix.py` discovers this manifest, entry point, and Dockerfile;
the image context is `x-free/`. Contract edits require regeneration and matching English translations,
including the exact usage-document key. The existing `x` catalog entry must not be replaced.

For the `x-free/v0.2.0` tag, the public mirror's Release workflow builds the distributable image,
records its digest, and prepares the `sokel/x-free` catalog entry through `scripts/registry-entry.py`.
The catalog entry pins the image to that build's digest and must pass catalog admission before
publication. Update the platform's registry pin after the catalog entry is published. The workflow's
image reference and catalog entry determine the version available for installation.
