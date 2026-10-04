# gitlab — GitLab plugin (first-party built-in catalog)

18 operations + 5 kinds of polled events covering four domains: repository (read/write
files/branches/commits), MR (create/list/comment/merge), Issue, CI/CD (pipelines/trigger/job
logs), plus `call` as a catch-all and `health_check` (`/user`).
Works with both self-hosted CE and gitlab.com (set `base_url` in the credential). User guide:
[docs/gitlab.md](docs/gitlab.md).

## Event source (events.go)

Polling mode (GitLab has no long-polling/streaming): the Events API and failed pipelines each
have their own cursor, polled every 60s, scoped to the projects named in the credential's
`watch_projects`. **"MR merged" shows up in the Events API as `action=accepted`, not
`merged`** — looking for `merged` by intuition gets zero events; this is pinned by a test.
Failed pipelines aren't in the Events API at all (a gap in GitLab's own coverage), so they're
polled separately by `updated_after`. The first round only records position and doesn't
backfill; brief re-reports after a restart are deduped by the platform on `event_id`.

## Webhook (webhook.go)

The zero-latency version of events, received by the platform on our behalf (pick **either**
this or the polling source — enabling both for the same action triggers it twice): the user
generates a `/hooks/{token}` URL on the credential row and configures it into GitLab, and the
platform forwards it as a `__webhook__` frame to `handleWebhook` by token. Key points:

- **Signature verification is a plain string comparison**: GitLab sends the Secret token
  verbatim in the `X-Gitlab-Token` header (not HMAC), compared against the credential's
  `webhook_secret`; if the credential has none configured, verification is skipped (acceptable
  for self-hosted internal use, documented in the user guide).
- **Event mapping**: the four hook kinds Push/Merge Request/Issue/Pipeline map to the five
  existing event contracts; MR only recognizes `action=open/merge`, Pipeline only recognizes
  `status=failed`.
- **`event_id` uses the `wh:` prefix**, a separate namespace from the polling source — the two
  sides' object ids live in different domains, and forcing a shared mapping would be fragile.
- **Unrecognized event types return 200**: GitLab sends a bunch of types depending on project
  configuration, and a non-2xx response would make it retry repeatedly.

## issue_commented: all three filters were pinned down by testing

Captured once against **real GitLab's /events output**; none of these three gotchas could be
figured out just by reading code:

| Assumption | Reality |
|---|---|
| `target_iid` is the Issue number | It's the **comment's own id** (observed: 1970, while the Issue is #4). The number is in `note.noteable_iid` |
| A Note event is always an Issue comment | Issue and MR comments go through the **same** event, distinguished by `note.noteable_type` |
| All notes are written by humans | Relabeling or reassigning also generates a note (`note.system=true`) — without this guard, touching a label would dispatch work and burn money |

The webhook side must also guard against `system`. The test fixtures use that same observed
shape.

## issue_commented: it forms loops, so the event must carry enough to break them

A bot replying to an Issue is also a comment -> triggers again -> infinite loop. The plugin
shouldn't hardcode "which account is the bot" (that's a deployment concern), so the event must
carry `author` and the raw `comment` text in full, letting the workflow filter it itself. The
docs call out two required lines of defense: filter by author + recognize a prefix.

Only comments with `noteable_type == "Issue"` are recognized: MR / commit / inline-diff
comments all go through the same Note Hook, and without distinguishing them, "saying something
on an MR" would also dispatch Issue work (pinned by a test). On the polling side, the Events
API records these as `target_type=Note` + `action_name="commented on"`, with the body in
`note.body`.

## issue_labeled: why it needs its own event

Adding a label is `action: "update"` on GitLab's side, not an open — `issue_opened` will never
fire for it again, so the most natural usage pattern of "create the Issue, take a look, then
decide whether to dispatch work" is entirely dead (reported by an actual user).

The two paths fill it differently: **webhook** diffs `changes.labels.previous/current` (removing
a label is also an update, so it must only return genuine additions, otherwise removing a label
would also dispatch work); **polling**'s Events API simply can't report label changes (it only
has a handful of actions like opened/closed), so it has to poll the issues list separately and
diff against the previous round's labels in memory, with the first round only recording
position. `added_labels` is sorted: it's built into `event_id`, and jitter would break dedup.

## Issue events' body/labels

Besides iid/title/author, `issue_opened` also carries `description` (the body), `labels`, and
`url` — downstream consumers that "act on the Issue" need the body, and labels are the only
practical gate for "who can trigger this". The two paths fill it differently: **webhook** reads
directly from `object_attributes` (labels appear in two places in the payload with different
shapes, and `hookLabels` tries both); **polling**'s Events API only gives the title, so it
fetches `/issues/:iid` details once more — a failed fetch just leaves the body empty, since the
event itself must still be delivered.

## raw can't be normalized, but downstream must still be able to tell the paths apart

`raw`'s shape is inherently different between the two paths: polling gives an Events API event
object, webhook gives a Hook request body. These two upstream payloads can't be normalized
(actually normalizing them would mean one extra API call per event).

So the approach is to stop pretending they're the same: every event that carries `raw` requires
`source` (`poll` / `webhook`), and the field description says outright that reading `raw`
requires checking `source` first. Tests pin that both paths fill it in without exception —
missing it on one path means the same `{{raw.xxx}}` silently returns nothing once the deployment
switches paths, with no error.

**Prefer the already-normalized fields** (description / labels / iid / author…) — `raw` is an
escape hatch, not the main path.

## Pagination surfacing for lists

The six list operations (projects/branches/commits/MR/Issue/pipelines) all return `total` and
`has_more`, taken from GitLab's `X-Total` / `X-Next-Page` response headers — `glCall` has always
returned the header, nobody was reading it before.

Only returning "items on this page" is a silent truncation: with exactly 50 items, the caller
has no way to tell whether that's really all of them or the list got cut off. **Decide whether
there's more by `has_more`, not `total`**: on very large result sets GitLab omits `X-Total`
(computing the total is too expensive), and then `total=0` while `has_more` is still true —
looking at `total` would make it look like there's nothing at all. Tests pin both shapes.

## Gotchas (read before touching the code)

- **project has two forms**: a numeric id passes through as-is; a path must be
  `url.PathEscape`'d (`backend/server` -> `backend%2Fserver`) into the path segment.
- **File paths are encoded as a whole** (`/` -> `%2F`) — a convention of the files endpoint;
  see `pathEscapeAll`.
- **GitLab returns 404, not 403, for projects you lack access to** (to prevent probing) — the
  404 error must mention this, otherwise the user will keep double-checking the path forever.
  Pinned by a test.
- **Writing a file goes through the commits endpoint** (not files): it first probes whether the
  file exists to decide `action` (create/update), and a single commit carries the commit
  message; multi-file writes go through `call`.
- **Pipeline variables are a `[{key,value}]` array**, not an object — passing an object directly
  gets a 400 from GitLab.
- The response body cap is 32MB (job logs can be large); tail truncation is done on the plugin
  side.

## Development

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```
