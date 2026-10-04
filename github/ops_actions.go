package main

// Actions + 发布 + 仓库家务。

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/sokel-dev/sokel-official-plugins/github/schema"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

func opWorkflowRunsList(ctx plugin.Ctx, in *WorkflowRunsListIn) (*WorkflowRunsListOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	q := perPage(in.Page)
	putIf(q, "branch", in.Branch)
	putIf(q, "actor", in.Actor)
	// GitHub 把「状态」与「结论」塞在同一个 status 参数里（success/failure 其实是结论）——
	// 契约上合成一个枚举是对的，这里原样透传即可。
	putIf(q, "status", in.Status)
	path := rp + "/actions/runs"
	if w := strings.TrimSpace(in.Workflow); w != "" {
		path = rp + "/actions/workflows/" + urlPathEscape(w) + "/runs"
	}
	raw, h, err := ghCall(ctx, http.MethodGet, path, q)
	if err != nil {
		return nil, err
	}
	m := digObj(raw)
	out := &WorkflowRunsListOut{HasMore: hasNext(h)}
	for _, it := range arr(m, "workflow_runs") {
		r, _ := it.(map[string]any)
		if r == nil {
			continue
		}
		out.Runs = append(out.Runs, schema.WorkflowRun{
			ID: num(r, "id"), Name: str(r, "name"), Status: str(r, "status"),
			Conclusion: str(r, "conclusion"), Branch: str(r, "head_branch"),
			SHA: str(r, "head_sha"), Event: str(r, "event"),
			Actor: nested(r, "actor", "login"), RunNumber: num(r, "run_number"),
			Attempt: num(r, "run_attempt"), URL: str(r, "html_url"),
			CreatedAt: str(r, "created_at"), UpdatedAt: str(r, "updated_at"),
		})
	}
	out.Count = len(out.Runs)
	return out, nil
}

func opWorkflowDispatch(ctx plugin.Ctx, in *WorkflowDispatchIn) (*WorkflowDispatchOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	ref := strings.TrimSpace(in.Ref)
	if ref == "" {
		raw, _, err := ghCall(ctx, http.MethodGet, rp, nil)
		if err != nil {
			return nil, err
		}
		ref = str(digObj(raw), "default_branch")
	}
	body := map[string]any{"ref": ref}
	if len(in.Inputs) > 0 {
		// GitHub 只收字符串值，传数字/布尔会 422 且报错文字不说是这个原因。
		conv := map[string]any{}
		for k, v := range in.Inputs {
			conv[k] = fmt.Sprintf("%v", v)
		}
		body["inputs"] = conv
	}
	_, _, err = ghCall(ctx, http.MethodPost,
		rp+"/actions/workflows/"+urlPathEscape(in.Workflow)+"/dispatches", body)
	if err != nil {
		if strings.Contains(err.Error(), "422") {
			return nil, fmt.Errorf("%w\n（这个接口 422 最常见的两个原因："+
				"工作流文件里没写 on: workflow_dispatch；或者 ref 上还没有这个工作流文件）", err)
		}
		return nil, err
	}
	return &WorkflowDispatchOut{
		Dispatched: true,
		Note: "GitHub 这个接口不返回运行 ID（204 空响应）；" +
			"要拿 run_id 请隔几秒调「工作流运行记录」按分支查最新一条",
	}, nil
}

func opRunJobs(ctx plugin.Ctx, in *RunJobsIn) (*RunJobsOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	raw, _, err := ghCall(ctx, http.MethodGet,
		rp+"/actions/runs/"+strconv.Itoa(in.RunID)+"/jobs", map[string]any{"per_page": 100})
	if err != nil {
		return nil, err
	}
	m := digObj(raw)
	out := &RunJobsOut{}
	for _, it := range arr(m, "jobs") {
		j, _ := it.(map[string]any)
		if j == nil {
			continue
		}
		job := schema.Job{
			ID: num(j, "id"), Name: str(j, "name"), Status: str(j, "status"),
			Conclusion: str(j, "conclusion"), StartedAt: str(j, "started_at"),
			URL: str(j, "html_url"),
		}
		for _, sit := range arr(j, "steps") {
			s, _ := sit.(map[string]any)
			if s == nil {
				continue
			}
			concl := str(s, "conclusion")
			job.Steps = append(job.Steps, str(s, "name")+"="+concl)
			// 汇总失败步骤：调用方拿它直接发通知，不用自己再遍历一遍。
			if concl == "failure" {
				out.FailedSteps = append(out.FailedSteps, job.Name+"/"+str(s, "name"))
			}
		}
		out.Jobs = append(out.Jobs, job)
	}
	out.Count = len(out.Jobs)
	return out, nil
}

// opJobLog 取作业日志。
//
// 这个接口回 302 跳到一个短期有效的存储地址；Go 的 http.Client 默认会跟随重定向，
// 所以直接读到的就是正文。**但那个地址是带签名的**，不能把它交给下游再取一次。
func opJobLog(ctx plugin.Ctx, in *JobLogIn) (*JobLogOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	raw, _, err := ghCall(ctx, http.MethodGet,
		rp+"/actions/jobs/"+strconv.Itoa(in.JobID)+"/logs", nil)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	// 0 与「没填」在生成的结构体里都是 0，分不开——所以约定 -1 才是全文（契约里写明了）。
	tail := in.Tail
	if tail == 0 {
		tail = 200
	}
	truncated := false
	if tail > 0 && len(lines) > tail {
		lines = lines[len(lines)-tail:]
		truncated = true
	}
	return &JobLogOut{
		Log: strings.Join(lines, "\n"), Lines: len(lines), Truncated: truncated,
	}, nil
}

// —— 发布 ——

func opReleasesList(ctx plugin.Ctx, in *ReleasesListIn) (*ReleasesListOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	raw, h, err := ghCall(ctx, http.MethodGet, rp+"/releases", perPage(in.Page))
	if err != nil {
		return nil, err
	}
	list := digList(raw)
	out := &ReleasesListOut{Count: len(list), HasMore: hasNext(h)}
	for _, m := range list {
		out.Releases = append(out.Releases, toRelease(m))
	}
	return out, nil
}

func toRelease(m map[string]any) schema.Release {
	return schema.Release{
		ID: num(m, "id"), TagName: str(m, "tag_name"), Name: str(m, "name"),
		Body: clip(str(m, "body"), 8000), Draft: boolean(m, "draft"),
		Prerelease: boolean(m, "prerelease"), Author: nested(m, "author", "login"),
		URL: str(m, "html_url"), CreatedAt: str(m, "created_at"),
		PublishedAt: str(m, "published_at"),
	}
}

func opReleaseCreate(ctx plugin.Ctx, in *ReleaseCreateIn) (*ReleaseCreateOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"tag_name": in.Tag}
	putIf(body, "target_commitish", in.Target)
	putIf(body, "name", orDefault(in.Name, in.Tag))
	putIf(body, "body", in.Body)
	if in.GenerateNotes {
		body["generate_release_notes"] = true
	}
	if in.Draft {
		body["draft"] = true
	}
	if in.Prerelease {
		body["prerelease"] = true
	}
	raw, _, err := ghCall(ctx, http.MethodPost, rp+"/releases", body)
	if err != nil {
		return nil, err
	}
	m := digObj(raw)
	return &ReleaseCreateOut{
		ReleaseID: num(m, "id"), Tag: str(m, "tag_name"),
		URL: str(m, "html_url"), Body: str(m, "body"),
	}, nil
}

// —— 仓库家务 ——

func opLabelsList(ctx plugin.Ctx, in *LabelsListIn) (*LabelsListOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	raw, h, err := ghCall(ctx, http.MethodGet, rp+"/labels", perPage(in.Page))
	if err != nil {
		return nil, err
	}
	list := digList(raw)
	out := &LabelsListOut{Count: len(list), HasMore: hasNext(h)}
	for _, m := range list {
		out.Labels = append(out.Labels, schema.Label{
			Name: str(m, "name"), Color: str(m, "color"), Description: str(m, "description"),
		})
	}
	return out, nil
}

// opLabelCreate 建标签；已存在则改。
// 幂等是刻意的：labeler 机器人每次跑都想「确保这些标签存在」，
// 已存在就报错的话调用方得自己先查一遍再判断，白白多一次往返和一段分支。
func opLabelCreate(ctx plugin.Ctx, in *LabelCreateIn) (*LabelCreateOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"name": in.Name}
	putIf(body, "color", strings.TrimPrefix(strings.TrimSpace(in.Color), "#"))
	putIf(body, "description", in.Description)
	_, _, err = ghCall(ctx, http.MethodPost, rp+"/labels", body)
	if err == nil {
		return &LabelCreateOut{Name: in.Name, Created: true}, nil
	}
	if !strings.Contains(err.Error(), "422") {
		return nil, err
	}
	// 422 = 同名已存在 → 改它。
	delete(body, "name")
	if len(body) == 0 {
		return &LabelCreateOut{Name: in.Name}, nil
	}
	if _, _, err := ghCall(ctx, http.MethodPatch,
		rp+"/labels/"+urlPathEscape(in.Name), body); err != nil {
		return nil, err
	}
	return &LabelCreateOut{Name: in.Name}, nil
}

func opMilestonesList(ctx plugin.Ctx, in *MilestonesListIn) (*MilestonesListOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	raw, _, err := ghCall(ctx, http.MethodGet, rp+"/milestones", map[string]any{
		"state": orDefault(in.State, "open"), "per_page": 100,
	})
	if err != nil {
		return nil, err
	}
	list := digList(raw)
	out := &MilestonesListOut{Count: len(list)}
	for _, m := range list {
		out.Milestones = append(out.Milestones, schema.Milestone{
			Number: num(m, "number"), Title: str(m, "title"),
			Description: str(m, "description"), State: str(m, "state"),
			OpenIssues: num(m, "open_issues"), ClosedIssues: num(m, "closed_issues"),
			DueOn: str(m, "due_on"), URL: str(m, "html_url"),
		})
	}
	return out, nil
}

func opCollaboratorsList(ctx plugin.Ctx, in *CollaboratorsListIn) (*CollaboratorsListOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	q := perPage(in.Page)
	putIf(q, "permission", in.Permission)
	raw, h, err := ghCall(ctx, http.MethodGet, rp+"/collaborators", q)
	if err != nil {
		return nil, err
	}
	list := digList(raw)
	out := &CollaboratorsListOut{Count: len(list), HasMore: hasNext(h)}
	for _, m := range list {
		out.Collaborators = append(out.Collaborators, schema.Collaborator{
			Login: str(m, "login"), Permission: highestPermission(obj(m, "permissions")),
			Type: str(m, "type"), URL: str(m, "html_url"),
		})
	}
	return out, nil
}

// highestPermission GitHub 回的是一张布尔表（admin/maintain/push/triage/pull 同时为真），
// 取最高的那一档才是人想看的「他是什么角色」。
func highestPermission(p map[string]any) string {
	for _, k := range []string{"admin", "maintain", "push", "triage", "pull"} {
		if boolean(p, k) {
			return k
		}
	}
	return ""
}

// opBranchProtectionGet 查分支保护。
//
// 没有保护规则时 GitHub 回 404。翻译成 protected=false 而不是抛错——
// 「这个分支没保护」是巡检想要的答案之一，不是故障。
func opBranchProtectionGet(ctx plugin.Ctx, in *BranchProtectionGetIn) (*BranchProtectionGetOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	branch := strings.TrimSpace(in.Branch)
	if branch == "" {
		raw, _, err := ghCall(ctx, http.MethodGet, rp, nil)
		if err != nil {
			return nil, err
		}
		branch = str(digObj(raw), "default_branch")
	}
	raw, _, err := ghCall(ctx, http.MethodGet,
		rp+"/branches/"+urlPathEscape(branch)+"/protection", nil)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			return &BranchProtectionGetOut{Protected: false}, nil
		}
		return nil, err
	}
	m := digObj(raw)
	rev := obj(m, "required_pull_request_reviews")
	checks := obj(m, "required_status_checks")
	return &BranchProtectionGetOut{
		Protected:              true,
		RequiredApprovals:      num(rev, "required_approving_review_count"),
		DismissStaleReviews:    boolean(rev, "dismiss_stale_reviews"),
		RequireCodeOwnerReview: boolean(rev, "require_code_owner_reviews"),
		RequiredChecks:         namesOf(checks["contexts"], ""),
		EnforceAdmins:          boolean(obj(m, "enforce_admins"), "enabled"),
		AllowForcePush:         boolean(obj(m, "allow_force_pushes"), "enabled"),
		RequiredLinearHistory:  boolean(obj(m, "required_linear_history"), "enabled"),
	}, nil
}
