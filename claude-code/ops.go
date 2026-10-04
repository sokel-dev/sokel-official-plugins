package main

// 操作实现。
//
// 一条贯穿的取舍：**默认不动远端**。CC 改完代码留在工作树里，出参给 diff 与结论；
// 要 push 得显式打开。让一个 agent 默认握着 push 权限，是那种出事之后才被发现的默认值。

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

const diffLimit = 60000 // diff 出参上限：再大对下游节点也没用，完整改动在工作树/分支里

func credOf(ctx plugin.Ctx) Cred {
	var c Cred
	sokel.BindCredential(ctx, &c)
	return c
}

// streamSink 把 CC 的过程转成两路产出：
//
//	Text 帧 —— 调试台逐帧看的原始流
//	JSON 帧 —— 画布节点的部分产出。**形状与最终出参一致**（都是 log 字段），
//	           前端一套渲染就够，不必为「进行中」另写一份视图。
//
// 节流 300ms：CC 一次任务可能吐几百段文字，逐段全量 emit 会把进度通道打爆
// （每帧都要过 NATS + SSE 桥）。
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

// runOne 两个任务操作共用的主干：备工作树 → 跑 CC → 收改动 → 按需推送。
//
// fixedTree 非空 = 接管插件之外的会话：直接在那个目录干活，不碰 git 准备逻辑
// （那个目录多半是人自己 clone 的，不在插件工作区里）。
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

	emitJSON(map[string]any{"worktree": tree}) // 路径先交出去：跑挂了也知道去哪看现场
	sink := &streamSink{text: emitText, json: emitJSON}
	res, err := runClaude(ctx, c, tree, o, sink.onText, sink.onTool)
	out := &RunTaskOut{
		Branch: branch, Worktree: tree, Log: sink.text_(), SessionID: res.SessionID,
		CostUsd: res.CostUSD, Turns: res.Turns, DeniedTools: res.DeniedTools,
		Conclusion: res.Conclusion, OK: res.OK,
	}
	if err != nil {
		// 跑挂了也要把已经发生的事交出去：日志、花掉的钱、被拦下的工具——
		// 「失败了但看不到它做过什么」等于没法查。
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
			// 外部目录的远端与分支是人自己配的，插件猜一个推上去太危险——让人自己 push。
			return out, fmt.Errorf("接管外部目录时不支持推送：那个仓库的远端和分支归你管，请自行 git push")
		}
		if len(out.ChangedFiles) == 0 {
			emitText("· 没有改动，跳过推送")
		} else {
			emitText(fmt.Sprintf("· 推送 %d 个改动到 %s", len(out.ChangedFiles), branch))
			msg := firstLine(o.Task)
			pushed, perr := commitAndPush(tree, project, branch, "claude-code: "+msg)
			if perr != nil {
				// 推送失败不抹掉这次任务的成果：出参照常给，错误单独报。
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
	// 既没点名分支、又没给闲置天数 = 「删光所有工作树」。这种要求必须说出口，
	// 不能由一个空表单默默达成——工作树里可能有还没提交的改动。
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
		// 走 git worktree remove 而不是 rm -rf：后者在仓库里留悬空记录，
		// 下次同名分支 worktree add 直接报 already registered。
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
