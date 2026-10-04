# github plugin

> For **people modifying this plugin**. End users see `docs/github.md` (reported during the
> registration handshake, shown in the UI's "Usage" tab).

GitHub project-maintenance automation, 43 operations + 10 events. The benchmark is the
various **GitHub bots** (the Probot family, Renovate, stale-bot, reviewdog, Mergify,
release-drafter, ChatOps), and the bar is "can this set of operations rebuild those bots",
not "is the REST API fully wrapped".

## Why it looks like this

Every line here maps to a real gotcha — read before touching anything.

### 1. `/issues` returns PRs too, so they're dropped by default

GitHub's Issues and PRs share a number space, and `GET /issues` **returns PRs along with
issues**, with no error. Copying GitLab's mental model for listing issues gets you a pile of
PRs mixed in.

`IssuesList` drops them by default (`include_prs` turns this off explicitly), and the output
includes `dropped_prs` so you can see how many were dropped, with each issue also carrying
`is_pr`. The criterion is whether the payload has a `pull_request` key —
not the title, not the URL (`isPullRequest`).

⚠️ The filtering happens **after pagination**, so `count` is the post-filter count while
`has_more` reflects upstream. This isn't a bug: paging needs to know whether upstream has a
next page.

### 2. Webhook signature verification is HMAC-SHA256, not plaintext comparison

GitLab puts the secret in plaintext in a header for comparison; GitHub uses
**HMAC-SHA256 over the entire raw request body**
(`X-Hub-Signature-256: sha256=<hex>`). Copying the GitLab approach will never verify
correctly.

Three things that must not be loosened:

- Compare with `hmac.Equal` (constant-time), not `==`;
- What gets signed must be the **raw bytes** — if the body is decoded and re-encoded as JSON
  even once, the signature stops matching;
- A secret is configured but the request carries no signature → **reject**. Letting it
  through is pretending verification happened.

### 3. `event_id` uses `X-GitHub-Delivery` directly

GitHub assigns each delivery a UUID, and **reuses the same one on redelivery** — which is
exactly the semantics the platform needs for deduplication. No need to build a fragile key
yourself out of "object id + timestamp" (which is what the GitLab side has to do).

### 4. Writing a file requires probing for the blob sha first

The Contents API returns 422 when updating an existing file without a `sha`, and the error
text doesn't say "you're missing the sha". `opFileWrite` does a get-then-put internally,
trading one extra round trip for eliminating a whole class of confusing failures.

### 5. 403 has two completely different meanings

Insufficient permissions and being rate-limited both show up as 403. They're distinguished by
whether `X-RateLimit-Remaining` is zero. Conflating the two means the user will keep checking
their token scopes over "insufficient permissions" when all they needed to do was wait a few
minutes — this is the single most time-wasting source of confusion when integrating with
GitHub. `ghErr` separates the two, and also specifically recognizes the "secondary rate
limit".

### 6. The bot-feedback surface is this plugin's focus

`commit_status_create` / `check_run_create` / `reaction_add` are what distinguish
"a GitHub bot" from "a GitHub API client": a bot needs to be able to write its conclusion
**back** onto the PR page. Without these, review-type workflows can only post a comment, and
comments don't participate in branch-protection required checks.

### 7. Projects V2 is GraphQL-only

The REST API for classic Projects has been retired. The four board operations go through
GraphQL, everything else through REST, with the boundary drawn in `client.go` — invisible at
the contract level (and it should be invisible there, since callers shouldn't care about the
transport).

Two GraphQL-specific gotchas:

- **Organizations and individual users use different queries**, and picking the wrong one
  **doesn't error** — it returns null data, which looks like "this board is empty". So when
  `is_org` is left blank, both are tried.
- **GraphQL errors don't go through HTTP status codes**: a failed query still returns 200,
  with the error in the response body's `errors` field. If you don't parse `errors`, a
  misspelled field name will look like "it returned empty data".
- GHES's GraphQL endpoint is `/api/graphql`, **not under REST's `/api/v3`**. Shaping it like
  REST gets you a 404, and a 404 looks like "you don't have permission", which makes it hard
  to think of "the endpoint is wrong".

### 8. The polling source doesn't fire on its first round

The activity feed returns the most recent 30 items at once. If the first round fired them all,
the plugin would dump thirty stale events into workflows the moment it starts. So the first
round only records the cursor (`primed`), and events only start flowing from the second round
on. The failed-workflow feed follows the same rule.

### 9. ChatOps must be able to recognize the bot itself

`is_bot` shows up on comment-type events. Without blocking on it, a bot replying to its own
comment loops forever, and at one API call per round it can burn through the rate-limit quota
within minutes. Both criteria matter: `user.type == "Bot"` is authoritative, and `login`
ending in `[bot]` is the fallback for older payloads that lack `type`.


### Why the polling path had no tests before

It wasn't an oversight — it was the signature. The three poll functions originally took
`sokel.SourceCtx` — a **struct** — which can't be faked, so the entire polling path had zero
test coverage. The webhook side had test coverage purely by luck: `sokel.WebhookCtx` happens
to be an **alias** for `plugin.SourceCtx`, which is an interface.

Now all three poll functions take the interface `plugin.SourceCtx` (`runEvents` still takes
the struct, since that's required by `RegisterSource`). **Follow this pattern for any new
event source**: the top-level function takes the struct, the function doing the actual work
takes the interface.

Seven "doesn't error but is useless" points on the polling path now have tests guarding them:
not firing on the first round, oldest-to-newest ordering, filtering out already-seen items,
comparing event ids numerically (a carry where `"9" > "10"` would **permanently silence**
polling with no error), the activity-feed payload shape (which **differs** from the webhook
one), deduping failed workflows by run_id+attempt, and not letting a 404 from Actions being
disabled take down the entire round.

## Files

| File | What it is |
|---|---|
| `schema/schema.go` | Top-level comments + shared fields + repo-domain contract |
| `schema/issue.go` | Issue-domain contract |
| `schema/pr.go` | PR domain + bot-feedback-surface contract |
| `schema/actions.go` | Actions + releases + repo-housekeeping contract |
| `schema/projects.go` | Boards + search + fallbacks + health-check contract |
| `schema/events.go` | Event contract + credential contract |
| `zz_*.go` | **Generated, do not hand-edit** (`sokel-gen`) |
| `client.go` | REST + GraphQL call layer, error translation, parsing helpers |
| `ops_*.go` | Per-domain handlers |
| `webhook.go` | Platform-received webhook (signature verification + dispatch) |
| `events.go` | Polling event source |
| `main.go` | Wiring |
| `docs/github.md` | The **user-facing** manual, `//go:embed`-ed into the binary and reported at handshake |

## Development

```bash
go generate ./...            # must be run after changing schema/
go build ./... && go vet ./...
go test ./...
```

If you change `schema/` without regenerating, `sokel-gen check .` will fail — CI blocks on
that.

### Running locally

```bash
SOKEL_ENDPOINT=http://localhost:8088 SOKEL_TOKEN=skp_xxx go run .
```

## Integration testing against real GitHub

A fake upstream can only validate our own wiring logic. There are four categories it
inherently can't validate, and they're also the easiest to get wrong: whether a GraphQL query
string is actually correct (the fake upstream doesn't parse it at all, so the four board
operations have **zero coverage** in fake-upstream tests), whether `/issues` actually returns
PRs, what the Link header and rate-limit headers actually look like, and whether a token's
permissions are sufficient.

### 1. The API half: `live_test.go`

```bash
# the read-only batch, any token works
GITHUB_TOKEN=ghp_xxx go test -run Live -v ./...

# add write operations (will actually create an Issue/branch/PR) — **make sure to point at a throwaway repo**
GITHUB_TOKEN=ghp_xxx GITHUB_WRITE_REPO=you/scratch go test -run Live -v ./...

# boards (the token needs project permissions)
GITHUB_TOKEN=ghp_xxx GITHUB_PROJECT_OWNER=you go test -run LiveProject -v ./...

# GHES
GITHUB_TOKEN=xxx GITHUB_BASE_URL=https://github.example.com go test -run Live -v ./...
```

If `GITHUB_TOKEN` isn't set, the whole batch is skipped, so CI and other people's machines
won't turn red because of it.

`TestLivePullRequestFlow` leaves behind a branch and a file (the PR is closed automatically);
delete them manually if needed.

### 2. The webhook half: `webhook-replay.sh`

Webhooks go into the platform over HTTP, so Go unit tests can't cover them, and getting real
GitHub to hit your local machine requires a tunnel. This script swaps "GitHub sends it" for a
local curl, **computing the real signature**, so signature verification, dispatch, and
deduplication all genuinely run:

```bash
./webhook-replay.sh http://localhost:8088/hooks/whk_xxx s3cret issues
```

Fire it twice in one go (correct signature + wrong signature), then compare in the platform's
"Plugin Details → Webhook" tab: the 200 one should have a nonzero triggered-event count, the
401 one should have zero. Fire it again with the same delivery id — it should be deduplicated.

### 3. End-to-end: real GitHub → real platform

Only do this once the first two steps pass, otherwise the surface to debug is too large.

```bash
# let GitHub reach your local machine (pick one)
cloudflared tunnel --url http://localhost:8088
```

Once you have a public address, fill in `<public-address>/hooks/<token-given-by-the-platform>`
under the GitHub repo's Settings → Webhooks, set Content type to `application/json`, and match
the Secret to the one in the credential. After saving, GitHub immediately sends a ping; a green
check on the config page means it's connected. Then open an issue in the repo and check whether
the workflow fired.

**If there's no public address, use the polling source instead**: fill in which repos the
credential should watch for events, wait a minute (the first round only records the cursor and
doesn't backfill history), then go perform an action in the repo.

### Things to check first during integration testing

| Symptom | Likely cause |
|---|---|
| All webhooks return 401 | Secrets don't match on both sides; or Content type is set to `x-www-form-urlencoded` |
| Webhook returns 200 but the workflow didn't fire | The event type isn't checked on the GitHub side; or the trigger is bound to a different event |
| The same action fires twice | Webhook and the polling source are both enabled (the docs say to pick one) |
| The polling source is configured but nothing happens | First round only records the cursor — wait a minute, then perform a new action |
| A written-back status doesn't show up on the PR page | Wrong sha was used — it should be the PR's `head_sha`, not the merge commit |
| A board operation returns "empty list" instead of an error | Org vs. individual was guessed wrong (try explicitly turning on "is organization") |

## What else to touch after changing the contract

1. `go generate ./...` to regenerate `zz_*.go`;
2. Added an **operation** → add a line `OnXxx(p, opXxx)` in `main.go`, otherwise it exists in
   the contract but calling it says "not found";
3. Added an **event** → wire both paths: `webhook.go` and `events.go`.
   Wiring only one shows up as "it fires via webhook but not via polling", or vice versa;
4. Changed user-visible behavior → update `docs/github.md` accordingly;
5. The one-line platform-side description is in `server/internal/api/seed.go`, and the icon is
   in the `BRANDS` table in `web/src/features/plugin/components/PluginIcon.tsx` and the `icons`
   table in seed.

## Not done / out of scope

- **GitHub App identity**. Only personal access tokens are supported right now. An App would
  get a higher rate-limit quota, could create check runs, and could appear on PRs as an
  "app" identity — but handling JWT signing and installation-token rotation is a different
  scale of work. `check_run_create` returns 403 under a classic PAT, and the error message
  says so.
- **Discussions**. GraphQL only, with a shape very different from Issues; no demand has driven
  this yet.
- **Organization-level operations** (member management, teams, org settings). This plugin is
  scoped to "maintaining a project", not "administering an organization".
- **Writing branch protection**. Only reads are implemented (sufficient for compliance
  checks). The write payload is large, and one mistake could loosen protection on the main
  branch — the risk doesn't match the payoff. Use the generic-call operation if you need to
  change it, since that's explicit.
- **Cursor persistence for the polling source**. The cursor lives in memory, so after a plugin
  restart the first round re-primes (i.e. doesn't backfill). Events that occur during the
  restart window are missed — this is one of the reasons to prefer the webhook path, and the
  docs say so.
