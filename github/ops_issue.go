package main

// Issue 域。注意 GitHub 的 /issues 会连 PR 一起返回（见 schema 包顶注第一条）。

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/sokel-dev/sokel-official-plugins/github/schema"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

func opIssuesList(ctx plugin.Ctx, in *IssuesListIn) (*IssuesListOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	q := perPage(in.Page)
	q["state"] = orDefault(in.State, "open")
	q["sort"] = orDefault(in.Sort, "created")
	q["direction"] = orDefault(in.Direction, "desc")
	putIf(q, "assignee", in.Assignee)
	putIf(q, "creator", in.Creator)
	putIf(q, "milestone", in.Milestone)
	putIf(q, "since", in.Since)
	if len(in.Labels) > 0 {
		q["labels"] = strings.Join(in.Labels, ",")
	}
	raw, h, err := ghCall(ctx, http.MethodGet, rp+"/issues", q)
	if err != nil {
		return nil, err
	}
	list := digList(raw)
	out := &IssuesListOut{HasMore: hasNext(h)}
	for _, m := range list {
		// 默认剔掉 PR。**注意 has_more 仍按上游分页来**：剔掉之后本页可能只剩两条，
		// 但那不代表没有下一页——所以 count 报的是剔完的条数，has_more 报的是上游的。
		if isPullRequest(m) && !in.IncludePrs {
			out.DroppedPrs++
			continue
		}
		out.Issues = append(out.Issues, toIssue(m))
	}
	out.Count = len(out.Issues)
	return out, nil
}

func opIssueGet(ctx plugin.Ctx, in *IssueGetIn) (*IssueGetOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	n := strconv.Itoa(in.Number)
	raw, _, err := ghCall(ctx, http.MethodGet, rp+"/issues/"+n, nil)
	if err != nil {
		return nil, err
	}
	iss := toIssue(digObj(raw))
	out := &IssueGetOut{Issue: iss}
	if in.WithComments {
		craw, _, err := ghCall(ctx, http.MethodGet, rp+"/issues/"+n+"/comments",
			map[string]any{"per_page": 100})
		if err != nil {
			return nil, err
		}
		for _, m := range digList(craw) {
			out.Comments = append(out.Comments, schema.Comment{
				ID: num(m, "id"), Author: nested(m, "user", "login"),
				Body: clip(str(m, "body"), 4000), URL: str(m, "html_url"),
				CreatedAt: str(m, "created_at"),
			})
		}
	}
	return out, nil
}

func opIssueCreate(ctx plugin.Ctx, in *IssueCreateIn) (*IssueCreateOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"title": in.Title}
	if s := strings.TrimSpace(in.Body); s != "" {
		body["body"] = s
	}
	if len(in.Labels) > 0 {
		body["labels"] = in.Labels
	}
	if len(in.Assignees) > 0 {
		body["assignees"] = in.Assignees
	}
	if ms := strings.TrimSpace(in.Milestone); ms != "" {
		n, err := milestoneNumber(ctx, rp, ms)
		if err != nil {
			return nil, err
		}
		body["milestone"] = n
	}
	raw, _, err := ghCall(ctx, http.MethodPost, rp+"/issues", body)
	if err != nil {
		return nil, err
	}
	m := digObj(raw)
	return &IssueCreateOut{Number: num(m, "number"), URL: str(m, "html_url")}, nil
}

func opIssueUpdate(ctx plugin.Ctx, in *IssueUpdateIn) (*IssueUpdateOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	body := map[string]any{}
	putIf(body, "title", in.Title)
	putIf(body, "body", in.Body)
	putIf(body, "state", in.State)
	putIf(body, "state_reason", in.StateReason)
	if ms := strings.TrimSpace(in.Milestone); ms != "" {
		if strings.EqualFold(ms, "none") {
			body["milestone"] = nil
		} else {
			n, err := milestoneNumber(ctx, rp, ms)
			if err != nil {
				return nil, err
			}
			body["milestone"] = n
		}
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("没有要改的字段——标题/正文/状态/里程碑至少给一个")
	}
	raw, _, err := ghCall(ctx, http.MethodPatch, rp+"/issues/"+strconv.Itoa(in.Number), body)
	if err != nil {
		return nil, err
	}
	m := digObj(raw)
	return &IssueUpdateOut{
		Number: num(m, "number"), State: str(m, "state"), URL: str(m, "html_url"),
	}, nil
}

// milestoneNumber 里程碑「编号或标题」→ 编号。
// 允许填标题是因为画布上写死一个数字编号很脆——里程碑重建一次编号就变了。
func milestoneNumber(ctx plugin.Ctx, rp, v string) (int, error) {
	if n, err := strconv.Atoi(v); err == nil {
		return n, nil
	}
	raw, _, err := ghCall(ctx, http.MethodGet, rp+"/milestones",
		map[string]any{"state": "all", "per_page": 100})
	if err != nil {
		return 0, err
	}
	var titles []string
	for _, m := range digList(raw) {
		t := str(m, "title")
		if strings.EqualFold(t, v) {
			return num(m, "number"), nil
		}
		titles = append(titles, t)
	}
	return 0, fmt.Errorf("找不到里程碑 %q——现有的是：%s", v, strings.Join(titles, " / "))
}

func opIssueComment(ctx plugin.Ctx, in *IssueCommentIn) (*IssueCommentOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	raw, _, err := ghCall(ctx, http.MethodPost,
		rp+"/issues/"+strconv.Itoa(in.Number)+"/comments", map[string]any{"body": in.Body})
	if err != nil {
		return nil, err
	}
	m := digObj(raw)
	return &IssueCommentOut{CommentID: num(m, "id"), URL: str(m, "html_url")}, nil
}

func opIssueLabel(ctx plugin.Ctx, in *IssueLabelIn) (*IssueLabelOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	base := rp + "/issues/" + strconv.Itoa(in.Number) + "/labels"
	switch orDefault(in.Mode, "add") {
	case "set":
		raw, _, err := ghCall(ctx, http.MethodPut, base, map[string]any{"labels": in.Labels})
		if err != nil {
			return nil, err
		}
		return &IssueLabelOut{Labels: namesOf(anyOf(raw), "name")}, nil
	case "remove":
		// 逐个删——GitHub 没有批量删除接口。删不存在的标签会 404，忽略它：
		// 「确保这个标签不在」是幂等诉求，本来就不在不算失败。
		for _, l := range in.Labels {
			_, _, err := ghCall(ctx, http.MethodDelete, base+"/"+urlPathEscape(l), nil)
			if err != nil && !strings.Contains(err.Error(), "404") {
				return nil, err
			}
		}
		raw, _, err := ghCall(ctx, http.MethodGet, base, nil)
		if err != nil {
			return nil, err
		}
		return &IssueLabelOut{Labels: namesOf(anyOf(raw), "name")}, nil
	default:
		raw, _, err := ghCall(ctx, http.MethodPost, base, map[string]any{"labels": in.Labels})
		if err != nil {
			return nil, err
		}
		return &IssueLabelOut{Labels: namesOf(anyOf(raw), "name")}, nil
	}
}

func opIssueAssign(ctx plugin.Ctx, in *IssueAssignIn) (*IssueAssignOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	base := rp + "/issues/" + strconv.Itoa(in.Number) + "/assignees"
	method := http.MethodPost
	if orDefault(in.Mode, "add") == "remove" {
		method = http.MethodDelete
	}
	raw, _, err := ghCall(ctx, method, base, map[string]any{"assignees": in.Assignees})
	if err != nil {
		return nil, err
	}
	return &IssueAssignOut{Assignees: namesOf(digObj(raw)["assignees"], "login")}, nil
}

// anyOf 应答可能是数组也可能是对象，统一成 any 交给 namesOf。
func anyOf(raw []byte) any {
	if list := digList(raw); len(list) > 0 {
		out := make([]any, 0, len(list))
		for _, m := range list {
			out = append(out, m)
		}
		return out
	}
	if m := digObj(raw); m != nil {
		return m["labels"]
	}
	return nil
}
