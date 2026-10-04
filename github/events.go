package main

// 轮询事件源（不用配 webhook 的那条路；与 webhook.go 二选一）。
//
// 走 /repos/{o}/{r}/events：GitHub 把仓库的活动流放在这里，一次调用就能拿到
// 推送/Issue/PR/评论/发布五类，比逐个接口轮询省得多（速率配额每小时 5000 次，
// 一个仓库一分钟一次是 60 次/小时，盯十来个仓库很宽裕）。
//
// 两个必须处理的细节：
//
//   - **首轮不触发**。第一次拉到的是最近 30 条历史活动，照发的话插件一启动就会
//     把三十条陈年事件全推进工作流。所以首轮只记游标（primed），从第二轮起才推。
//   - **活动流有缓存**。GitHub 明说这个接口有约 60 秒缓存，所以轮询间隔小于 60 秒
//     没有意义，只是白烧配额。
//
// 工作流失败不在活动流里（GitHub 不把它算作仓库活动），单独查一次 Actions 运行记录。

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

const pollInterval = 60 * time.Second

func runEvents(ctx plugin.SourceCtx) error {
	cred := credOf(ctx)
	repos := splitRepos(cred.WatchRepos)
	if len(repos) == 0 {
		// 没配就报「等配置」而不是静静退出——凭证列表上那一行会亮起来，
		// 否则用户只会看到一个什么都不做的源，无从判断是坏了还是没配。
		ctx.ReportStatus("idle", "凭证里没填「事件盯哪些仓库」，轮询源不启动")
		return nil
	}
	if strings.TrimSpace(cred.Token) == "" {
		ctx.ReportStatus("auth_required", "凭证里没有访问令牌")
		return nil
	}
	log.Printf("[github] 轮询源启动，盯 %d 个仓库：%s", len(repos), strings.Join(repos, ", "))

	seen := map[string]string{}  // repo → 上次见过的最新事件 id
	primed := map[string]bool{}  // repo → 首轮是否已过
	runSeen := map[string]bool{} // 工作流失败去重（run_id+attempt）

	for {
		for _, repo := range repos {
			if ctx.Err() != nil {
				return nil
			}
			if err := pollRepo(ctx, repo, seen, primed); err != nil {
				log.Printf("[github] 轮询 %s 失败：%v", repo, err)
			}
			if err := pollFailedRuns(ctx, repo, runSeen, primed[repo]); err != nil {
				log.Printf("[github] 查 %s 的失败工作流出错：%v", repo, err)
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(pollInterval):
		}
	}
}

func splitRepos(s string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '\n' || r == ' ' || r == '\t'
	}) {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// pollRepo 拉一个仓库的活动流并按类型分发。
func pollRepo(ctx plugin.SourceCtx, repo string, seen map[string]string, primed map[string]bool) error {
	rp, err := repoPath(repo)
	if err != nil {
		return err
	}
	raw, _, err := ghCall(ctx, http.MethodGet, rp+"/events", map[string]any{"per_page": 30})
	if err != nil {
		return err
	}
	list := digList(raw)
	if len(list) == 0 {
		return nil
	}
	last := seen[repo]
	newest := str(list[0], "id")

	if !primed[repo] {
		// 首轮：只记游标，不推。
		seen[repo], primed[repo] = newest, true
		log.Printf("[github] %s 首轮记录游标 %s（历史事件不补发）", repo, newest)
		return nil
	}
	// 活动流是新→旧，倒着遍历才能按时间顺序推。
	for i := len(list) - 1; i >= 0; i-- {
		e := list[i]
		id := str(e, "id")
		if id == "" || id == last || !newerThan(id, last) {
			continue
		}
		dispatchActivity(ctx, repo, id, e)
	}
	seen[repo] = newest
	return nil
}

// newerThan 活动流的 id 是**递增的十进制数字串**（但会超过 int64 的舒适区，按长度+字典序比）。
func newerThan(id, last string) bool {
	if last == "" {
		return true
	}
	if len(id) != len(last) {
		return len(id) > len(last)
	}
	return id > last
}

// dispatchActivity 把一条活动流事件翻译成插件契约里的事件。
//
// 活动流的 payload 与 webhook 的 payload **形状相近但不相同**（比如没有 repository 顶层对象），
// 所以不能直接复用 webhook 那套解析——这是两条路各写一份的原因。
func dispatchActivity(ctx plugin.SourceCtx, repo, id string, e map[string]any) {
	pl := obj(e, "payload")
	actor := nested(e, "actor", "login")
	eid := "poll:" + id

	switch str(e, "type") {
	case "PushEvent":
		commits := arr(pl, "commits")
		msg := ""
		if n := len(commits); n > 0 {
			if m, ok := commits[n-1].(map[string]any); ok {
				msg = clip(str(m, "message"), 2000)
			}
		}
		_ = TriggerCommitPushed(ctx, eid, &CommitPushedEvent{
			Repo: repo, Ref: strings.TrimPrefix(str(pl, "ref"), "refs/heads/"),
			CommitSHA: str(pl, "head"), CommitMessage: msg, CommitCount: len(commits),
			Author: actor, Source: "poll",
		})
	case "IssuesEvent":
		iss := obj(pl, "issue")
		switch str(pl, "action") {
		case "opened", "reopened":
			_ = TriggerIssueOpened(ctx, eid, &IssueOpenedEvent{
				Repo: repo, Number: num(iss, "number"), Title: str(iss, "title"),
				Body: clip(str(iss, "body"), 4000), Author: nested(iss, "user", "login"),
				Labels: namesOf(iss["labels"], "name"), URL: str(iss, "html_url"),
				Source: "poll",
			})
		case "labeled":
			_ = TriggerIssueLabeled(ctx, eid, &IssueLabeledEvent{
				Repo: repo, Number: num(iss, "number"), Title: str(iss, "title"),
				Body: clip(str(iss, "body"), 4000), AddedLabel: nested(pl, "label", "name"),
				Labels: namesOf(iss["labels"], "name"), IsPr: isPullRequest(iss),
				Actor: actor, URL: str(iss, "html_url"), Source: "poll",
			})
		}
	case "PullRequestEvent":
		pr := obj(pl, "pull_request")
		switch str(pl, "action") {
		case "opened", "reopened":
			_ = TriggerPrOpened(ctx, eid, &PrOpenedEvent{
				Repo: repo, Number: num(pr, "number"), Title: str(pr, "title"),
				Body: clip(str(pr, "body"), 4000), Author: nested(pr, "user", "login"),
				Base: nested(pr, "base", "ref"), Head: nested(pr, "head", "ref"),
				HeadSHA: nested(pr, "head", "sha"), Draft: boolean(pr, "draft"),
				Labels: namesOf(pr["labels"], "name"), URL: str(pr, "html_url"),
				Source: "poll",
			})
		case "closed":
			if boolean(pr, "merged") {
				_ = TriggerPrMerged(ctx, eid, &PrMergedEvent{
					Repo: repo, Number: num(pr, "number"), Title: str(pr, "title"),
					Author: nested(pr, "user", "login"), MergedBy: nested(pr, "merged_by", "login"),
					Base: nested(pr, "base", "ref"), MergeSHA: str(pr, "merge_commit_sha"),
					Labels: namesOf(pr["labels"], "name"), URL: str(pr, "html_url"),
					Source: "poll",
				})
			}
		}
	case "PullRequestReviewEvent":
		pr := obj(pl, "pull_request")
		rv := obj(pl, "review")
		_ = TriggerPrReviewSubmitted(ctx, eid, &PrReviewSubmittedEvent{
			Repo: repo, Number: num(pr, "number"), Title: str(pr, "title"),
			Reviewer: nested(rv, "user", "login"), State: strings.ToLower(str(rv, "state")),
			Body: clip(str(rv, "body"), 4000), HeadSHA: nested(pr, "head", "sha"),
			URL: str(rv, "html_url"), Source: "poll",
		})
	case "IssueCommentEvent":
		if str(pl, "action") != "created" {
			return
		}
		iss := obj(pl, "issue")
		cm := obj(pl, "comment")
		ev := IssueCommentedEvent{
			Repo: repo, Number: num(iss, "number"), Title: str(iss, "title"),
			Comment: clip(str(cm, "body"), 8000), CommentID: num(cm, "id"),
			Author: nested(cm, "user", "login"), AuthorAssociation: str(cm, "author_association"),
			IsBot: isBot(obj(cm, "user")), URL: str(cm, "html_url"), Source: "poll",
		}
		if isPullRequest(iss) {
			_ = TriggerPrCommented(ctx, eid, &PrCommentedEvent{
				Repo: ev.Repo, Number: ev.Number, Title: ev.Title, Comment: ev.Comment,
				CommentID: ev.CommentID, Author: ev.Author,
				AuthorAssociation: ev.AuthorAssociation, IsBot: ev.IsBot,
				URL: ev.URL, Source: "poll",
			})
			return
		}
		_ = TriggerIssueCommented(ctx, eid, &ev)
	case "ReleaseEvent":
		if str(pl, "action") != "published" {
			return
		}
		rel := obj(pl, "release")
		_ = TriggerReleasePublished(ctx, eid, &ReleasePublishedEvent{
			Repo: repo, Tag: str(rel, "tag_name"), Name: str(rel, "name"),
			Body: clip(str(rel, "body"), 8000), Prerelease: boolean(rel, "prerelease"),
			Author: nested(rel, "author", "login"), URL: str(rel, "html_url"),
			Source: "poll",
		})
	}
}

// pollFailedRuns 查最近失败的工作流。活动流里没有它，只能单独问一次。
func pollFailedRuns(ctx plugin.SourceCtx, repo string, seen map[string]bool, primed bool) error {
	rp, err := repoPath(repo)
	if err != nil {
		return err
	}
	raw, _, err := ghCall(ctx, http.MethodGet, rp+"/actions/runs", map[string]any{
		"status": "failure", "per_page": 10,
	})
	if err != nil {
		// Actions 没开的仓库这里会 404——不该让整个轮询循环报错。
		if strings.Contains(err.Error(), "404") {
			return nil
		}
		return err
	}
	for _, it := range arr(digObj(raw), "workflow_runs") {
		r, _ := it.(map[string]any)
		if r == nil {
			continue
		}
		// run_id + attempt：重试失败是一次新的真事件，只按 run_id 去重会把它吃掉。
		key := repo + ":" + str(r, "id") + ":" + str(r, "run_attempt")
		if seen[key] {
			continue
		}
		seen[key] = true
		if !primed {
			continue // 首轮只记不推，与活动流同一条规矩
		}
		_ = TriggerWorkflowFailed(ctx, "poll:run:"+key, &WorkflowFailedEvent{
			Repo: repo, Workflow: str(r, "name"), RunID: num(r, "id"),
			Branch: str(r, "head_branch"), CommitSHA: str(r, "head_sha"),
			Event: str(r, "event"), Actor: nested(r, "actor", "login"),
			Attempt: num(r, "run_attempt"), URL: str(r, "html_url"), Source: "poll",
		})
	}
	return nil
}

var _ = context.Background
