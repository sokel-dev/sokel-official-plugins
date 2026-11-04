# GitHub

Bring GitHub project maintenance into workflows. **You can assemble the behaviors of many kinds of GitHub bots**:
automatic labeling and triage, stale-issue cleanup, PR review results, ChatOps slash commands, release notifications, and project-board transitions.
Both github.com and GitHub Enterprise Server are supported.

## Configure credentials

### Access token (required)

GitHub avatar → **Settings → Developer settings → Personal access tokens**. Either token type works:

| Token type | Permissions to select | Notes |
|---|---|---|
| **Classic token** | `repo` for private repositories, `workflow` to trigger Actions, and `project` for project boards. | One key covers all repositories; simplest to configure. |
| **Fine-grained token** | Grant access per repository, with at least read/write Contents, Issues, and Pull requests. | More secure. **Writing check results also requires Commit statuses and Checks**; project boards require Projects. |

> A dedicated bot account is recommended. Comments show the account that owns the token.
> If you use your own account, colleagues may think you are replying to issues in the middle of the night.

⚠️ **Fine-grained tokens do not report their own permission scopes**. An empty Token scopes field in a credential test is normal
and does not mean the token has no permissions.

### GitHub Enterprise Server

Set GitHub URL to the instance root, such as `https://github.example.com`. **Do not include `/api/v3`**; the plugin adds it.
If you include it anyway, the plugin removes the duplicate.

### Webhook Secret (optional, needed only for webhooks)

See the next section.

### Repositories to watch (optional, needed only for polling)

See the next section.

## Events: choose one of two routes

To trigger a workflow when something happens, such as an issue being opened, there are two routes. **Choose only one**:
enabling both causes the same action to trigger once through each route.

### Route 1: Webhooks (immediate, recommended)

1. Click Webhook on the platform credential row and copy the generated URL.
2. In the GitHub repository, open **Settings → Webhooks → Add webhook**:
   - **Payload URL**: paste the generated URL.
   - **Content type**: select `application/json`, **not** `x-www-form-urlencoded`; the plugin cannot decode the latter.
   - **Secret**: choose a string and **enter the same string in the platform credential's Webhook Secret field**.
   - **Which events**: choose "Let me select individual events" and select those you need from the table below.
3. After saving, GitHub immediately sends a ping. A green check next to that delivery means the connection works.

Events to select:

| Desired event | GitHub event selection |
|---|---|
| New commits | Pushes |
| PR opened, merged, or labeled | Pull requests |
| PR reviewed | Pull request reviews |
| Issue opened or labeled | Issues |
| New comment on an issue or PR | Issue comments |
| New release | Releases |
| Workflow failed | Workflow runs |

> **Always configure a Secret**. Otherwise, anyone who knows the URL can inject fake events into your workflows.
> The plugin skips signature verification when the platform credential has no Secret, an option intended for internal deployments. Do not use that configuration for public repositories.

### Route 2: Polling (no changes to GitHub settings)

Set Repositories to watch in the credential to a comma-separated list such as `sokel-dev/sokel,acme/infra`. Polling starts only when this is filled.

- Latency is about one minute. GitHub's activity feed itself is cached for roughly 60 seconds, so faster polling does not help.
- **The plugin does not replay historical events at startup**. The first pass records a cursor; delivery starts from the second pass.
  After configuring it, wait a minute before creating an action on GitHub to verify the connection.
- Useful when you lack repository administrator permissions or an internal GitHub instance cannot conveniently call back to the platform.

## Bots you can build

| Goal | Building blocks |
|---|---|
| **Automatic triage**: label, assign, and add new issues to a board | Issue opened trigger → Add labels / Assign / Add to project. |
| **Stale cleanup**: warn, then close inactive issues | Schedule → List issues ordered by updated ascending → Comment → Update issue to closed with reason `not_planned`. |
| **PR check results**: run checks and report on the PR | PR opened trigger → List changed files → Your checks → Set commit status using the event's `head_sha`. |
| **Inline review feedback** | Submit review, supplying file and line in `comments`. |
| **ChatOps**: `/deploy` in a comment | New PR comment trigger → Check `author_association` and `is_bot` → React 👀 → Perform work → React 🚀. |
| **Automatic merging** after checks pass | PR reviewed trigger → Conditions → Merge PR, **setting `expect_sha` to `head_sha`**. |
| **PR-opening bot** for dependency updates or backports | Create branch → Write file → Open PR → Request review. |
| **Release notifications** | Release published trigger → Send a message. |
| **CI watchdog** | Workflow failed trigger → List run jobs → Get job logs → Open an issue or notify. |

## Common stumbling blocks

- **GitHub issue lists also contain PRs**. This is how GitHub's Issues API works. The plugin **filters PRs out by default**;
  `dropped_prs` reports how many were removed. Enable Include PRs to retain them.
- **`closed` does not mean `merged`**. A PR can be closed without merging. PR merged is a separate event;
  do not substitute PR closed.
- **Use Comment for ordinary PR comments**, just as for issues: GitHub shares the comments API.
  Only comments **on a specific diff line** use Submit review.
- **An inline comment's line number must belong to the current diff**. Otherwise, the entire review is rejected, rather than just that comment being ignored.
  If uncertain, use List changed files with `with_patch` enabled to inspect the diff.
- **Set commit statuses on the PR's `head_sha`**, not the merge commit SHA. A status written to the wrong commit will not appear on the PR page.
- **Create check run requires a GitHub App or a fine-grained token with Checks permission**. A classic PAT cannot create one.
  With a classic token, use Set commit status instead; it provides similar results without annotations or a Markdown summary.
- **Trigger workflow does not return a run ID**. GitHub returns an empty response. Wait a few seconds, then query Workflow runs.
- **ChatOps must check `is_bot`**. Otherwise, a bot can respond to its own comments and exhaust the rate limit in minutes.
  Also check `author_association` for `OWNER`, `MEMBER`, or `COLLABORATOR`;
  without it, any passerby could command your bot through slash commands.
- **Projects require an account name, not a repository name**. Projects V2 belong to users or organizations.
  Enable Is organization for an organization project. If omitted, the plugin tries both, costing one extra request.

## Limits

Authenticated requests allow **5000 calls per hour**; GHES limits are configured by administrators. Credential tests report the remaining allowance.

When rate-limited, the plugin identifies the limit and how long to wait. **This differs from insufficient permissions**:
both can return 403, but the first requires waiting and the second requires changing token permissions.

Many writes in a short period can also trigger a secondary rate limit. The plugin then suggests adding waits between nodes.
