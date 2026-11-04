# Claude Code

Connect **Claude Code installed on the local machine** to a workflow: provide a GitLab project and a task.
It edits code in a real worktree, streams progress, and passes its conclusion and list of changes to downstream nodes.

## Deployment prerequisites

This plugin **must run on a machine meeting all three requirements**: access to your GitLab instance (self-hosted instances are often on an internal network),
`claude` installed, and enough disk space for the repository. Like the Kubernetes plugin, it cannot be deployed on just any machine.

## Configure credentials

| Field | What to enter |
|---|---|
| Anthropic API Key | **Required**. Without it, Claude Code reads someone's login state on the machine, making account ownership and usage billing unclear. |
| GitLab URL / token | For a self-hosted instance, enter its root URL. The token needs `write_repository`; `read_repository` is sufficient for read-only tasks. |
| Workspace directory | Where repositories and worktrees are stored. Leave empty to use the system temporary directory. |
| Outbound proxy | The proxy Claude Code uses for the Anthropic API. **Only Claude Code uses it; git always connects directly.** |
| Proxy bypass domains | Enter internal GitLab domains here. |

## Usage

A typical workflow has two nodes: **Run task** → **Create MR** (GitLab plugin). Claude Code edits code and pushes a branch;
the GitLab plugin opens the MR. Each handles its own stage, making failures easier to locate.

Make the task as specific as possible and **include acceptance criteria**. “Fix X and make `go test ./internal/api` pass”
is much more useful than “Fix X”: it prompts Claude Code to verify the change with tests.

To follow up after a run, such as “The tests failed; try again,” use **Continue task**. Pass the output `session_id` and
`branch`; Claude Code remembers the previous round and continues in the same worktree.

## Common pitfalls

- **Code is not pushed by default**. The operation returns a diff and conclusion, leaving code in the worktree. Enable pushing explicitly;
  giving an agent push access by default can have consequences that become apparent only after something goes wrong.
- **Always set a cost limit**. `max_budget_usd` is the only brake if the agent runs out of control. Without it,
  the bill may be your first indication of the problem.
- **Permission mode** defaults to `acceptEdits` (file edits are allowed). `bypassPermissions` hands over the machine;
  use it only in an isolated container. If a task is unfinished, inspect `denied_tools` first: permissions are often too restrictive.
- **The same branch name reuses the same worktree**. A second run is faster, but it can see the previous run's changes.
  For a clean starting point, use a different branch name or run Clean worktrees first.
- **Tool filters use colon syntax**: `Bash(git:*)`, not `Bash(git *)`.
