package main

// Operation implementations.
//
// One tradeoff runs through all of them: **don't touch the remote by default**. CC leaves its code
// changes in the worktree, and the outputs carry the diff and the conclusion; pushing has to be turned
// on explicitly. Giving an agent push access by default is exactly the kind of default that only gets
// noticed after something has already gone wrong.

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

// diffLimit caps the diff output: beyond this it's no use to downstream nodes anyway, and the full
// change still lives in the worktree/branch.
const diffLimit = 60000

func credOf(ctx plugin.Ctx) Cred {
	var c Cred
	sokel.BindCredential(ctx, &c)
	return c
}

// streamSink turns CC's progress into two output streams:
//
//	Text frames  — the raw stream the debug console shows frame by frame
//	JSON frames  — the canvas node's partial output. **Same shape as the final output** (both are a
//	               log field), so the frontend needs only one rendering path, no separate "in progress"
//	               view.
//
// Throttled to 300ms: a single CC task can emit hundreds of text chunks, and emitting the full
// accumulated log on every chunk would flood the progress channel (every frame crosses the NATS + SSE
// bridge).
type streamSink struct {
	mu       sync.Mutex
	log      strings.Builder
	lastPush time.Time
	text     func(string)
	json     func(any)
}

func (s *streamSink) onText(chunk string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log.WriteString(chunk)
	if !strings.HasSuffix(chunk, "\n") {
		s.log.WriteString("\n")
	}
	s.text(chunk)
	if time.Since(s.lastPush) < 300*time.Millisecond {
		return
	}
	s.lastPush = time.Now()
	s.json(map[string]any{"log": s.log.String()})
}

func (s *streamSink) onTool(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	line := "· 调用工具 " + name
	s.log.WriteString(line + "\n")
	s.text(line)
}

func (s *streamSink) text_() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.log.String()
}

// runOne is the shared backbone of both task operations: prepare worktree → run CC → collect changes →
// push if requested.
//
// A non-empty fixedTree means taking over a session outside the plugin: work directly in that directory
// and skip the git-preparation logic (that directory was most likely cloned by hand and isn't inside the
// plugin's workspace).
func runOne(ctx plugin.Ctx, project, branch, base, fixedTree string, push bool, o ccOptions,
	emitText func(string), emitJSON func(any)) (*RunTaskOut, error) {

	c := credOf(ctx)
	if strings.TrimSpace(o.Task) == "" {
		return nil, fmt.Errorf("没给任务——写清楚要它做什么，最好带上验收标准")
	}

	var tree string
	if t := strings.TrimSpace(fixedTree); t != "" {
		var err error
		if tree, err = externalTree(t); err != nil {
			return nil, err
		}
		emitText("· 接管外部目录 " + tree)
	} else {
		if strings.TrimSpace(project) == "" {
			return nil, fmt.Errorf("没给项目（也没给「直接指定工作目录」）")
		}
		if branch == "" {
			branch = "cc/" + time.Now().Format("20060102-150405")
		}
		emitText(fmt.Sprintf("· 准备工作树 %s（分支 %s）", project, branch))
		var err error
		if tree, err = ensureWorktree(project, branch, base); err != nil {
			return nil, err
		}
	}

	emitJSON(map[string]any{"worktree": tree}) // hand off the path early: even if it crashes, you know where to look
	sink := &streamSink{text: emitText, json: emitJSON}
	res, err := runClaude(ctx, c, tree, o, sink.onText, sink.onTool)
	out := &RunTaskOut{
		Branch: branch, Worktree: tree, Log: sink.text_(), SessionID: res.SessionID,
		CostUsd: res.CostUSD, Turns: res.Turns, DeniedTools: res.DeniedTools,
		Conclusion: res.Conclusion, OK: res.OK,
	}
	if err != nil {
		// Even on failure, hand off what already happened: the log, the money spent, the tools that
		// got blocked — "it failed but you can't see what it did" means you can't debug it.
		out.ChangedFiles = changedFiles(tree)
		out.Diff = diffText(tree, diffLimit)
		return out, err
	}
	out.ChangedFiles = changedFiles(tree)
	out.Diff = diffText(tree, diffLimit)

	if res.SessionID != "" {
		emitText(fmt.Sprintf("· 接管这次会话：cd %s && claude --resume %s", tree, res.SessionID))
	}
	if push {
		if strings.TrimSpace(fixedTree) != "" {
			// For an external directory, the person set up the remote and branch themselves — it's
			// too risky for the plugin to guess and push; let them push it themselves.
			return out, fmt.Errorf("接管外部目录时不支持推送：那个仓库的远端和分支归你管，请自行 git push")
		}
		if len(out.ChangedFiles) == 0 {
			emitText("· 没有改动，跳过推送")
		} else {
			emitText(fmt.Sprintf("· 推送 %d 个改动到 %s", len(out.ChangedFiles), branch))
			msg := firstLine(o.Task)
			pushed, perr := commitAndPush(tree, project, branch, "claude-code: "+msg)
			if perr != nil {
				// A push failure shouldn't erase the task's results: outputs are still returned as
				// usual, the error is reported separately.
				out.OK = false
				return out, fmt.Errorf("代码已改好但推送失败: %w", perr)
			}
			out.Pushed = pushed
		}
	}
	return out, nil
}

func opRunTask(ctx plugin.Ctx, in *RunTaskIn, out *RunTaskEmitter) error {
	r, err := runOne(ctx, in.Project, in.Branch, in.BaseBranch, "", in.Push, ccOptions{
		Task: in.Task, Model: in.Model, Effort: in.Effort, SystemPrompt: in.SystemPrompt,
		PermissionMode: in.PermissionMode, AllowedTools: in.AllowedTools,
		DisallowedTools: in.DisallowedTools, MaxBudgetUSD: in.MaxBudgetUsd,
	}, out.Text, out.JSON)
	if r != nil {
		out.Vars(r)
	}
	return err
}

func opResumeTask(ctx plugin.Ctx, in *ResumeTaskIn, out *ResumeTaskEmitter) error {
	if strings.TrimSpace(in.SessionID) == "" {
		return fmt.Errorf("没给会话 ID——它在上一次「执行任务」的出参里")
	}
	r, err := runOne(ctx, in.Project, in.Branch, "", in.Worktree, in.Push, ccOptions{
		Task: in.Task, MaxBudgetUSD: in.MaxBudgetUsd, ResumeSession: in.SessionID,
	}, out.Text, out.JSON)
	if r != nil {
		out.Vars((*ResumeTaskOut)(r))
	}
	return err
}

func opListWorktrees(_ plugin.Ctx, in *ListWorktreesIn) (*ListWorktreesOut, error) {
	items := scanWorktrees(in.Project)
	out := &ListWorktreesOut{Items: items, Count: len(items), Workspace: workspaceRoot()}
	for _, it := range items {
		out.TotalMb += it.SizeMB
	}
	return out, nil
}

func opCleanup(_ plugin.Ctx, in *CleanupIn) (*CleanupOut, error) {
	branch := strings.TrimSpace(in.Branch)
	// Neither a named branch nor an idle-days threshold means "delete every worktree". That intent
	// must be stated explicitly, not reached silently via an empty form — a worktree may hold
	// uncommitted changes.
	if branch == "" && in.IdleDays <= 0 {
		return nil, fmt.Errorf("要么点名一个分支，要么给「只删闲置超过几天的」——" +
			"两个都不给等于删光所有工作树，这个插件不替你做这个决定")
	}

	out := &CleanupOut{}
	for _, it := range scanWorktrees(in.Project) {
		if branch != "" && it.Branch != branch && slug(it.Branch) != slug(branch) {
			continue
		}
		if in.IdleDays > 0 && it.IdleDays < in.IdleDays {
			continue
		}
		if it.Dirty && !in.IncludeDirty {
			out.SkippedDirty++
			continue
		}
		out.Items = append(out.Items, it)
		out.FreedMb += it.SizeMB
		if in.DryRun {
			continue
		}
		// Use git worktree remove rather than rm -rf: the latter leaves a dangling record in the
		// repo, and the next worktree add for the same branch name fails outright with "already
		// registered".
		if _, err := git(repoPath(it.Project), "worktree", "remove", "--force", it.Path); err != nil {
			return out, err
		}
		out.Removed++
	}
	return out, nil
}

func opHealthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	c := credOf(ctx)
	out := &HealthCheckOut{WorkspaceDir: workspaceRoot()}

	ver, err := claudeVersion(ctx, c)
	if err != nil {
		out.Message = fmt.Sprintf("Claude Code 不可用：%v（这台机器装了 claude 吗？凭证里也可以指定可执行路径）", err)
		return out, nil
	}
	out.ClaudeVersion = ver

	if err := ensureWritable(out.WorkspaceDir); err != nil {
		out.Message = fmt.Sprintf("工作区不可写：%v", err)
		return out, nil
	}
	if err := pingGitlab(); err != nil {
		out.Message = fmt.Sprintf("Claude Code %s 就绪，但 GitLab 不通：%v", ver, err)
		return out, nil
	}
	out.GitlabOK = true
	out.OK = true
	auth := "机器登录态"
	if strings.TrimSpace(c.APIKey) != "" {
		auth = "凭证里的 API Key"
	}
	out.Message = fmt.Sprintf("就绪（Claude Code %s，额度走%s，工作区 %s）", ver, auth, out.WorkspaceDir)
	return out, nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return clip(strings.TrimSpace(s), 72)
}
