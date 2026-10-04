package main

// 工作区：仓库缓存 + 按分支的 git worktree。
//
//	<workspace>/repos/<slug>          仓库本体（首次 clone，之后只 fetch）
//	<workspace>/trees/<slug>/<branch> 每个分支一份工作树
//
// 为什么是 worktree 而不是每次重 clone：一个仓库 clone 一次几十秒到几分钟，
// 而同一个项目会被反复派任务。worktree 共享同一份对象库，开一个是秒级。
// 为什么按分支分目录：两个任务共用一个工作树必然互相踩——一个在改，另一个 checkout 走了。
//
// **代理只给 CC，git 一律直连**：这个插件的典型部署是「内网 GitLab + 需要代理才能连
// Anthropic」，两者的出站路径正好相反。把 http_proxy 也塞给 git，clone 内网仓库就会
// 卡在代理上超时——而且报错长得像网络抖动，很难往代理上想。

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// slug 项目路径 → 目录名。斜杠换下划线：group/name 是两级，直接当路径会多挖一层目录。
func slug(project string) string {
	s := strings.TrimSpace(project)
	s = strings.Trim(s, "/")
	repl := strings.NewReplacer("/", "_", ":", "_", " ", "_", "..", "_")
	return repl.Replace(s)
}

// repoURL 拼带凭证的 clone 地址。token 只出现在**命令行参数**里，不写进 .git/config——
// 落进配置文件的令牌会跟着工作树一直躺在磁盘上，而工作树是 CC 能读的。
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
	// 没给令牌 = 用这台机器上 git 自己的认证（credential helper / .netrc / SSH）。
	// 这是更干净的做法：令牌根本不进插件进程。
	tok := cfg.GitToken
	if tok == "" {
		return clean, clean, nil
	}
	authed = fmt.Sprintf("%s://oauth2:%s@%s/%s.git", u.Scheme, url.QueryEscape(tok), u.Host, p)
	return clean, authed, nil
}

func workspaceRoot() string { return cfg.Workspace }

// git 跑一条 git 命令。dir 为空表示不指定工作目录（如 clone）。
func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	// 直连：见文件顶注。同时禁掉交互式取凭证，否则认证失败时会挂在提示符上等到超时。
	cmd.Env = append(gitEnv(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s 失败: %s", strings.Join(args, " "), strings.TrimSpace(clip(string(out), 400)))
	}
	return string(out), nil
}

// gitEnv 剥掉代理变量的环境。
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

// ensureWorktree 备好某分支的工作树，返回其路径。
//
// 已存在就只 fetch 不 reset——「同名分支复用」的语义是接着上次干，
// reset 会把上一轮 CC 的改动悄悄抹掉（最坏的一类 bug：没有任何报错，只是活白干了）。
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
		// 落地后把 remote 换回不带令牌的形态（clone 会把带令牌的 URL 写进 .git/config）
		if _, err := git(repo, "remote", "set-url", "origin", clean); err != nil {
			return "", err
		}
	}
	if _, err := git(repo, "fetch", "--quiet", "--prune", authed, "+refs/heads/*:refs/remotes/origin/*"); err != nil {
		return "", err
	}

	if _, err := os.Stat(tree); err == nil {
		return tree, nil // 已有工作树：接着用
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

// defaultBranchRef 仓库默认分支。取不到就退 origin/main——退不到也没关系，
// 上层的 worktree add 会报出真实原因（"invalid reference"），比在这里瞎猜好。
func defaultBranchRef(repo string) string {
	out, err := git(repo, "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD")
	if err == nil {
		if r := strings.TrimSpace(out); r != "" {
			return strings.TrimPrefix(r, "refs/remotes/")
		}
	}
	return "origin/main"
}

// changedFiles 工作树里改过的文件（含未跟踪）。
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
		// porcelain 格式前两位是状态位，第三位空格；重命名形如 "R  old -> new"
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

// diffText 工作树的改动。含未跟踪文件（先 add -N 让它们进 diff），超长截断。
func diffText(tree string, limit int) string {
	_, _ = git(tree, "add", "-A", "-N")
	out, err := git(tree, "diff")
	if err != nil {
		return ""
	}
	return clip(out, limit)
}

// commitAndPush 提交并推分支。没有改动时不造空提交。
func commitAndPush(tree, project, branch, message string) (bool, error) {
	if len(changedFiles(tree)) == 0 {
		return false, nil
	}
	if _, err := git(tree, "add", "-A"); err != nil {
		return false, err
	}
	// 提交人身份：走命令行 -c 而不是改仓库配置，避免污染复用的工作树。
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

// ensureWritable 工作区在不在、写不写得动。体检要答的是「现在能不能干活」，
// 只判目录存在不够——跑起来才发现没权限写，报错会出现在半路上。
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

// pingGitlab 用 ls-remote 探一下：既验地址也验令牌，比只 ping 主机有用。
func pingGitlab() error {
	_, authed, err := repoURL("x/y")
	if err != nil {
		return err
	}
	base := authed[:strings.LastIndex(authed, "/x/y.git")]
	if _, err := git("", "ls-remote", "--exit-code", "-h", base+"/x/y.git"); err != nil {
		// 探测仓库多半不存在——那也说明「地址通、认证过」，只有连不上/认证失败才算不通。
		msg := err.Error()
		if strings.Contains(msg, "not found") || strings.Contains(msg, "404") ||
			strings.Contains(msg, "The project you were looking for") {
			return nil
		}
		return err
	}
	return nil
}

// externalTree 校验「接管外部目录」的路径。
//
// 这是插件里唯一一处让工作流入参决定 CC 在哪干活的地方，等于把爆炸半径从工作区扩到整机——
// 所以要凭证里显式开闸。闸门放在**凭证**而不是入参上：配凭证的是运维，编排工作流的是业务，
// 该由前者决定后者能指到哪儿。
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
