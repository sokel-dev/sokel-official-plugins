# GitLab

Connect GitLab to workflows: read and write repository files, create/review/merge MRs, open issues, inspect pipelines and failure logs,
and trigger builds. **Self-hosted CE and gitlab.com are both supported**; set the instance URL in the credential.

## 1. Prepare credentials

1. GitLab avatar → **Preferences → Access Tokens** → create a personal access token.
   Select `api`; `read_api` is sufficient for read-only scenarios.
2. In the credential, set **GitLab URL** to the self-hosted instance root, such as `https://git.example.com`.
   Leave it empty for gitlab.com. Paste the **Access token**.
3. A dedicated bot account is recommended. Assign project roles as needed: Reporter for reads,
   Developer for commits and MR creation, and Maintainer for merging protected branches.

For self-hosted instances with self-signed HTTPS certificates, install the CA in the plugin container's system trust store; otherwise connections fail.

## 1.3. Find information

**Search** finds code, issues, MRs, and commits. Leave project empty to search the whole instance, a direct starting point for an agent asking where a function is used.
Hits are normalized into one structure: code includes `path`, `line`, and `snippet`; issues include `iid` and `title`.
The canvas does not need separate drill-down paths for different search types.

> Global code search can be slow on large instances. Restrict the project when possible.

## 1.4. Let an agent review an MR

- **MR changes** returns changed files and each file's diff. **Use this for agent review**.
  Oversized file diffs are truncated and marked with `truncated`. **Truncation is per file, not across the whole response**,
  so a migration with tens of thousands of lines does not crowd out all other changes.
- **Update MR** / **Approve MR** changes labels, closes the MR, or casts an approval.
  Approval is cast **as the credential's account**; do not let an agent make decisions on someone else's behalf.
- **Inline MR comment** attaches review feedback to a file and line. Supply only the path, line number, and comment.
  The plugin retrieves the three positioning SHAs from the MR rather than asking users to enter base/start/head correctly on the canvas.
  For a deleted line, use Original file line number.
- **MR commented** lets a comment under an MR send the agent back to review it. It is distinct from an issue-comment event:
  GitLab sends the same Note event type, and the plugin routes by `noteable_type`.

## 1.5. Let an agent finish the process

Close the loop after handling work; otherwise, the same issue can trigger repeatedly:

- **Update issue** closes, adds/removes labels, or assigns it. **After handling an issue, remove the trigger label, such as `claude`, and add `cc-done`**.
  This is the cleanest way to prevent repeat triggers, more reliable than maintaining a separate “already handled” record in the workflow.
  Fill only fields to change; empty fields remain unchanged.
- **Update comment**: for long tasks, **post “Processing…” first, then update that same comment when done** instead of adding another.
  This keeps the thread readable and reduces opportunities for comment-trigger loops. Use `note_id` from Comment on issue.

## 2. Operation reference

- **The project field accepts two forms**: a numeric ID or `group/name`, the repository URL path after the hostname.
  All operations follow this rule.
- **Write file (commit)** creates a missing file or updates an existing one automatically. With
  `start_branch`, one workflow can create a branch, commit, and open an MR.
- **Merge MR** is rejected by GitLab if CI has not passed or conflicts exist; the error explains why.
  Enable Merge when pipeline succeeds to let GitLab merge automatically.
- **Job logs**: retrieve the tail of a failed job's log → ask an LLM to summarize the cause → notify a group, completing the CI troubleshooting loop.
- **Generic call** handles APIs without typed operations, including releases, tags, webhooks, and membership management.
  Follow the API documentation and omit the `/api/v4` prefix from the path.

> **Use `has_more` for pagination, not the number of returned items**. Lists return `total` and `has_more`.
> Exactly 50 items does not mean the end. GitLab omits totals for very large result sets; `total=0` then, but `has_more` remains authoritative.

## 3. Event triggers

Choose according to your latency needs:

**① Plugin event source: polling, about one-minute latency, simplest setup.** Set Projects to watch in the credential
to comma-separated paths such as `backend/server,infra/deploy`. It starts after saving and provides five event types:
**Push / MR opened / MR merged / Issue opened / Pipeline failed**. Add an Event trigger node and select GitLab.
Polling starts only when projects are configured; using ordinary operations alone incurs no polling overhead.

**② Plugin webhook endpoint: immediate delivery with typed events, recommended.** Click Webhook on the credential row to generate
a URL, then paste it in the GitLab project's Settings → Webhooks. Set Secret token to the credential's
webhook_secret and select Push / Merge request / Issues / Pipeline events.
It uses **the same typed event contract as polling**: Push, MR opened, MR merged, Issue opened, and Pipeline failed.
The canvas does not need to distinguish the origin. **Choose either webhooks or polling**; enabling both triggers twice for the same action.

**For “create an issue, then add a label,” use `issue_labeled`**. Adding a label is an *update* in GitLab,
not an open event, so `issue_opened` **does not fire again**. To review an issue before assigning work,
listen for `issue_labeled`, which includes `added_labels` for this change and `labels` for all current labels.
**Decide whether to assign work using `added_labels`**. The full `labels` array may still contain claude even when an unrelated label was added.
Both routes support this: webhooks read `changes.labels`; polling compares states itself when no webhook is configured.
The first poll only establishes a baseline, without turning existing labeled issues into runs.

**Continue work from an issue comment** by listening for `issue_commented`. It includes `comment`, the new comment text,
and `author`, the author.

> ⚠️ **This event naturally creates loops**: a bot reply is also a comment, which triggers another reply, indefinitely.
> The workflow **must** include two safeguards: filter out the bot account with a condition on `author`,
> and accept only comments beginning with a prefix such as `/cc`, checked through `comment`. Omitting either can lead to continuing charges until someone notices.

For multi-turn work, retrieve the previous session ID with **List worktrees** in the Claude Code plugin.
Find the worktree named `cc/issue-<number>` and use its `last_session` as the input to Continue task.
The session remembers earlier work; no additional storage is needed.

**Issue events include body and labels**. `description` contains the issue body, which is usually the input downstream work needs;
a title is often only one sentence. `labels` is an array. **Use a label as a gate**: handling only issues tagged with a label
such as `claude` is safer than running for every issue. The polling source makes an extra detail request to populate these fields.

**③ Workflow-level webhook trigger**: receive raw JSON at the workflow entry point and navigate the fields yourself.
Use this only for event types the plugin does not cover.

## 4. Common combinations

- **CI failure alerts**: Pipeline failed event → List jobs → Job logs → LLM summary → Feishu or SMS.
- **Automatic issue creation**: monitoring detects a problem → Create issue with labels and assignment → Comment with updates.
- **Configuration changes**: Write file using `start_branch` → Create MR → Human approval node → Merge MR.
- **Change monitoring**: schedule → List commits with since set to the previous check time → New commits → Notify or trigger a build.

## 5. Troubleshooting

| Error | Resolution |
|---|---|
| GitLab rejects the token (401) | It may be expired or revoked. Generate another token with at least the api scope. |
| Insufficient token permissions (403) | The account must be a project member with an adequate role; merging requires Maintainer. |
| Not found (404) | **GitLab also returns 404 for unauthorized projects** to prevent discovery. First verify the token's account belongs to the project, then check the path. |
| Connection failed | For self-hosted instances, ensure the plugin container can reach the instance. Install self-signed CAs in the system trust store. |
| Merge rejected (405) | CI, conflicts, or branch protection may block it. Check the MR page for the cause. |
