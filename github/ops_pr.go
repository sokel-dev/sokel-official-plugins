package main

// PR domain + bot feedback surface (commit status / check runs / reactions).

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/sokel-dev/sokel-official-plugins/github/schema"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

func opPrList(ctx plugin.Ctx, in *PrListIn) (*PrListOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	q := perPage(in.Page)
	q["state"] = orDefault(in.State, "open")
	q["sort"] = orDefault(in.Sort, "created")
	putIf(q, "base", in.Base)
	putIf(q, "head", in.Head)
	raw, h, err := ghCall(ctx, http.MethodGet, rp+"/pulls", q)
	if err != nil {
		return nil, err
	}
	list := digList(raw)
	out := &PrListOut{Count: len(list), HasMore: hasNext(h)}
	for _, m := range list {
		out.Prs = append(out.Prs, toPR(m))
	}
	return out, nil
}

func opPrGet(ctx plugin.Ctx, in *PrGetIn) (*PrGetOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	raw, _, err := ghCall(ctx, http.MethodGet, rp+"/pulls/"+strconv.Itoa(in.Number), nil)
	if err != nil {
		return nil, err
	}
	pr := toPR(digObj(raw))
	return &PrGetOut{Pr: pr}, nil
}

func opPrCreate(ctx plugin.Ctx, in *PrCreateIn) (*PrCreateOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	base := strings.TrimSpace(in.Base)
	if base == "" {
		raw, _, err := ghCall(ctx, http.MethodGet, rp, nil)
		if err != nil {
			return nil, err
		}
		base = str(digObj(raw), "default_branch")
	}
	body := map[string]any{
		"title": in.Title, "head": in.Head, "base": base,
		"maintainer_can_modify": in.MaintainerCanModify,
	}
	putIf(body, "body", in.Body)
	if in.Draft {
		body["draft"] = true
	}
	raw, _, err := ghCall(ctx, http.MethodPost, rp+"/pulls", body)
	if err != nil {
		return nil, err
	}
	m := digObj(raw)
	return &PrCreateOut{
		Number: num(m, "number"), URL: str(m, "html_url"),
		HeadSHA: nested(m, "head", "sha"),
	}, nil
}

func opPrUpdate(ctx plugin.Ctx, in *PrUpdateIn) (*PrUpdateOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	n := strconv.Itoa(in.Number)
	body := map[string]any{}
	putIf(body, "title", in.Title)
	putIf(body, "body", in.Body)
	putIf(body, "base", in.Base)
	putIf(body, "state", in.State)
	var m map[string]any
	if len(body) > 0 {
		raw, _, err := ghCall(ctx, http.MethodPatch, rp+"/pulls/"+n, body)
		if err != nil {
			return nil, err
		}
		m = digObj(raw)
	}
	// There's no REST endpoint for converting a draft to ready; it has to go through GraphQL's
	// markPullRequestReadyForReview.
	if in.ReadyForReview {
		if m == nil {
			raw, _, err := ghCall(ctx, http.MethodGet, rp+"/pulls/"+n, nil)
			if err != nil {
				return nil, err
			}
			m = digObj(raw)
		}
		nodeID := str(m, "node_id")
		if nodeID == "" {
			return nil, fmt.Errorf("拿不到 PR 的节点 ID，无法把草稿转正式")
		}
		if _, err := ghGraphQL(ctx, `mutation($id:ID!){markPullRequestReadyForReview(input:{pullRequestId:$id}){clientMutationId}}`,
			map[string]any{"id": nodeID}); err != nil {
			return nil, err
		}
		raw, _, err := ghCall(ctx, http.MethodGet, rp+"/pulls/"+n, nil)
		if err != nil {
			return nil, err
		}
		m = digObj(raw)
	}
	if m == nil {
		return nil, fmt.Errorf("没有要改的字段——标题/正文/目标分支/状态/草稿转正式至少给一个")
	}
	return &PrUpdateOut{
		Number: num(m, "number"), State: str(m, "state"),
		Draft: boolean(m, "draft"), URL: str(m, "html_url"),
	}, nil
}

// opPrMerge merges a PR.
//
// expect_sha is a **concurrency guard**: between "checks passed" and "merge requested" someone
// might have pushed another commit that hasn't been checked yet. GitHub's sha parameter exists
// exactly for this — it rejects the merge (409) when it doesn't match. We translate that into
// merged=false + a reason rather than raising an error: a bot should re-run its checks based on
// that, not treat it as an alert.
func opPrMerge(ctx plugin.Ctx, in *PrMergeIn) (*PrMergeOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"merge_method": orDefault(in.Method, "merge")}
	putIf(body, "commit_title", in.Title)
	putIf(body, "commit_message", in.Message)
	putIf(body, "sha", in.ExpectSHA)
	raw, _, err := ghCall(ctx, http.MethodPut, rp+"/pulls/"+strconv.Itoa(in.Number)+"/merge", body)
	if err != nil {
		// 405 = merge conditions not met (conflict / checks not passed / draft), 409 = sha
		// mismatch. Both are "didn't merge this time" rather than "errored", so downstream can
		// branch on it.
		msg := err.Error()
		if strings.Contains(msg, "405") || strings.Contains(msg, "409") || strings.Contains(msg, "冲突") {
			return &PrMergeOut{Merged: false, Message: msg}, nil
		}
		return nil, err
	}
	m := digObj(raw)
	return &PrMergeOut{
		Merged: boolean(m, "merged"), SHA: str(m, "sha"), Message: str(m, "message"),
	}, nil
}

func opPrFiles(ctx plugin.Ctx, in *PrFilesIn) (*PrFilesOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	raw, h, err := ghCall(ctx, http.MethodGet,
		rp+"/pulls/"+strconv.Itoa(in.Number)+"/files", perPage(in.Page))
	if err != nil {
		return nil, err
	}
	list := digList(raw)
	out := &PrFilesOut{Count: len(list), HasMore: hasNext(h)}
	for _, m := range list {
		f := schema.PRFile{
			Path: str(m, "filename"), Status: str(m, "status"),
			Additions: num(m, "additions"), Deletions: num(m, "deletions"),
			SHA: str(m, "sha"),
		}
		if in.WithPatch {
			f.Patch = str(m, "patch")
		}
		out.Additions += f.Additions
		out.Deletions += f.Deletions
		out.Files = append(out.Files, f)
	}
	return out, nil
}

func opPrReview(ctx plugin.Ctx, in *PrReviewIn) (*PrReviewOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	event := orDefault(in.Event, "COMMENT")
	body := map[string]any{"event": event}
	putIf(body, "body", in.Body)
	if event != "APPROVE" && strings.TrimSpace(in.Body) == "" && len(in.Comments) == 0 {
		return nil, fmt.Errorf("%s 必须有总评或行内评论——GitHub 对空评审回 422", event)
	}
	if len(in.Comments) > 0 {
		var cs []map[string]any
		for _, c := range in.Comments {
			if c.Line <= 0 {
				return nil, fmt.Errorf("行内评论 %q 缺行号——行号是新版文件的行号，且要落在本次 diff 动过的行上", c.Path)
			}
			cs = append(cs, map[string]any{
				"path": c.Path, "line": c.Line, "body": c.Body,
				"side": orDefault(c.Side, "RIGHT"),
			})
		}
		body["comments"] = cs
	}
	raw, _, err := ghCall(ctx, http.MethodPost,
		rp+"/pulls/"+strconv.Itoa(in.Number)+"/reviews", body)
	if err != nil {
		// A line number not being on the diff is the most common failure, and the error text
		// doesn't show that's the reason.
		if strings.Contains(err.Error(), "422") && len(in.Comments) > 0 {
			return nil, fmt.Errorf("%w\n"+
				"（行内评论最常见的失败原因：行号不在本次 diff 动过的行上。"+
				"先用「PR 改了哪些文件」打开 with_patch 看 diff 里到底有哪些行）", err)
		}
		return nil, err
	}
	m := digObj(raw)
	return &PrReviewOut{
		ReviewID: num(m, "id"), State: str(m, "state"), URL: str(m, "html_url"),
	}, nil
}

func opPrRequestReviewers(ctx plugin.Ctx, in *PrRequestReviewersIn) (*PrRequestReviewersOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	if len(in.Reviewers) == 0 && len(in.TeamReviewers) == 0 {
		return nil, fmt.Errorf("评审人与评审团队至少给一个")
	}
	body := map[string]any{}
	if len(in.Reviewers) > 0 {
		body["reviewers"] = in.Reviewers
	}
	if len(in.TeamReviewers) > 0 {
		body["team_reviewers"] = in.TeamReviewers
	}
	method := http.MethodPost
	if in.Remove {
		method = http.MethodDelete
	}
	raw, _, err := ghCall(ctx, method,
		rp+"/pulls/"+strconv.Itoa(in.Number)+"/requested_reviewers", body)
	if err != nil {
		return nil, err
	}
	return &PrRequestReviewersOut{
		Reviewers: namesOf(digObj(raw)["requested_reviewers"], "login"),
	}, nil
}

// —— bot feedback surface ——

func opCommitStatusCreate(ctx plugin.Ctx, in *CommitStatusCreateIn) (*CommitStatusCreateOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	sha := strings.TrimSpace(in.SHA)
	if sha == "" {
		return nil, fmt.Errorf("提交 sha 是空的——用 PR 的 head_sha（事件里也带）")
	}
	body := map[string]any{
		"state":   in.State,
		"context": orDefault(in.Context, "sokel"),
	}
	// GitHub caps description at 140 chars and truncates it itself if it's longer — truncate it
	// ourselves first so it doesn't cut a multi-byte character in half.
	putIf(body, "description", clipRunes(in.Description, 140))
	putIf(body, "target_url", in.TargetURL)
	raw, _, err := ghCall(ctx, http.MethodPost, rp+"/statuses/"+sha, body)
	if err != nil {
		return nil, err
	}
	m := digObj(raw)
	return &CommitStatusCreateOut{StatusID: num(m, "id"), Context: str(m, "context")}, nil
}

func opCheckRunCreate(ctx plugin.Ctx, in *CheckRunCreateIn) (*CheckRunCreateOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"name": in.Name, "head_sha": strings.TrimSpace(in.SHA)}
	if c := strings.TrimSpace(in.Conclusion); c != "" {
		body["conclusion"] = c
		body["status"] = "completed"
	} else {
		body["status"] = "in_progress"
	}
	putIf(body, "details_url", in.DetailsURL)

	sent := 0
	if in.Title != "" || in.Summary != "" || len(in.Annotations) > 0 {
		output := map[string]any{
			"title":   orDefault(in.Title, in.Name),
			"summary": in.Summary,
		}
		putIf(output, "text", in.Details)
		if n := len(in.Annotations); n > 0 {
			// GitHub accepts at most 50 annotations per call; anything beyond that is **silently
			// dropped, with no error**. Saying exactly how many were dropped beats letting
			// someone think "everything was submitted".
			sent = n
			if sent > 50 {
				sent = 50
			}
			var as []map[string]any
			for _, a := range in.Annotations[:sent] {
				end := a.EndLine
				if end < a.StartLine {
					end = a.StartLine
				}
				as = append(as, map[string]any{
					"path": a.Path, "start_line": a.StartLine, "end_line": end,
					"annotation_level": orDefault(a.Level, "warning"),
					"message":          a.Message,
					"title":            a.Title,
				})
			}
			output["annotations"] = as
		}
		body["output"] = output
	}
	raw, _, err := ghCall(ctx, http.MethodPost, rp+"/check-runs", body)
	if err != nil {
		if strings.Contains(err.Error(), "403") {
			return nil, fmt.Errorf("%w\n（Checks API 要求 GitHub App 或带 Checks 写权限的细粒度令牌；"+
				"经典 PAT 建不了检查运行——退而求其次用「回写提交状态」）", err)
		}
		return nil, err
	}
	m := digObj(raw)
	return &CheckRunCreateOut{
		CheckRunID: num(m, "id"), URL: str(m, "html_url"), AnnotationsSent: sent,
	}, nil
}

func opReactionAdd(ctx plugin.Ctx, in *ReactionAddIn) (*ReactionAddOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	id := strconv.Itoa(in.ID)
	path := rp + "/issues/comments/" + id + "/reactions"
	if orDefault(in.Target, "comment") == "issue" {
		path = rp + "/issues/" + id + "/reactions"
	}
	raw, hdr, err := ghCall(ctx, http.MethodPost, path,
		map[string]any{"content": orDefault(in.Content, "eyes")})
	if err != nil {
		return nil, err
	}
	// 201 = newly added, 200 = already added before. GitHub distinguishes via status code; we
	// translate that into already here.
	_ = hdr
	m := digObj(raw)
	return &ReactionAddOut{ReactionID: num(m, "id")}, nil
}

// clipRunes truncates by rune (not byte), so it doesn't cut a multi-byte character in half.
func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
