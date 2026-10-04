package main

// Workspace: a repo cache + a git worktree per branch.
//
//	<workspace>/repos/<slug>          the repo itself (cloned once, then only fetched)
//	<workspace>/trees/<slug>/<branch> one worktree per branch
//
// Why a worktree instead of re-cloning every time: cloning a repo takes tens of seconds to minutes, and
// the same project gets tasked repeatedly. Worktrees share the same object store, so opening one is a
// matter of seconds. Why one directory per branch: two tasks sharing a single worktree would inevitably
// step on each other — one editing while the other checks out from under it.
//
// **The proxy is given only to CC, git always connects directly**: this plugin's typical deployment is
// "internal GitLab + a proxy needed to reach Anthropic", and the two have opposite outbound paths.
// Handing http_proxy to git as well would make cloning an internal repo hang until it times out on the
// proxy — and the error looks exactly like network flakiness, which makes it hard to even suspect the
// proxy.

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// slug turns a project path into a directory name. Slashes become underscores: group/name has two
// levels, and using it directly as a path would dig an extra directory layer.
func slug(project string) string {
	s := strings.TrimSpace(project)
	s = strings.Trim(s, "/")
	repl := strings.NewReplacer("/", "_", ":", "_", " ", "_", "..", "_")
	return repl.Replace(s)
}

// repoURL builds the clone URL with credentials baked in. The token only ever appears as a **command-line
// argument**, never written into .git/config — a token that lands in the config file would sit on disk
// for as long as the worktree exists, and the worktree is something CC can read.
func repoURL(project string) (clean, authed string, err error) {
	base := cfg.GitBase
	if base == "" {
		base = "https://gitlab.com"
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return "", "", fmt.Errorf("环境变量 SOKEL_CC_GIT_BASE 里的地址 %q 解析不了", cfg.GitBase)
	}
	p := strings.Trim(strings.TrimSpace(project), "/")
	if p == "" {
		return "", "", fmt.Errorf("没给项目")
	}
	clean = fmt.Sprintf("%s://%s/%s.git", u.Scheme, u.Host, p)
	// No token given = fall back to git's own auth on this machine (credential helper / .netrc / SSH).
	// This is the cleaner option: the token never even enters the plugin process.
	tok := cfg.GitToken
	if tok == "" {
		return clean, clean, nil
	}
	authed = fmt.Sprintf("%s://oauth2:%s@%s/%s.git", u.Scheme, url.QueryEscape(tok), u.Host, p)
	return clean, authed, nil
}

func workspaceRoot() string { return cfg.Workspace }

// git runs a single git command. An empty dir means no working directory is set (e.g. for clone).
func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	// Connect directly: see the file's top comment. Also disable interactive credential prompts,
	// otherwise an auth failure would hang on a prompt until it times out.
	cmd.Env = append(gitEnv(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s 失败: %s", strings.Join(args, " "), strings.TrimSpace(clip(string(out), 400)))
	}
	return string(out), nil
}

// gitEnv is the environment with proxy variables stripped out.
func gitEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		k := strings.ToLower(kv[:strings.IndexByte(kv, '=')+1])
		if strings.HasPrefix(k, "http_proxy=") || strings.HasPrefix(k, "https_proxy=") || strings.HasPrefix(k, "all_proxy=") {
			continue
		}
		env = append(env, kv)
	}
	return env
}

// ensureWorktree makes sure a branch's worktree exists and returns its path.
//
// If it already exists, only fetch, never reset — the semantics of "reusing the same branch name" is
// to pick up where the last run left off, and a reset would silently wipe out CC's previous changes
// (the worst kind of bug: no error at all, the work just quietly vanishes).
func ensureWorktree(project, branch, base string) (string, error) {
	clean, authed, err := repoURL(project)
	if err != nil {
		return "", err
	}
	root := workspaceRoot()
	repo := filepath.Join(root, "repos", slug(project))
	tree := filepath.Join(root, "trees", slug(project), slug(branch))

	if _, err := os.Stat(filepath.Join(repo, ".git")); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(repo), 0o755); err != nil {
			return "", fmt.Errorf("建工作区目录失败: %w", err)
		}
		if _, err := git("", "clone", "--quiet", authed, repo); err != nil {
			return "", err
		}
		// Once cloned, switch the remote back to the token-free form (clone writes the token-bearing
		// URL into .git/config)
		if _, err := git(repo, "remote", "set-url", "origin", clean); err != nil {
			return "", err
		}
	}
	if _, err := git(repo, "fetch", "--quiet", "--prune", authed, "+refs/heads/*:refs/remotes/origin/*"); err != nil {
		return "", err
	}

	if _, err := os.Stat(tree); err == nil {
		return tree, nil // worktree already exists: reuse it
	}
	if err := os.MkdirAll(filepath.Dir(tree), 0o755); err != nil {
		return "", fmt.Errorf("建工作树目录失败: %w", err)
	}
	ref := "origin/" + strings.TrimSpace(base)
	if strings.TrimSpace(base) == "" {
		ref = defaultBranchRef(repo)
	}
	if _, err := git(repo, "worktree", "add", "-B", branch, tree, ref); err != nil {
		return "", err
	}
	return tree, nil
}

// defaultBranchRef returns the repo's default branch. Falls back to origin/main if it can't be
// determined — that's fine even if it's wrong, since the caller's worktree add will surface the real
// reason ("invalid reference") rather than this function guessing blindly.
func defaultBranchRef(repo string) string {
	out, err := git(repo, "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD")
	if err == nil {
		if r := strings.TrimSpace(out); r != "" {
			return strings.TrimPrefix(r, "refs/remotes/")
		}
	}
	return "origin/main"
}

// changedFiles lists files changed in the worktree (including untracked ones).
func changedFiles(tree string) []string {
	out, err := git(tree, "status", "--porcelain")
	if err != nil {
		return nil
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 4 {
			continue
		}
		// porcelain format: first two chars are status codes, third is a space; a rename looks like
		// "R  old -> new"
		p := strings.TrimSpace(line[2:])
		if i := strings.Index(p, " -> "); i >= 0 {
			p = p[i+4:]
		}
		if p != "" {
			files = append(files, p)
		}
	}
	return files
}

// diffText returns the worktree's diff. Includes untracked files (add -N first so they show up in the
// diff), truncated if too long.
func diffText(tree string, limit int) string {
	_, _ = git(tree, "add", "-A", "-N")
	out, err := git(tree, "diff")
	if err != nil {
		return ""
	}
	return clip(out, limit)
}

// commitAndPush commits and pushes the branch. Never makes an empty commit when there are no changes.
func commitAndPush(tree, project, branch, message string) (bool, error) {
	if len(changedFiles(tree)) == 0 {
		return false, nil
	}
	if _, err := git(tree, "add", "-A"); err != nil {
		return false, err
	}
	// Commit author identity: passed via command-line -c rather than editing the repo config, to
	// avoid polluting a reused worktree.
	if _, err := git(tree, "-c", "user.name=Sokel Claude Code", "-c", "user.email=claude-code@sokel.local",
		"commit", "--quiet", "-m", message); err != nil {
		return false, err
	}
	_, authed, err := repoURL(project)
	if err != nil {
		return false, err
	}
	if _, err := git(tree, "push", "--quiet", authed, "HEAD:refs/heads/"+branch); err != nil {
		return false, err
	}
	return true, nil
}

func clip(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n] + "\n…（已截断）"
}

func repoPath(project string) string {
	return filepath.Join(workspaceRoot(), "repos", slug(project))
}

func worktreePath(project, branch string) (string, error) {
	if strings.TrimSpace(project) == "" || strings.TrimSpace(branch) == "" {
		return "", fmt.Errorf("项目与分支都要给")
	}
	return filepath.Join(workspaceRoot(), "trees", slug(project), slug(branch)), nil
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// ensureWritable checks whether the workspace exists and is writable. A health check needs to answer
// "can it actually work right now" — just checking the directory exists isn't enough, since a missing
// write permission would only surface mid-run.
func ensureWritable(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".probe-*")
	if err != nil {
		return err
	}
	name := f.Name()
	_ = f.Close()
	return os.Remove(name)
}

// pingGitlab probes with ls-remote: it verifies both the address and the token, which is more useful
// than just pinging the host.
func pingGitlab() error {
	_, authed, err := repoURL("x/y")
	if err != nil {
		return err
	}
	base := authed[:strings.LastIndex(authed, "/x/y.git")]
	if _, err := git("", "ls-remote", "--exit-code", "-h", base+"/x/y.git"); err != nil {
		// The probe repo most likely doesn't exist — which actually proves "address reachable,
		// authenticated OK"; only an unreachable host or an auth failure counts as actually down.
		msg := err.Error()
		if strings.Contains(msg, "not found") || strings.Contains(msg, "404") ||
			strings.Contains(msg, "The project you were looking for") {
			return nil
		}
		return err
	}
	return nil
}

// externalTree validates the path for "take over an external directory".
//
// This is the only place in the plugin where a workflow input decides where CC operates, which expands
// the blast radius from the workspace to the whole machine — so it needs an explicit gate in the
// credential. The gate lives on the **credential** rather than the input: whoever configures the
// credential is ops, whoever orchestrates the workflow is the business side, and it should be the
// former who decides how far the latter can reach.
func externalTree(dir string) (string, error) {
	if !cfg.AllowExt {
		return "", fmt.Errorf("这台机器没开外部目录——接管插件工作区之外的目录需要部署时设 SOKEL_CC_ALLOW_EXTERNAL_DIRS=1")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("工作目录 %q 解析失败: %w", dir, err)
	}
	st, err := os.Stat(abs)
	if err != nil || !st.IsDir() {
		return "", fmt.Errorf("工作目录 %s 不存在或不是目录（路径是**插件那台机器**上的绝对路径）", abs)
	}
	return abs, nil
}
