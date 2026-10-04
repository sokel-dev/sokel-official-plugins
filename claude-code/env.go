package main

// Deployment config: environment variables for the plugin process.
//
// The split with credentials is a clean line: **credentials answer "whose quota to use", environment
// answers "what this machine is allowed to do".** How the repo gets cloned, where the workspace lives,
// where claude is, whether to go through a proxy, whether directories outside the workspace are allowed —
// all of that is the latter. These are set by whoever deploys this machine, not by whoever picks a
// credential on the canvas.
//
//	SOKEL_CC_GIT_BASE      GitLab root URL (e.g. https://git.example.com)
//	SOKEL_CC_GIT_TOKEN     clone/push token. **If unset, falls back to git's own auth on this machine**
//	                       (credential helper / .netrc / SSH) — the cleaner option
//	SOKEL_CC_WORKSPACE     where repos and worktrees live, default <tempdir>/sokel-claude-code
//	SOKEL_CC_CLAUDE_BIN    path to the claude executable, default looked up from PATH
//	SOKEL_CC_ALLOW_EXTERNAL_DIRS=1  allow "continue task" to target a directory outside the workspace
//	http_proxy/https_proxy CC's outbound proxy — **the plugin does not forward it, CC already reads
//	                       these standard variables itself**; the git side has it actively stripped by
//	                       gitEnv() (internal repos must not go through the proxy)

import (
	"os"
	"path/filepath"
	"strings"
)

type deployEnv struct {
	GitBase   string
	GitToken  string
	Workspace string
	ClaudeBin string
	AllowExt  bool
}

func loadEnv() deployEnv {
	e := deployEnv{
		GitBase:   strings.TrimRight(strings.TrimSpace(os.Getenv("SOKEL_CC_GIT_BASE")), "/"),
		GitToken:  strings.TrimSpace(os.Getenv("SOKEL_CC_GIT_TOKEN")),
		Workspace: strings.TrimSpace(os.Getenv("SOKEL_CC_WORKSPACE")),
		ClaudeBin: strings.TrimSpace(os.Getenv("SOKEL_CC_CLAUDE_BIN")),
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SOKEL_CC_ALLOW_EXTERNAL_DIRS"))) {
	case "1", "true", "yes", "on":
		e.AllowExt = true
	}
	if e.Workspace == "" {
		e.Workspace = filepath.Join(os.TempDir(), "sokel-claude-code")
	}
	if e.ClaudeBin == "" {
		e.ClaudeBin = "claude"
	}
	return e
}

// cfg is the process-wide read-only deployment config. Read once at startup: changing environment
// variables while running shouldn't make the same process's behavior drift.
var cfg = loadEnv()
