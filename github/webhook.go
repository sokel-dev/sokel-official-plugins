package main

// 平台代收 webhook（零延迟版事件；与 events.go 的轮询源二选一）：
// GitHub 仓库 Settings→Webhooks 指向平台的 /hooks/{token}，Content type 选 application/json。
//
// 验签：GitHub 用 **HMAC-SHA256** 签整个请求体，放在 X-Hub-Signature-256 头里，
// 形如 sha256=<hex>。这与 GitLab 的「明文 token 比对」不是一回事——照抄 GitLab 的写法
// 会永远验不过。比对必须用 hmac.Equal（常数时间），普通 == 会泄漏时序信息。
//
// event_id 直接用 **X-GitHub-Delivery**：GitHub 给每次投递一个 UUID，而**重投时沿用同一个**，
// 正好就是平台去重想要的语义——不用自己按对象 id + 时间戳拼一个脆弱的键。
//
// 与轮询源**故意不同源**（轮询用 poll: 前缀）：两条都开时同一个动作会各触发一次，
// 文档写明二选一，不在这里硬去重（两边的 id 不同域，硬拼会造出脆弱的映射）。

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

func handleWebhook(ctx sokel.WebhookCtx, req *sokel.WebhookRequest) sokel.WebhookResponse {
	if resp, ok := verifySignature(ctx, req); !ok {
		return resp
	}
	event := req.Header("X-GitHub-Event")
	delivery := req.Header("X-GitHub-Delivery")
	if delivery == "" {
		delivery = "nodelivery"
	}
	var body map[string]any
	if json.Unmarshal(req.Body, &body) != nil {
		return sokel.Text(400, "bad payload")
	}
	// GitHub 在你保存 webhook 配置时立刻发一条 ping——回 200 它才显示成绿色。
	if event == "ping" {
		return sokel.Text(200, "pong")
	}
	repo := nestedStr(body, "repository", "full_name")
	action := str(body, "action")

	switch event {
	case "push":
		emitPush(ctx, delivery, repo, body)
	case "pull_request":
		emitPullRequest(ctx, delivery, repo, action, body)
	case "pull_request_review":
		if action == "submitted" {
			pr := obj(body, "pull_request")
			rv := obj(body, "review")
			_ = TriggerPrReviewSubmitted(ctx, "wh:"+delivery, &PrReviewSubmittedEvent{
				Repo: repo, Number: num(pr, "number"), Title: str(pr, "title"),
				Reviewer: nested(rv, "user", "login"),
				// GitHub 这里给的是大写 APPROVED/CHANGES_REQUESTED，契约里说的是小写——统一掉
				State: strings.ToLower(str(rv, "state")),
				Body:  clip(str(rv, "body"), 4000), HeadSHA: nested(pr, "head", "sha"),
				URL: str(rv, "html_url"), Source: "webhook",
			})
		}
	case "issues":
		emitIssues(ctx, delivery, repo, action, body)
	case "issue_comment":
		emitComment(ctx, delivery, repo, action, body)
	case "release":
		if action == "published" {
			rel := obj(body, "release")
			_ = TriggerReleasePublished(ctx, "wh:"+delivery, &ReleasePublishedEvent{
				Repo: repo, Tag: str(rel, "tag_name"), Name: str(rel, "name"),
				Body: clip(str(rel, "body"), 8000), Prerelease: boolean(rel, "prerelease"),
				Author: nested(rel, "author", "login"), URL: str(rel, "html_url"),
				Source: "webhook",
			})
		}
	case "workflow_run":
		run := obj(body, "workflow_run")
		if action == "completed" && str(run, "conclusion") == "failure" {
			_ = TriggerWorkflowFailed(ctx, "wh:"+delivery, &WorkflowFailedEvent{
				Repo: repo, Workflow: str(run, "name"), RunID: num(run, "id"),
				Branch: str(run, "head_branch"), CommitSHA: str(run, "head_sha"),
				Event: str(run, "event"), Actor: nested(run, "actor", "login"),
				Attempt: num(run, "run_attempt"), URL: str(run, "html_url"),
				Source: "webhook",
			})
		}
	}
	// 认不出的事件类型也回 200：GitHub 按仓库配置会发一堆类型，非 2xx 会让它反复重试，
	// 而且连发几次失败之后 GitHub 会把这个 webhook 整个停掉。
	return sokel.OK()
}

// verifySignature 验 HMAC-SHA256 签名。
//
// 凭证没配 secret 则跳过校验（内网/私有仓库可接受，文档写明了风险）。
// 配了 secret 但请求没带签名 → 拒：那说明 GitHub 那侧没填 secret，两边不一致，
// 这时放行等于假装验过了。
func verifySignature(ctx sokel.WebhookCtx, req *sokel.WebhookRequest) (sokel.WebhookResponse, bool) {
	secret := strings.TrimSpace(ctx.Credential()["webhook_secret"])
	if secret == "" {
		return sokel.OK(), true
	}
	got := strings.TrimSpace(req.Header("X-Hub-Signature-256"))
	if got == "" {
		return sokel.Text(401, "missing signature (webhook secret configured on this credential)"), false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(req.Body) // 必须是**原始字节**——重新编码过的 JSON 算出来的签名对不上
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(got), []byte(want)) {
		return sokel.Text(401, "bad signature"), false
	}
	return sokel.OK(), true
}

func emitPush(ctx sokel.WebhookCtx, delivery, repo string, body map[string]any) {
	after := str(body, "after")
	// 删分支也是 push 事件，且 after 全是 0——不判的话下游会去处理一个不存在的提交。
	deleted := boolean(body, "deleted")
	commits := arr(body, "commits")
	msg := ""
	if n := len(commits); n > 0 {
		if m, ok := commits[n-1].(map[string]any); ok {
			msg = clip(str(m, "message"), 2000)
		}
	}
	_ = TriggerCommitPushed(ctx, "wh:"+delivery, &CommitPushedEvent{
		Repo: repo, Ref: strings.TrimPrefix(str(body, "ref"), "refs/heads/"),
		CommitSHA: after, CommitMessage: msg, CommitCount: len(commits),
		Author:  firstNonEmpty(nestedStr(body, "pusher", "name"), nestedStr(body, "sender", "login")),
		Forced:  boolean(body, "forced"),
		Created: boolean(body, "created"), Deleted: deleted,
		Source: "webhook",
	})
}

func emitPullRequest(ctx sokel.WebhookCtx, delivery, repo, action string, body map[string]any) {
	pr := obj(body, "pull_request")
	switch action {
	case "opened", "reopened", "ready_for_review":
		_ = TriggerPrOpened(ctx, "wh:"+delivery, &PrOpenedEvent{
			Repo: repo, Number: num(pr, "number"), Title: str(pr, "title"),
			Body: clip(str(pr, "body"), 4000), Author: nested(pr, "user", "login"),
			Base: nested(pr, "base", "ref"), Head: nested(pr, "head", "ref"),
			HeadSHA: nested(pr, "head", "sha"), Draft: boolean(pr, "draft"),
			Labels: namesOf(pr["labels"], "name"), URL: str(pr, "html_url"),
			Source: "webhook",
		})
	case "closed":
		// **closed 不等于 merged**：关掉不合也是 closed。判据是 merged 字段。
		if boolean(pr, "merged") {
			_ = TriggerPrMerged(ctx, "wh:"+delivery, &PrMergedEvent{
				Repo: repo, Number: num(pr, "number"), Title: str(pr, "title"),
				Author: nested(pr, "user", "login"), MergedBy: nested(pr, "merged_by", "login"),
				Base: nested(pr, "base", "ref"), MergeSHA: str(pr, "merge_commit_sha"),
				Labels: namesOf(pr["labels"], "name"), URL: str(pr, "html_url"),
				Source: "webhook",
			})
		}
	case "labeled":
		// PR 被打标签走的是 pull_request 事件，不是 issues——两处都要发，否则
		// 「给 PR 打标签触发流程」这个很常见的用法会静默失效。
		_ = TriggerIssueLabeled(ctx, "wh:"+delivery, &IssueLabeledEvent{
			Repo: repo, Number: num(pr, "number"), Title: str(pr, "title"),
			Body: clip(str(pr, "body"), 4000), AddedLabel: nestedStr(body, "label", "name"),
			Labels: namesOf(pr["labels"], "name"), IsPr: true,
			Actor: nestedStr(body, "sender", "login"), URL: str(pr, "html_url"),
			Source: "webhook",
		})
	}
}

func emitIssues(ctx sokel.WebhookCtx, delivery, repo, action string, body map[string]any) {
	iss := obj(body, "issue")
	switch action {
	case "opened", "reopened":
		_ = TriggerIssueOpened(ctx, "wh:"+delivery, &IssueOpenedEvent{
			Repo: repo, Number: num(iss, "number"), Title: str(iss, "title"),
			Body: clip(str(iss, "body"), 4000), Author: nested(iss, "user", "login"),
			Labels: namesOf(iss["labels"], "name"), URL: str(iss, "html_url"),
			Source: "webhook",
		})
	case "labeled":
		// GitHub 每加一个标签发一次事件，且直接给出这次加的是哪个——
		// 比 GitLab 那种「自己 diff 前后两个数组」干净得多。
		_ = TriggerIssueLabeled(ctx, "wh:"+delivery, &IssueLabeledEvent{
			Repo: repo, Number: num(iss, "number"), Title: str(iss, "title"),
			Body: clip(str(iss, "body"), 4000), AddedLabel: nestedStr(body, "label", "name"),
			Labels: namesOf(iss["labels"], "name"), IsPr: isPullRequest(iss),
			Actor: nestedStr(body, "sender", "login"), URL: str(iss, "html_url"),
			Source: "webhook",
		})
	}
}

// emitComment 评论。GitHub 用**同一个事件**发 Issue 与 PR 的评论——
// 靠 issue.pull_request 这个键区分，不区分的话「在 PR 里说句话」也会去派 Issue 的活。
func emitComment(ctx sokel.WebhookCtx, delivery, repo, action string, body map[string]any) {
	if action != "created" {
		return // 编辑/删除评论不触发，否则机器人会被自己改过的评论反复唤醒
	}
	iss := obj(body, "issue")
	cm := obj(body, "comment")
	author := nested(cm, "user", "login")
	bot := isBot(obj(cm, "user"))
	if isPullRequest(iss) {
		_ = TriggerPrCommented(ctx, "wh:"+delivery, &PrCommentedEvent{
			Repo: repo, Number: num(iss, "number"), Title: str(iss, "title"),
			Comment: clip(str(cm, "body"), 8000), CommentID: num(cm, "id"),
			Author: author, AuthorAssociation: str(cm, "author_association"),
			IsBot: bot, URL: str(cm, "html_url"), Source: "webhook",
		})
		return
	}
	_ = TriggerIssueCommented(ctx, "wh:"+delivery, &IssueCommentedEvent{
		Repo: repo, Number: num(iss, "number"), Title: str(iss, "title"),
		Comment: clip(str(cm, "body"), 8000), CommentID: num(cm, "id"),
		Author: author, AuthorAssociation: str(cm, "author_association"),
		IsBot: bot, URL: str(cm, "html_url"), Source: "webhook",
	})
}

// isBot 这条是不是机器人发的。
//
// **ChatOps 必须挡掉**：机器人回复自己的评论会无限循环——这是接 GitHub 机器人第一个踩的坑，
// 而且循环起来非常快（每轮一次 API 调用，几分钟就能把速率配额打光）。
// 两个判据都要：type 字段是权威的，名字后缀兜住 type 缺席的老 payload。
func isBot(user map[string]any) bool {
	if strings.EqualFold(str(user, "type"), "Bot") {
		return true
	}
	return strings.HasSuffix(str(user, "login"), "[bot]")
}

func nestedStr(m map[string]any, k1, k2 string) string { return nested(m, k1, k2) }
