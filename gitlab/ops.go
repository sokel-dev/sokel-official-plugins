package main

// 全部操作实现。列表统一 per_page=50 + page 入参；应答只解契约要的字段。

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/sokel-dev/sokel-official-plugins/gitlab/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

func pageOf(p int) string {
	if p <= 0 {
		p = 1
	}
	return strconv.Itoa(p)
}

// —— 项目 ——

func opProjectsList(ctx plugin.Ctx, in *ProjectsListIn) (*ProjectsListOut, error) {
	params := map[string]any{"membership": "true", "per_page": 50, "page": pageOf(in.Page),
		"order_by": "last_activity_at"}
	if s := strings.TrimSpace(in.Search); s != "" {
		params["search"] = s
	}
	raw, hdr, err := glCall(ctx, http.MethodGet, "/projects", params)
	if err != nil {
		return nil, err
	}
	out := &ProjectsListOut{}
	for _, m := range digList(raw) {
		out.Projects = append(out.Projects, schema.Project{
			ID: num(m, "id"), Path: str(m, "path_with_namespace"), Name: str(m, "name"),
			DefaultBranch: str(m, "default_branch"), WebURL: str(m, "web_url"),
			LastActivity: str(m, "last_activity_at"),
		})
	}
	out.Count = len(out.Projects)
	out.Total, out.HasMore = pageInfo(hdr)
	return out, nil
}

// —— 仓库 ——

func opFileGet(ctx plugin.Ctx, in *FileGetIn) (*FileGetOut, error) {
	p, err := pid(in.Project)
	if err != nil {
		return nil, err
	}
	path := strings.TrimSpace(in.Path)
	if path == "" {
		return nil, fmt.Errorf("文件路径是空的（仓库内路径，如 config/app.yaml）")
	}
	params := map[string]any{}
	if r := strings.TrimSpace(in.Ref); r != "" {
		params["ref"] = r
	} else {
		// files 接口 ref 必填：先取默认分支。
		braw, _, berr := glCall(ctx, http.MethodGet, "/projects/"+p, nil)
		if berr != nil {
			return nil, berr
		}
		var proj map[string]any
		_ = json.Unmarshal(braw, &proj)
		params["ref"] = str(proj, "default_branch")
	}
	raw, _, err := glCall(ctx, http.MethodGet, "/projects/"+p+"/repository/files/"+pathEscapeAll(path), params)
	if err != nil {
		return nil, err
	}
	var f struct {
		Content      string `json:"content"`
		Encoding     string `json:"encoding"`
		LastCommitID string `json:"last_commit_id"`
	}
	_ = json.Unmarshal(raw, &f)
	content := f.Content
	if f.Encoding == "base64" {
		if b, derr := base64.StdEncoding.DecodeString(f.Content); derr == nil {
			content = string(b)
		}
	}
	return &FileGetOut{Content: content, LastCommitID: f.LastCommitID}, nil
}

// pathEscapeAll 文件路径整体编码（含 /）——GitLab files 接口的约定。
func pathEscapeAll(p string) string {
	return strings.ReplaceAll(strings.ReplaceAll(p, "%", "%25"), "/", "%2F")
}

func opFileWrite(ctx plugin.Ctx, in *FileWriteIn) (*FileWriteOut, error) {
	p, err := pid(in.Project)
	if err != nil {
		return nil, err
	}
	path, msg := strings.TrimSpace(in.Path), strings.TrimSpace(in.Message)
	if path == "" || msg == "" {
		return nil, fmt.Errorf("文件路径与提交信息都要填")
	}
	branch := strings.TrimSpace(in.Branch)
	if branch == "" {
		braw, _, berr := glCall(ctx, http.MethodGet, "/projects/"+p, nil)
		if berr != nil {
			return nil, berr
		}
		var proj map[string]any
		_ = json.Unmarshal(braw, &proj)
		branch = str(proj, "default_branch")
	}
	// create 还是 update？先探文件在不在（分支上）——commits 接口的 action 必须精确。
	action := "update"
	if _, _, gerr := glCall(ctx, http.MethodGet,
		"/projects/"+p+"/repository/files/"+pathEscapeAll(path), map[string]any{"ref": branch}); gerr != nil {
		if strings.Contains(gerr.Error(), "404") {
			action = "create"
		}
		// 其他错误也按 update 试——commits 接口会给出更准的报错。
	}
	body := map[string]any{
		"branch": branch, "commit_message": msg,
		"actions": []map[string]any{{"action": action, "file_path": path, "content": in.Content}},
	}
	if sb := strings.TrimSpace(in.StartBranch); sb != "" {
		body["start_branch"] = sb
	}
	raw, _, err := glCall(ctx, http.MethodPost, "/projects/"+p+"/repository/commits", body)
	if err != nil {
		return nil, err
	}
	var c map[string]any
	_ = json.Unmarshal(raw, &c)
	return &FileWriteOut{CommitID: str(c, "id"), WebURL: str(c, "web_url")}, nil
}

func opBranchesList(ctx plugin.Ctx, in *BranchesListIn) (*BranchesListOut, error) {
	p, err := pid(in.Project)
	if err != nil {
		return nil, err
	}
	params := map[string]any{"per_page": 50, "page": pageOf(in.Page)}
	if s := strings.TrimSpace(in.Search); s != "" {
		params["search"] = s
	}
	raw, hdr, err := glCall(ctx, http.MethodGet, "/projects/"+p+"/repository/branches", params)
	if err != nil {
		return nil, err
	}
	out := &BranchesListOut{}
	for _, m := range digList(raw) {
		out.Branches = append(out.Branches, schema.Branch{
			Name: str(m, "name"), Default: boolean(m, "default"), Protected: boolean(m, "protected"),
			CommitID: nested(m, "commit", "id"),
		})
	}
	out.Count = len(out.Branches)
	out.Total, out.HasMore = pageInfo(hdr)
	return out, nil
}

func opCommitsList(ctx plugin.Ctx, in *CommitsListIn) (*CommitsListOut, error) {
	p, err := pid(in.Project)
	if err != nil {
		return nil, err
	}
	params := map[string]any{"per_page": 50, "page": pageOf(in.Page)}
	if r := strings.TrimSpace(in.Ref); r != "" {
		params["ref_name"] = r
	}
	if s := strings.TrimSpace(in.Since); s != "" {
		params["since"] = s
	}
	raw, hdr, err := glCall(ctx, http.MethodGet, "/projects/"+p+"/repository/commits", params)
	if err != nil {
		return nil, err
	}
	out := &CommitsListOut{}
	for _, m := range digList(raw) {
		out.Commits = append(out.Commits, schema.Commit{
			ID: str(m, "id"), ShortID: str(m, "short_id"), Title: str(m, "title"),
			Author: str(m, "author_name"), CreatedAt: str(m, "created_at"), WebURL: str(m, "web_url"),
		})
	}
	out.Count = len(out.Commits)
	out.Total, out.HasMore = pageInfo(hdr)
	return out, nil
}

// —— MR ——

func mrFrom(m map[string]any) schema.Mr {
	return schema.Mr{
		IID: num(m, "iid"), Title: str(m, "title"), State: str(m, "state"),
		SourceBranch: str(m, "source_branch"), TargetBranch: str(m, "target_branch"),
		Author: nested(m, "author", "username"), WebURL: str(m, "web_url"),
		MergeStatus: str(m, "detailed_merge_status"),
	}
}

func opMrList(ctx plugin.Ctx, in *MrListIn) (*MrListOut, error) {
	p, err := pid(in.Project)
	if err != nil {
		return nil, err
	}
	params := map[string]any{"per_page": 50, "page": pageOf(in.Page), "state": defStr(in.State, "opened")}
	if t := strings.TrimSpace(in.TargetBranch); t != "" {
		params["target_branch"] = t
	}
	raw, hdr, err := glCall(ctx, http.MethodGet, "/projects/"+p+"/merge_requests", params)
	if err != nil {
		return nil, err
	}
	out := &MrListOut{}
	for _, m := range digList(raw) {
		out.Mrs = append(out.Mrs, mrFrom(m))
	}
	out.Count = len(out.Mrs)
	out.Total, out.HasMore = pageInfo(hdr)
	return out, nil
}

func defStr(v, def string) string {
	if s := strings.TrimSpace(v); s != "" {
		return s
	}
	return def
}

func opMrCreate(ctx plugin.Ctx, in *MrCreateIn) (*MrCreateOut, error) {
	p, err := pid(in.Project)
	if err != nil {
		return nil, err
	}
	src, title := strings.TrimSpace(in.SourceBranch), strings.TrimSpace(in.Title)
	if src == "" || title == "" {
		return nil, fmt.Errorf("源分支与标题都要填")
	}
	target := strings.TrimSpace(in.TargetBranch)
	if target == "" {
		braw, _, berr := glCall(ctx, http.MethodGet, "/projects/"+p, nil)
		if berr != nil {
			return nil, berr
		}
		var proj map[string]any
		_ = json.Unmarshal(braw, &proj)
		target = str(proj, "default_branch")
	}
	raw, _, err := glCall(ctx, http.MethodPost, "/projects/"+p+"/merge_requests", map[string]any{
		"source_branch": src, "target_branch": target, "title": title,
		"description": in.Description, "remove_source_branch": in.RemoveSourceBranch,
	})
	if err != nil {
		return nil, err
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return &MrCreateOut{Iid: num(m, "iid"), WebURL: str(m, "web_url")}, nil
}

func opMrMerge(ctx plugin.Ctx, in *MrMergeIn) (*MrMergeOut, error) {
	p, err := pid(in.Project)
	if err != nil {
		return nil, err
	}
	if in.Iid <= 0 {
		return nil, fmt.Errorf("MR 编号要填（!42 的 42）")
	}
	body := map[string]any{"squash": in.Squash}
	if in.WhenPipelineSucceeds {
		body["merge_when_pipeline_succeeds"] = true
	}
	raw, _, err := glCall(ctx, http.MethodPut,
		fmt.Sprintf("/projects/%s/merge_requests/%d/merge", p, in.Iid), body)
	if err != nil {
		if strings.Contains(err.Error(), "405") {
			return nil, fmt.Errorf("GitLab 拒绝合并（405）——CI 未过、有冲突或分支被保护；MR 页面上看具体卡在哪")
		}
		return nil, err
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return &MrMergeOut{State: str(m, "state"), MergeCommitID: str(m, "merge_commit_sha")}, nil
}

func opMrNote(ctx plugin.Ctx, in *MrNoteIn) (*MrNoteOut, error) {
	p, err := pid(in.Project)
	if err != nil {
		return nil, err
	}
	if in.Iid <= 0 || strings.TrimSpace(in.Body) == "" {
		return nil, fmt.Errorf("MR 编号与评论内容都要填")
	}
	raw, _, err := glCall(ctx, http.MethodPost,
		fmt.Sprintf("/projects/%s/merge_requests/%d/notes", p, in.Iid), map[string]any{"body": in.Body})
	if err != nil {
		return nil, err
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return &MrNoteOut{NoteID: num(m, "id")}, nil
}

// —— Issue ——

func opIssuesList(ctx plugin.Ctx, in *IssuesListIn) (*IssuesListOut, error) {
	p, err := pid(in.Project)
	if err != nil {
		return nil, err
	}
	params := map[string]any{"per_page": 50, "page": pageOf(in.Page), "state": defStr(in.State, "opened")}
	if l := strings.TrimSpace(in.Labels); l != "" {
		params["labels"] = l
	}
	raw, hdr, err := glCall(ctx, http.MethodGet, "/projects/"+p+"/issues", params)
	if err != nil {
		return nil, err
	}
	out := &IssuesListOut{}
	for _, m := range digList(raw) {
		var labels []string
		if arr, ok := m["labels"].([]any); ok {
			for _, l := range arr {
				labels = append(labels, fmt.Sprintf("%v", l))
			}
		}
		out.Issues = append(out.Issues, schema.Issue{
			IID: num(m, "iid"), Title: str(m, "title"), State: str(m, "state"), Labels: labels,
			Author: nested(m, "author", "username"), WebURL: str(m, "web_url"), CreatedAt: str(m, "created_at"),
		})
	}
	out.Count = len(out.Issues)
	out.Total, out.HasMore = pageInfo(hdr)
	return out, nil
}

func opIssueCreate(ctx plugin.Ctx, in *IssueCreateIn) (*IssueCreateOut, error) {
	p, err := pid(in.Project)
	if err != nil {
		return nil, err
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, fmt.Errorf("标题是空的")
	}
	body := map[string]any{"title": title, "description": in.Description}
	if l := strings.TrimSpace(in.Labels); l != "" {
		body["labels"] = l
	}
	if a := strings.TrimSpace(in.Assignee); a != "" {
		// 指派要 user id，先按用户名查。
		uraw, _, uerr := glCall(ctx, http.MethodGet, "/users", map[string]any{"username": a})
		if uerr == nil {
			if us := digList(uraw); len(us) > 0 {
				body["assignee_ids"] = []int{num(us[0], "id")}
			}
		}
	}
	raw, _, err := glCall(ctx, http.MethodPost, "/projects/"+p+"/issues", body)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return &IssueCreateOut{Iid: num(m, "iid"), WebURL: str(m, "web_url")}, nil
}

func opIssueNote(ctx plugin.Ctx, in *IssueNoteIn) (*IssueNoteOut, error) {
	p, err := pid(in.Project)
	if err != nil {
		return nil, err
	}
	if in.Iid <= 0 || strings.TrimSpace(in.Body) == "" {
		return nil, fmt.Errorf("Issue 编号与评论内容都要填")
	}
	raw, _, err := glCall(ctx, http.MethodPost,
		fmt.Sprintf("/projects/%s/issues/%d/notes", p, in.Iid), map[string]any{"body": in.Body})
	if err != nil {
		return nil, err
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return &IssueNoteOut{NoteID: num(m, "id")}, nil
}

// —— CI/CD ——

func opPipelinesList(ctx plugin.Ctx, in *PipelinesListIn) (*PipelinesListOut, error) {
	p, err := pid(in.Project)
	if err != nil {
		return nil, err
	}
	params := map[string]any{"per_page": 50, "page": pageOf(in.Page)}
	if s := strings.TrimSpace(in.Status); s != "" {
		params["status"] = s
	}
	if r := strings.TrimSpace(in.Ref); r != "" {
		params["ref"] = r
	}
	raw, hdr, err := glCall(ctx, http.MethodGet, "/projects/"+p+"/pipelines", params)
	if err != nil {
		return nil, err
	}
	out := &PipelinesListOut{}
	for _, m := range digList(raw) {
		out.Pipelines = append(out.Pipelines, schema.Pipeline{
			ID: num(m, "id"), Status: str(m, "status"), Ref: str(m, "ref"),
			SHA: str(m, "sha"), WebURL: str(m, "web_url"), CreatedAt: str(m, "created_at"),
		})
	}
	out.Count = len(out.Pipelines)
	out.Total, out.HasMore = pageInfo(hdr)
	return out, nil
}

func opPipelineTrigger(ctx plugin.Ctx, in *PipelineTriggerIn) (*PipelineTriggerOut, error) {
	p, err := pid(in.Project)
	if err != nil {
		return nil, err
	}
	ref := strings.TrimSpace(in.Ref)
	if ref == "" {
		return nil, fmt.Errorf("分支/tag 是空的")
	}
	body := map[string]any{"ref": ref}
	if len(in.Variables) > 0 {
		// pipeline 接口的变量形态是 [{key,value}] 数组。
		var vars []map[string]any
		for k, v := range in.Variables {
			vars = append(vars, map[string]any{"key": k, "value": fmt.Sprintf("%v", v)})
		}
		body["variables"] = vars
	}
	raw, _, err := glCall(ctx, http.MethodPost, "/projects/"+p+"/pipeline", body)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return &PipelineTriggerOut{PipelineID: num(m, "id"), WebURL: str(m, "web_url"), Status: str(m, "status")}, nil
}

func opPipelineJobs(ctx plugin.Ctx, in *PipelineJobsIn) (*PipelineJobsOut, error) {
	p, err := pid(in.Project)
	if err != nil {
		return nil, err
	}
	if in.PipelineID <= 0 {
		return nil, fmt.Errorf("流水线 ID 要填（流水线列表的产出）")
	}
	raw, _, err := glCall(ctx, http.MethodGet,
		fmt.Sprintf("/projects/%s/pipelines/%d/jobs", p, in.PipelineID), map[string]any{"per_page": 100})
	if err != nil {
		return nil, err
	}
	out := &PipelineJobsOut{}
	for _, m := range digList(raw) {
		dur, _ := m["duration"].(float64)
		out.Jobs = append(out.Jobs, schema.Job{
			ID: num(m, "id"), Name: str(m, "name"), Stage: str(m, "stage"),
			Status: str(m, "status"), Duration: dur, WebURL: str(m, "web_url"),
		})
	}
	out.Count = len(out.Jobs)
	return out, nil
}

func opJobLog(ctx plugin.Ctx, in *JobLogIn) (*JobLogOut, error) {
	p, err := pid(in.Project)
	if err != nil {
		return nil, err
	}
	if in.JobID <= 0 {
		return nil, fmt.Errorf("Job ID 要填（Job 列表的产出）")
	}
	raw, _, err := glCall(ctx, http.MethodGet,
		fmt.Sprintf("/projects/%s/jobs/%d/trace", p, in.JobID), nil)
	if err != nil {
		return nil, err
	}
	tail := in.TailLines
	if tail <= 0 {
		tail = 200
	}
	if tail > 2000 {
		tail = 2000
	}
	lines := strings.Split(string(raw), "\n")
	if len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}
	log := strings.Join(lines, "\n")
	return &JobLogOut{Log: log, Lines: len(lines)}, nil
}

// —— 保底 / 体检 ——

func opCall(ctx plugin.Ctx, in *CallIn) (*CallOut, error) {
	path := strings.TrimSpace(in.Path)
	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("path 要以 / 开头（不带 /api/v4 前缀），如 /projects/42/releases")
	}
	if strings.HasPrefix(path, "/api/") {
		return nil, fmt.Errorf("path 不要带 /api/v4 前缀（插件会加），直接写 %q 里 v4 之后的部分", path)
	}
	method := strings.ToUpper(defStr(in.Method, "GET"))
	raw, _, err := glCall(ctx, method, path, in.Body)
	if err != nil {
		return nil, err
	}
	var v any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &v)
	}
	return &CallOut{Data: v}, nil
}

func opHealthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	raw, _, err := glCall(ctx, http.MethodGet, "/user", nil)
	if err != nil {
		return &HealthCheckOut{OK: false, Message: err.Error()}, nil
	}
	var u map[string]any
	_ = json.Unmarshal(raw, &u)
	name := str(u, "username")
	return &HealthCheckOut{OK: true, Username: name, Message: "token 可用：@" + name}, nil
}

// —— 闭环：改 Issue / 改评论 ——

func opIssueUpdate(ctx plugin.Ctx, in *IssueUpdateIn) (*IssueUpdateOut, error) {
	p, err := pid(in.Project)
	if err != nil {
		return nil, err
	}
	if in.Iid <= 0 {
		return nil, fmt.Errorf("没给 Issue 编号")
	}
	body := map[string]any{}
	// 只把**真填了的**字段发出去：GitLab 收到 title:"" 会把标题清空，
	// 而「留空 = 不改」才是这个操作的语义。
	if v := strings.TrimSpace(in.StateEvent); v != "" {
		body["state_event"] = v
	}
	if v := strings.TrimSpace(in.Title); v != "" {
		body["title"] = v
	}
	if v := strings.TrimSpace(in.Description); v != "" {
		body["description"] = v
	}
	if len(in.AddLabels) > 0 {
		body["add_labels"] = strings.Join(in.AddLabels, ",")
	}
	if len(in.RemoveLabels) > 0 {
		body["remove_labels"] = strings.Join(in.RemoveLabels, ",")
	}
	if v := strings.TrimSpace(in.Assignee); v != "" {
		id, err := userID(ctx, v)
		if err != nil {
			return nil, err
		}
		body["assignee_ids"] = []int{id}
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("一个要改的字段都没给")
	}
	raw, _, err := glCall(ctx, http.MethodPut, fmt.Sprintf("/projects/%s/issues/%d", p, in.Iid), body)
	if err != nil {
		return nil, err
	}
	m := digObj(raw)
	out := &IssueUpdateOut{State: str(m, "state"), URL: str(m, "web_url")}
	out.Labels = digStrings(m["labels"])
	return out, nil
}

func opNoteUpdate(ctx plugin.Ctx, in *NoteUpdateIn) (*NoteUpdateOut, error) {
	p, err := pid(in.Project)
	if err != nil {
		return nil, err
	}
	if in.Iid <= 0 || in.NoteID <= 0 || strings.TrimSpace(in.Body) == "" {
		return nil, fmt.Errorf("编号、评论 ID、新内容都要填")
	}
	kind := "issues"
	if strings.TrimSpace(in.Target) == "merge_request" {
		kind = "merge_requests"
	}
	raw, _, err := glCall(ctx, http.MethodPut,
		fmt.Sprintf("/projects/%s/%s/%d/notes/%d", p, kind, in.Iid, in.NoteID),
		map[string]any{"body": in.Body})
	if err != nil {
		return nil, err
	}
	return &NoteUpdateOut{NoteID: num(digObj(raw), "id")}, nil
}

// userID 用户名 → 数字 id（指派接口只收 id）。
func userID(ctx plugin.Ctx, username string) (int, error) {
	raw, _, err := glCall(ctx, http.MethodGet, "/users", map[string]any{"username": username})
	if err != nil {
		return 0, err
	}
	list := digList(raw)
	if len(list) == 0 {
		return 0, fmt.Errorf("找不到用户「%s」", username)
	}
	return num(list[0], "id"), nil
}

// —— MR review ——

const defaultDiffLimit = 20000

func opMrChanges(ctx plugin.Ctx, in *MrChangesIn) (*MrChangesOut, error) {
	p, err := pid(in.Project)
	if err != nil {
		return nil, err
	}
	if in.Iid <= 0 {
		return nil, fmt.Errorf("没给 MR 编号")
	}
	raw, _, err := glCall(ctx, http.MethodGet,
		fmt.Sprintf("/projects/%s/merge_requests/%d/changes", p, in.Iid), nil)
	if err != nil {
		return nil, err
	}
	m := digObj(raw)
	limit := in.MaxDiffChars
	if limit <= 0 {
		limit = defaultDiffLimit
	}
	out := &MrChangesOut{
		Title: str(m, "title"), SourceBranch: str(m, "source_branch"), TargetBranch: str(m, "target_branch"),
	}
	arr, _ := m["changes"].([]any)
	for _, it := range arr {
		c, _ := it.(map[string]any)
		if c == nil {
			continue
		}
		d := str(c, "diff")
		// 单文件截断而不是整体截断：一个几万行的迁移 diff 会把下游的上下文占满，
		// 但**其余文件的改动不该跟着一起没**——那会让 review 漏掉真正要看的地方。
		if len(d) > limit {
			d = d[:limit] + "\n…（此文件 diff 已截断）"
			out.Truncated = true
		}
		nf, _ := c["new_file"].(bool)
		df, _ := c["deleted_file"].(bool)
		out.Changes = append(out.Changes, schema.MrChange{
			Path: str(c, "new_path"), OldPath: str(c, "old_path"),
			New: nf, Deleted: df, Diff: d,
		})
	}
	out.Count = len(out.Changes)
	return out, nil
}

func opMrApprove(ctx plugin.Ctx, in *MrApproveIn) (*MrApproveOut, error) {
	p, err := pid(in.Project)
	if err != nil {
		return nil, err
	}
	if in.Iid <= 0 {
		return nil, fmt.Errorf("没给 MR 编号")
	}
	raw, _, err := glCall(ctx, http.MethodPost,
		fmt.Sprintf("/projects/%s/merge_requests/%d/approve", p, in.Iid), nil)
	if err != nil {
		return nil, err
	}
	m := digObj(raw)
	out := &MrApproveOut{ApprovalsLeft: num(m, "approvals_left")}
	if arr, ok := m["approved_by"].([]any); ok {
		for _, it := range arr {
			e, _ := it.(map[string]any)
			u, _ := e["user"].(map[string]any)
			if n := str(u, "username"); n != "" {
				out.ApprovedBy = append(out.ApprovedBy, n)
			}
		}
	}
	return out, nil
}

func opMrUpdate(ctx plugin.Ctx, in *MrUpdateIn) (*MrUpdateOut, error) {
	p, err := pid(in.Project)
	if err != nil {
		return nil, err
	}
	if in.Iid <= 0 {
		return nil, fmt.Errorf("没给 MR 编号")
	}
	body := map[string]any{}
	// 与 issue_update 同一条约定：只发真填了的字段，留空 = 不改。
	if v := strings.TrimSpace(in.StateEvent); v != "" {
		body["state_event"] = v
	}
	if v := strings.TrimSpace(in.Title); v != "" {
		body["title"] = v
	}
	if v := strings.TrimSpace(in.Description); v != "" {
		body["description"] = v
	}
	if len(in.AddLabels) > 0 {
		body["add_labels"] = strings.Join(in.AddLabels, ",")
	}
	if len(in.RemoveLabels) > 0 {
		body["remove_labels"] = strings.Join(in.RemoveLabels, ",")
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("一个要改的字段都没给")
	}
	raw, _, err := glCall(ctx, http.MethodPut,
		fmt.Sprintf("/projects/%s/merge_requests/%d", p, in.Iid), body)
	if err != nil {
		return nil, err
	}
	m := digObj(raw)
	return &MrUpdateOut{State: str(m, "state"), Labels: digStrings(m["labels"]), URL: str(m, "web_url")}, nil
}

// —— 搜索 / 行级评论 ——

func opSearch(ctx plugin.Ctx, in *SearchIn) (*SearchOut, error) {
	q := strings.TrimSpace(in.Query)
	if q == "" {
		return nil, fmt.Errorf("没给搜索词")
	}
	scope := defStr(in.Scope, "blobs")
	params := map[string]any{"scope": scope, "search": q, "per_page": 50, "page": pageOf(in.Page)}
	path := "/search"
	if p := strings.TrimSpace(in.Project); p != "" {
		id, err := pid(p)
		if err != nil {
			return nil, err
		}
		path = "/projects/" + id + "/search"
	}
	raw, hdr, err := glCall(ctx, http.MethodGet, path, params)
	if err != nil {
		return nil, err
	}
	out := &SearchOut{}
	for _, m := range digList(raw) {
		out.Results = append(out.Results, searchHit(scope, m))
	}
	out.Count = len(out.Results)
	out.Total, out.HasMore = pageInfo(hdr)
	return out, nil
}

// searchHit 各 scope 的命中形状差得很远（代码给 path/startline/data，Issue 给 iid/title），
// 归一成一个结构：画布上不该为「搜的是代码还是 Issue」准备两套下钻路径。
func searchHit(scope string, m map[string]any) schema.SearchHit {
	h := schema.SearchHit{URL: str(m, "web_url")}
	switch scope {
	case "blobs":
		h.Path, h.Ref = str(m, "path"), str(m, "ref")
		h.Line = num(m, "startline")
		h.Title = str(m, "basename")
		h.Snippet = clip(str(m, "data"), 2000)
	case "commits":
		h.Title = str(m, "title")
		h.Snippet = clip(str(m, "message"), 2000)
		h.Ref = str(m, "short_id")
	default: // issues / merge_requests
		h.Iid, h.Title = num(m, "iid"), str(m, "title")
		h.Snippet = clip(str(m, "description"), 2000)
	}
	if p := num(m, "project_id"); p > 0 && h.Project == "" {
		h.Project = fmt.Sprintf("%d", p) // 全局搜索只回 project_id，路径要另查——先给 id 够用
	}
	return h
}

func opMrDiscussion(ctx plugin.Ctx, in *MrDiscussionIn) (*MrDiscussionOut, error) {
	p, err := pid(in.Project)
	if err != nil {
		return nil, err
	}
	if in.Iid <= 0 || strings.TrimSpace(in.Path) == "" || strings.TrimSpace(in.Body) == "" {
		return nil, fmt.Errorf("MR 编号、文件路径、评论内容都要填")
	}
	if in.Line <= 0 && in.OldLine <= 0 {
		return nil, fmt.Errorf("行号与原文件行号至少给一个（评论新增/上下文行用「行号」，评论被删除的行用「原文件行号」）")
	}
	// 行级评论要三个 sha 定位 diff。它们在 MR 自己身上（diff_refs），
	// **插件自己去取**：让调用方在画布上填 base/start/head 三个 sha 是不可能用对的。
	raw, _, err := glCall(ctx, http.MethodGet, fmt.Sprintf("/projects/%s/merge_requests/%d", p, in.Iid), nil)
	if err != nil {
		return nil, err
	}
	refs, _ := digObj(raw)["diff_refs"].(map[string]any)
	if refs == nil {
		return nil, fmt.Errorf("取不到 MR 的 diff 基准（diff_refs）——MR 可能还在生成 diff，稍后再试")
	}
	pos := map[string]any{
		"position_type": "text",
		"base_sha":      str(refs, "base_sha"),
		"start_sha":     str(refs, "start_sha"),
		"head_sha":      str(refs, "head_sha"),
		"new_path":      in.Path,
		"old_path":      in.Path,
	}
	if in.Line > 0 {
		pos["new_line"] = in.Line
	}
	if in.OldLine > 0 {
		pos["old_line"] = in.OldLine
	}
	raw, _, err = glCall(ctx, http.MethodPost,
		fmt.Sprintf("/projects/%s/merge_requests/%d/discussions", p, in.Iid),
		map[string]any{"body": in.Body, "position": pos})
	if err != nil {
		return nil, err
	}
	d := digObj(raw)
	out := &MrDiscussionOut{DiscussionID: str(d, "id")}
	if arr, ok := d["notes"].([]any); ok && len(arr) > 0 {
		if n, ok := arr[0].(map[string]any); ok {
			out.NoteID = num(n, "id")
		}
	}
	return out, nil
}
