package main

// Actions + releases + repo housekeeping.

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
	// GitHub packs both "status" and "conclusion" into the same status parameter
	// (success/failure are actually conclusions) — merging them into one enum at the contract
	// level is correct, so we just pass it through as-is here.
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
		// GitHub only accepts string values; passing a number/bool gives a 422 whose error text
		// won't say that's the reason.
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
			// Aggregate failed steps so the caller can send a notification directly, without
			// having to iterate again itself.
			if concl == "failure" {
				out.FailedSteps = append(out.FailedSteps, job.Name+"/"+str(s, "name"))
			}
		}
		out.Jobs = append(out.Jobs, job)
	}
	out.Count = len(out.Jobs)
	return out, nil
}

// opJobLog fetches a job's log.
//
// This endpoint responds with a 302 to a short-lived storage URL; Go's http.Client follows
// redirects by default, so what we read directly is the log body. **But that URL is signed** —
// it must not be handed to a downstream consumer to fetch again.
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
	// 0 and "not set" are both 0 in the generated struct and can't be told apart — so by
	// convention -1 means the full text (documented in the contract).
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

// —— releases ——

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

// —— repo housekeeping ——

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

// opLabelCreate creates a label; if it already exists, updates it instead.
// The idempotence is deliberate: a labeler bot wants to "make sure these labels exist" on every
// run, and if erroring on an existing label were the behavior, the caller would have to check
// first anyway — a wasted round trip and a branch of its own.
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
	// 422 = a label with that name already exists -> update it.
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

// highestPermission: GitHub returns a boolean table (admin/maintain/push/triage/pull can all be
// true at once); taking the highest tier is what a human actually wants to see as "their role".
func highestPermission(p map[string]any) string {
	for _, k := range []string{"admin", "maintain", "push", "triage", "pull"} {
		if boolean(p, k) {
			return k
		}
	}
	return ""
}

// opBranchProtectionGet looks up branch protection.
//
// GitHub returns 404 when there's no protection rule. We translate that to protected=false rather
// than raising an error — "this branch isn't protected" is one of the valid answers an audit
// wants, not a failure.
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
