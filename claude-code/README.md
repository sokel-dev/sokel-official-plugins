# claude-code — Claude Code plugin (first-party built-in directory)

Wires **the locally installed Claude Code** into a workflow: 4 operations (run task / resume task /
cleanup worktrees / health_check). The task operations declare `Stream: true`, so progress streams back
live. User docs: [docs/claude-code.md](docs/claude-code.md).

**Deployment constraint**: must run on a machine that has access to the target GitLab, has `claude`
installed, and has disk space.

## Why a CLI subprocess

The Claude Agent SDK (Claude Code's library form) only has Python / TypeScript bindings — **no Go one**.
For a Go host, the CLI is the official path. The CLI has no `--cwd`, but Go's `cmd.Dir` fills that gap
nicely.

## Gotchas (read before touching the code)

- **Flags must be checked against the real `--help`, never copied from docs**: `--bare` and
  `--max-turns` **don't exist** in 2.1.38 (even though the docs mention them); `--allowedTools` is
  camelCase; the tool-filter syntax uses a **colon**, `Bash(git:*)`, not a space. The cost cap flag is
  `--max-budget-usd`. Tests pin all of this down.
- **The result line is the only trustworthy final state**; the exit code only tells you "how the process
  ended": 0 normal, 2 hit the cost budget, 130/143 killed by a signal — hitting the budget still produced
  real output, and treating it as a failure throws away money that was already spent.
- **Use bufio.Reader, not Scanner**: the `init` line alone can be tens of KB (it carries the whole
  tools/skills/plugins inventory), and Scanner's default 64KB limit would kill the whole stream outright
  with "token too long".
- **Only three line kinds are parsed** (system/init, assistant, result), everything else is ignored. CC
  also emits hook/mcp noise lines, and hard-coding an exhaustive list of types would break on the next
  version's new types anyway; non-JSON garbage lines are simply dropped so one bad line doesn't break the
  whole stream.
- **The proxy goes only to CC, git always connects directly**: the typical deployment is "internal
  GitLab + a proxy needed to reach Anthropic", and the two have opposite outbound paths. Handing
  `http_proxy` to git as well would make cloning an internal repo hang until it times out on the proxy,
  and the error looks exactly like network flakiness. `gitEnv()` strips it clean — just be careful not to
  strip `GOPROXY` along with it.
- **The token only ever travels via the command line, never into .git/config**: right after cloning, a
  `remote set-url` switches back to the clean URL. The worktree is a directory CC can read, and a token
  sitting there is a token handed over. Tests pin this down.
- **Reusing a worktree for the same branch name never resets it**: "reuse" means picking up where the
  last run left off; a reset would silently wipe out the previous run's changes — no error at all, the
  work just quietly vanishes.
- **Cleanup uses `git worktree remove`, not rm -rf**: the latter leaves a dangling record in the repo,
  and the next `worktree add` for the same branch name fails outright with "already registered".
- **Session takeover is bounded by "same machine + same OS user"**: CC writes sessions to
  `~/.claude/projects/<escaped cwd>/<id>.jsonl` with 0600 permissions. Whichever account the plugin runs
  under is the only one that can `--resume`. That's why the outputs must include the worktree path —
  otherwise knowing the session id alone doesn't tell you where to cd.
- **The gate for taking over an external directory lives in the deployment environment**
  (`SOKEL_CC_ALLOW_EXTERNAL_DIRS`, off by default): this is the only place where a workflow input decides
  where CC operates, which expands the blast radius from the workspace to the whole machine. The default
  must be to refuse, and tests pin this down. External directories **never get pushed** — the remote and
  branch belong to a person, and it's too risky for the plugin to guess and push.
- **The API key is optional**, and an empty value **must never be injected into the environment**:
  a bare `ANTHROPIC_API_KEY=` would shadow CC's own login state on the machine, blocking the legitimate
  "use the subscription quota" path with a confusing error. Tests pin this down.
- **A boolean switch's default must hold true by its plain meaning**: cleanup uses `include_dirty`
  (delete dirty ones too) rather than `keep_dirty` (skip dirty ones) — a boolean left unset is false, so
  with the former "not set = protected" holds naturally, while the latter would require guessing whether
  the author ever set it. The third time hitting this same zero-value/default pitfall.
- Sessions are CC's own to manage (`~/.claude/projects/`); cleaning up worktrees **never touches them** —
  otherwise it would accidentally wipe out manual sessions on the same machine.
- **The credential holds only the API key, everything else lives in process environment variables**
  (env.go). The GitLab address/token, workspace, claude path, and proxy used to all live in the
  credential — that stuffed deployment config into the credential: the same token would need configuring
  twice, once in the gitlab plugin and once here (rotation would inevitably miss one), deploying to a
  different machine would mean editing the credential, and the credential is picked by the **workflow
  author** while which directories a machine can touch should be decided by **ops**. The split is a clean
  line: credential = whose quota to use, environment = what this machine is allowed to do.

## Development

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```

The test fixtures are **actual stream-json output from a real 2.1.38 run** — synthetic fixtures would be
misleading here (you can't reliably guess field names, nesting depth, or which line actually carries
session_id).
