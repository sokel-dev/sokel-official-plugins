package main

// 平台代收 webhook（零延迟版事件；与 events.go 的轮询源并存，按需选一条）：
// GitLab 项目 Settings→Webhooks 指向平台 /hooks/{token}，勾 Push/MR/Issue/Pipeline。
//
// 验签：GitLab 把 Secret token 原样放 X-Gitlab-Token 头（不是 HMAC）——与凭证里的
// webhook_secret 比对即可；凭证没配 secret 则跳过校验（内网自建可接受，文档写明）。
//
// event_id 与轮询源**故意不同源**（webhook 用 wh: 前缀）：两条路并开时同一个动作
// 会各触发一次——文档写明「二选一」，不在这里硬去重（webhook payload 与 Events API
// 的对象 id 不同域，硬拼会造出脆弱的映射）。

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

func handleWebhook(ctx sokel.WebhookCtx, req *sokel.WebhookRequest) sokel.WebhookResponse {
	secret := strings.TrimSpace(ctx.Credential()["webhook_secret"])
	if secret != "" && req.Header("X-Gitlab-Token") != secret {
		return sokel.Text(401, "invalid token")
	}
	event := req.Header("X-Gitlab-Event")
	var body map[string]any
	if json.Unmarshal(req.Body, &body) != nil {
		return sokel.Text(400, "bad payload")
	}
	proj := nestedStr(body, "project", "path_with_namespace")
	switch event {
	case "Push Hook":
		after, _ := body["after"].(string)
		ref := strings.TrimPrefix(str(body, "ref"), "refs/heads/")
		count := 0
		if v, ok := body["total_commits_count"].(float64); ok {
			count = int(v)
		}
		title := ""
		if arr, ok := body["commits"].([]any); ok && len(arr) > 0 {
			if m, ok := arr[len(arr)-1].(map[string]any); ok {
				title = str(m, "title")
			}
		}
		_ = TriggerCommitPushed(ctx, "wh:"+after, &CommitPushedEvent{
			Project: proj, Ref: ref, CommitID: after, CommitTitle: title,
			CommitCount: count, Author: str(body, "user_username"), Raw: body, Source: "webhook",
		})
	case "Merge Request Hook":
		attrs, _ := body["object_attributes"].(map[string]any)
		iid := num(attrs, "iid")
		action := str(attrs, "action")
		e := fmt.Sprintf("wh:mr:%d:%s:%v", iid, action, attrs["updated_at"])
		switch action {
		case "open":
			_ = TriggerMrOpened(ctx, e, &MrOpenedEvent{Project: proj, Iid: iid,
				Title: str(attrs, "title"), Author: nestedStr(body, "user", "username"), Raw: body, Source: "webhook"})
		case "merge":
			_ = TriggerMrMerged(ctx, e, &MrMergedEvent{Project: proj, Iid: iid,
				Title: str(attrs, "title"), Author: nestedStr(body, "user", "username"), Raw: body, Source: "webhook"})
		}
	case "Issue Hook":
		attrs, _ := body["object_attributes"].(map[string]any)
		if added := addedLabels(body); str(attrs, "action") == "update" && len(added) > 0 {
			// event_id 带上 updated_at：同一个标签摘掉再打回来是两次真事件，
			// 只按 iid+标签名去重会把第二次吃掉。
			_ = TriggerIssueLabeled(ctx, fmt.Sprintf("wh:issue-label:%d:%s", num(attrs, "iid"), str(attrs, "updated_at")),
				&IssueLabeledEvent{
					Project: proj, Iid: num(attrs, "iid"), Title: str(attrs, "title"),
					Description: str(attrs, "description"), AddedLabels: added,
					Labels: hookLabels(body, attrs), URL: str(attrs, "url"),
					Author: nestedStr(body, "user", "username"),
				})
		}
		if str(attrs, "action") == "open" {
			_ = TriggerIssueOpened(ctx, fmt.Sprintf("wh:issue:%d", num(attrs, "iid")), &IssueOpenedEvent{
				Project: proj, Iid: num(attrs, "iid"), Title: str(attrs, "title"),
				Description: str(attrs, "description"), Labels: hookLabels(body, attrs),
				URL:    str(attrs, "url"),
				Author: nestedStr(body, "user", "username"), Raw: body, Source: "webhook"})
		}
	case "Note Hook":
		// 评论。只认 Issue 上的评论——MR / 提交 / 代码行评论都走同一个 Hook,
		// 不区分的话「在 MR 里说句话」也会去派 Issue 的活。
		attrs, _ := body["object_attributes"].(map[string]any)
		// system 为真 = 改标签/指派这类动作生成的系统 note,不是人说的话。
		// 不挡住的话「给 Issue 加个标签」也会去派一次活。
		sysNote, _ := attrs["system"].(bool)
		if mr, _ := body["merge_request"].(map[string]any); str(attrs, "noteable_type") == "MergeRequest" && !sysNote {
			_ = TriggerMrCommented(ctx, fmt.Sprintf("wh:mrnote:%d", num(attrs, "id")), &MrCommentedEvent{
				Project: proj, Iid: num(mr, "iid"), Title: str(mr, "title"),
				Comment: str(attrs, "note"), Author: nestedStr(body, "user", "username"),
				URL: str(attrs, "url"),
			})
		}
		if str(attrs, "noteable_type") == "Issue" && !sysNote {
			iss, _ := body["issue"].(map[string]any)
			_ = TriggerIssueCommented(ctx, fmt.Sprintf("wh:note:%d", num(attrs, "id")), &IssueCommentedEvent{
				Project: proj, Iid: num(iss, "iid"), Title: str(iss, "title"),
				Comment: str(attrs, "note"), Author: nestedStr(body, "user", "username"),
				URL: str(attrs, "url"),
			})
		}
	case "Pipeline Hook":
		attrs, _ := body["object_attributes"].(map[string]any)
		if str(attrs, "status") == "failed" {
			_ = TriggerPipelineFailed(ctx, fmt.Sprintf("wh:pipe:%d", num(attrs, "id")), &PipelineFailedEvent{
				Project: proj, PipelineID: num(attrs, "id"), Ref: str(attrs, "ref"),
				SHA: str(attrs, "sha"), WebURL: nestedStr(body, "project", "web_url"),
			})
		}
	}
	// 认不出的事件类型也回 200：GitLab 会按项目配置发一堆类型，不该让它反复重试。
	return sokel.OK()
}

func nestedStr(m map[string]any, k1, k2 string) string {
	if mm, ok := m[k1].(map[string]any); ok {
		return str(mm, k2)
	}
	return ""
}

// hookLabels 从 Issue Hook 取标签名。GitLab 把它放两处且形状不同：
// 顶层 labels 是对象数组 [{title:…}]，object_attributes.labels 有时是同款、有时缺席——
// 两处都试，取到哪个算哪个。
func hookLabels(body, attrs map[string]any) []string {
	for _, src := range []any{body["labels"], attrs["labels"]} {
		arr, ok := src.([]any)
		if !ok || len(arr) == 0 {
			continue
		}
		var out []string
		for _, it := range arr {
			switch v := it.(type) {
			case string:
				out = append(out, v)
			case map[string]any:
				if t := str(v, "title"); t != "" {
					out = append(out, t)
				}
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

// addedLabels 这次 update 新增了哪些标签。
//
// GitLab 把标签变更放在 changes.labels 的 previous/current 两个数组里（元素是标签对象，
// 名字在 title 上）。只有真新增才回非空——摘标签同样是一次 update，不该当成「派活信号」。
func addedLabels(body map[string]any) []string {
	ch, _ := body["changes"].(map[string]any)
	lb, _ := ch["labels"].(map[string]any)
	if lb == nil {
		return nil
	}
	titles := func(v any) map[string]bool {
		out := map[string]bool{}
		arr, _ := v.([]any)
		for _, it := range arr {
			if m, ok := it.(map[string]any); ok {
				if t := str(m, "title"); t != "" {
					out[t] = true
				}
			}
		}
		return out
	}
	prev, cur := titles(lb["previous"]), titles(lb["current"])
	var added []string
	for t := range cur {
		if !prev[t] {
			added = append(added, t)
		}
	}
	sort.Strings(added) // 稳定顺序：event_id 与出参都别随 map 遍历顺序抖
	return added
}
