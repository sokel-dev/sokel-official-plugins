package main

// httptest fakes GitLab to exercise the critical paths. Real self-hosted-instance integration testing goes through operation:test.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

type fakeCtx struct {
	context.Context
	cred map[string]string
}

func newFake(cred map[string]string) *fakeCtx {
	return &fakeCtx{Context: context.Background(), cred: cred}
}

func (f *fakeCtx) Credential() map[string]string { return f.cred }
func (f *fakeCtx) Upload(string, string, []byte) (*plugin.File, error) {
	return &plugin.File{ID: "f"}, nil
}
func (f *fakeCtx) UploadReader(string, string, io.Reader) (*plugin.File, error) {
	return &plugin.File{ID: "f"}, nil
}
func (f *fakeCtx) Fetch(*plugin.File) ([]byte, error) { return nil, nil }

// fakeSourceCtx is an event-source mock: it records Trigger calls.
type fakeSourceCtx struct {
	*fakeCtx
	onTrigger func(event, id string)
	onPayload func(event string, payload any)
}

func (f *fakeSourceCtx) Trigger(event, eventID string, payload any) error {
	if f.onTrigger != nil {
		f.onTrigger(event, eventID)
	}
	if f.onPayload != nil {
		f.onPayload(event, payload)
	}
	return nil
}
func (f *fakeSourceCtx) UpdateCredential(map[string]string) error { return nil }
func (f *fakeSourceCtx) ReportStatus(string, string)              {}

func credFor(srv *httptest.Server) map[string]string {
	return map[string]string{"base_url": srv.URL, "token": "glpat-test"}
}

// The project path must be fully URL-encoded into the path segment; the PRIVATE-TOKEN header must be present.
func TestProjectPathEncoding(t *testing.T) {
	var gotPath, gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotToken = r.URL.EscapedPath(), r.Header.Get("PRIVATE-TOKEN")
		_, _ = io.WriteString(w, `[]`)
	}))
	defer srv.Close()
	_, err := opBranchesList(newFake(credFor(srv)), &BranchesListIn{Project: "backend/server"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotPath, "/projects/backend%2Fserver/") {
		t.Errorf("项目路径要编码: %s", gotPath)
	}
	if gotToken != "glpat-test" {
		t.Errorf("token 头没带: %q", gotToken)
	}
}

// Reading a file: base64-decode it; the file path is fully encoded (including slashes).
func TestFileGetDecodesBase64(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.EscapedPath(), "config%2Fapp.yaml") {
			t.Errorf("文件路径要整体编码: %s", r.URL.EscapedPath())
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": base64.StdEncoding.EncodeToString([]byte("key: value")), "encoding": "base64",
			"last_commit_id": "abc"})
	}))
	defer srv.Close()
	out, err := opFileGet(newFake(credFor(srv)), &FileGetIn{Project: "42", Path: "config/app.yaml", Ref: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Content != "key: value" || out.LastCommitID != "abc" {
		t.Errorf("out=%+v", out)
	}
}

// Writing a file: action=create if it doesn't exist, update if it does. Goes through the commits endpoint.
func TestFileWriteCreateVsUpdate(t *testing.T) {
	exists := false
	var gotAction string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/repository/files/") {
			if !exists {
				w.WriteHeader(404)
				_, _ = io.WriteString(w, `{"message":"404 File Not Found"}`)
				return
			}
			_, _ = io.WriteString(w, `{"content":"","encoding":"base64"}`)
			return
		}
		var body struct {
			Actions []struct {
				Action string `json:"action"`
			} `json:"actions"`
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		gotAction = body.Actions[0].Action
		_, _ = io.WriteString(w, `{"id":"c1","web_url":"http://x/c1"}`)
	}))
	defer srv.Close()
	ctx := newFake(credFor(srv))

	out, err := opFileWrite(ctx, &FileWriteIn{Project: "42", Path: "new.txt", Content: "x", Branch: "main", Message: "add"})
	if err != nil {
		t.Fatal(err)
	}
	if gotAction != "create" || out.CommitID != "c1" {
		t.Errorf("不存在的文件要 create: action=%s out=%+v", gotAction, out)
	}
	exists = true
	if _, err := opFileWrite(ctx, &FileWriteIn{Project: "42", Path: "new.txt", Content: "y", Branch: "main", Message: "upd"}); err != nil {
		t.Fatal(err)
	}
	if gotAction != "update" {
		t.Errorf("已存在的文件要 update: %s", gotAction)
	}
}

// Triggering a pipeline: variables are converted to the [{key,value}] array shape.
func TestPipelineTriggerVariables(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		_, _ = io.WriteString(w, `{"id":7,"status":"pending","web_url":"http://x/p/7"}`)
	}))
	defer srv.Close()
	out, err := opPipelineTrigger(newFake(credFor(srv)), &PipelineTriggerIn{
		Project: "42", Ref: "main", Variables: map[string]any{"ENV": "staging"}})
	if err != nil {
		t.Fatal(err)
	}
	if out.PipelineID != 7 {
		t.Errorf("out=%+v", out)
	}
	vars := body["variables"].([]any)[0].(map[string]any)
	if vars["key"] != "ENV" || vars["value"] != "staging" {
		t.Errorf("变量要转 [{key,value}] 形态: %v", body["variables"])
	}
}

// Job log tail truncation.
func TestJobLogTail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "l1\nl2\nl3\nl4\nl5")
	}))
	defer srv.Close()
	out, err := opJobLog(newFake(credFor(srv)), &JobLogIn{Project: "42", JobID: 1, TailLines: 2})
	if err != nil {
		t.Fatal(err)
	}
	if out.Log != "l4\nl5" || out.Lines != 2 {
		t.Errorf("out=%+v", out)
	}
}

// A 404 error must mention "404 can also mean no permission" -- this is GitLab's anti-probing
// behavior, and someone unaware of it would think the path was wrong.
func TestNotFoundMentionsPermission(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		_, _ = io.WriteString(w, `{"message":"404 Project Not Found"}`)
	}))
	defer srv.Close()
	_, err := opMrList(newFake(credFor(srv)), &MrListIn{Project: "secret/repo"})
	if err == nil || !strings.Contains(err.Error(), "没权限") {
		t.Errorf("404 要提示权限的可能, got %v", err)
	}
}

// Input hygiene for call: a /api/v4 prefix must be rejected (it would produce /api/v4/api/v4).
func TestCallPathHygiene(t *testing.T) {
	ctx := newFake(map[string]string{"base_url": "http://x", "token": "t"})
	if _, err := opCall(ctx, &CallIn{Path: "/api/v4/projects"}); err == nil || !strings.Contains(err.Error(), "前缀") {
		t.Errorf("要拦 /api/v4 前缀, got %v", err)
	}
	if _, err := opCall(ctx, &CallIn{Path: "projects"}); err == nil || !strings.Contains(err.Error(), "开头") {
		t.Errorf("要拦不带 / 的路径, got %v", err)
	}
}

// Missing token points the way.
func TestMissingToken(t *testing.T) {
	_, err := opProjectsList(newFake(map[string]string{"base_url": "http://x"}), &ProjectsListIn{})
	if err == nil || !strings.Contains(err.Error(), "Access Tokens") {
		t.Errorf("缺 token 要指路, got %v", err)
	}
}

// Event mapping: Events API action/target -> platform event; GitLab's "merge" shows up as
// accepted, not merged (that's GitLab's own naming -- looking for merged by intuition would get zero events).
func TestTriggerEventMapping(t *testing.T) {
	var fired []string
	src := &fakeSourceCtx{fakeCtx: newFake(nil), onTrigger: func(event, id string) {
		fired = append(fired, event+"/"+id)
	}}
	evs := []glEvent{
		{ID: 1, ActionName: "pushed to", PushData: &struct {
			CommitCount int    `json:"commit_count"`
			Ref         string `json:"ref"`
			CommitTo    string `json:"commit_to"`
			CommitTitle string `json:"commit_title"`
		}{CommitCount: 2, Ref: "main", CommitTo: "abc"}},
		{ID: 2, ActionName: "opened", TargetType: "MergeRequest", TargetIID: 7},
		{ID: 3, ActionName: "accepted", TargetType: "MergeRequest", TargetIID: 7},
		{ID: 4, ActionName: "opened", TargetType: "Issue", TargetIID: 9},
		{ID: 5, ActionName: "commented on", TargetType: "Note"}, // unrecognized type is skipped
	}
	for _, ev := range evs {
		triggerEvent(src, Cred{}, "g/p", ev)
	}
	want := []string{"commit_pushed/1", "mr_opened/2", "mr_merged/3", "issue_opened/4"}
	if len(fired) != len(want) {
		t.Fatalf("fired=%v", fired)
	}
	for i := range want {
		if fired[i] != want[i] {
			t.Errorf("第 %d 个 = %s, want %s", i, fired[i], want[i])
		}
	}
}

// Webhook parsing: three kinds of GitLab hook map to existing events; signature verification
// compares X-Gitlab-Token; unrecognized event types return 200 (so GitLab doesn't keep retrying).
func TestWebhookParsing(t *testing.T) {
	var fired []string
	ctx := &fakeSourceCtx{fakeCtx: newFake(map[string]string{"webhook_secret": "s3cr3t"}),
		onTrigger: func(event, id string) { fired = append(fired, event+"/"+id) }}

	// signature verification fails
	resp := handleWebhook(ctx, &sokel.WebhookRequest{
		Headers: map[string]string{"X-Gitlab-Token": "wrong"}, Body: []byte(`{}`)})
	if resp.Status != 401 {
		t.Fatalf("错 token want 401 got %d", resp.Status)
	}

	// push
	push := `{"after":"abc123","ref":"refs/heads/main","total_commits_count":2,
	  "user_username":"dev1","project":{"path_with_namespace":"g/p"},
	  "commits":[{"title":"c1"},{"title":"最新提交"}]}`
	resp = handleWebhook(ctx, &sokel.WebhookRequest{
		Headers: map[string]string{"X-Gitlab-Token": "s3cr3t", "X-Gitlab-Event": "Push Hook"},
		Body:    []byte(push)})
	if resp.Status != 200 || len(fired) != 1 || fired[0] != "commit_pushed/wh:abc123" {
		t.Fatalf("push: status=%d fired=%v", resp.Status, fired)
	}

	// MR merge
	mr := `{"user":{"username":"dev2"},"project":{"path_with_namespace":"g/p"},
	  "object_attributes":{"iid":7,"action":"merge","title":"修个bug","updated_at":"t1"}}`
	resp = handleWebhook(ctx, &sokel.WebhookRequest{
		Headers: map[string]string{"X-Gitlab-Token": "s3cr3t", "X-Gitlab-Event": "Merge Request Hook"},
		Body:    []byte(mr)})
	if len(fired) != 2 || fired[1] != "mr_merged/wh:mr:7:merge:t1" {
		t.Fatalf("mr: fired=%v", fired)
	}

	// pipeline failed
	pipe := `{"project":{"path_with_namespace":"g/p","web_url":"http://x"},
	  "object_attributes":{"id":55,"status":"failed","ref":"main","sha":"s1"}}`
	_ = handleWebhook(ctx, &sokel.WebhookRequest{
		Headers: map[string]string{"X-Gitlab-Token": "s3cr3t", "X-Gitlab-Event": "Pipeline Hook"},
		Body:    []byte(pipe)})
	if len(fired) != 3 || fired[2] != "pipeline_failed/wh:pipe:55" {
		t.Fatalf("pipeline: fired=%v", fired)
	}

	// unrecognized type: 200 + no trigger
	resp = handleWebhook(ctx, &sokel.WebhookRequest{
		Headers: map[string]string{"X-Gitlab-Token": "s3cr3t", "X-Gitlab-Event": "Wiki Page Hook"},
		Body:    []byte(`{}`)})
	if resp.Status != 200 || len(fired) != 3 {
		t.Fatalf("未知类型要 200 且不触发: status=%d fired=%v", resp.Status, fired)
	}
}

// Issue events must carry the body and labels.
//
// The reason is concrete: "use the Issue content to dispatch work" -- the body is that
// content, while the title is often just a one-liner. Labels matter just as much; they're the
// gate for "who can trigger this" (only process Issues with a given label) -- without them,
// the only option is "whoever created it, it runs".
func TestIssueWebhookCarriesBodyAndLabels(t *testing.T) {
	var got *IssueOpenedEvent
	ctx := &fakeSourceCtx{fakeCtx: newFake(map[string]string{}),
		onPayload: func(event string, p any) {
			if ev, ok := p.(*IssueOpenedEvent); ok && event == "issue_opened" {
				got = ev
			}
		}}

	body := `{"object_attributes":{"iid":42,"action":"open","title":"登录超时",
	  "description":"复现步骤：\n1. 点登录\n2. 等 30 秒","url":"https://git/x/y/-/issues/42"},
	  "labels":[{"title":"bug"},{"title":"claude"}],
	  "user":{"username":"alice"},"project":{"path_with_namespace":"x/y"}}`
	resp := handleWebhook(ctx, &sokel.WebhookRequest{
		Headers: map[string]string{"X-Gitlab-Event": "Issue Hook"}, Body: []byte(body)})
	if resp.Status != 200 || got == nil {
		t.Fatalf("issue hook 该触发事件: status=%d payload=%+v", resp.Status, got)
	}
	if !strings.Contains(got.Description, "复现步骤") {
		t.Errorf("正文该带出来，got %q", got.Description)
	}
	if len(got.Labels) != 2 || got.Labels[0] != "bug" || got.Labels[1] != "claude" {
		t.Errorf("标签该解成名字数组，got %+v", got.Labels)
	}
	if got.URL == "" || got.Iid != 42 || got.Author != "alice" {
		t.Errorf("链接/编号/作者都该在，got %+v", got)
	}
}

// Labels appear in two places in the payload, in different shapes (array of objects / array
// of strings), and may be absent from both.
func TestHookLabelsShapes(t *testing.T) {
	objs := map[string]any{"labels": []any{map[string]any{"title": "bug"}}}
	if got := hookLabels(objs, map[string]any{}); len(got) != 1 || got[0] != "bug" {
		t.Errorf("顶层对象数组: got %+v", got)
	}
	strs := map[string]any{}
	attrs := map[string]any{"labels": []any{"perf"}}
	if got := hookLabels(strs, attrs); len(got) != 1 || got[0] != "perf" {
		t.Errorf("attrs 里的字符串数组: got %+v", got)
	}
	if got := hookLabels(map[string]any{}, map[string]any{}); got != nil {
		t.Errorf("都没有该回 nil（没标签是常态，不是错误），got %+v", got)
	}
}

// "Create the Issue first, then label it" is the most natural human usage, but adding a label
// is an update on GitLab's side, not an open, so issue_opened will never fire for it again.
// This event fills that gap.
func TestIssueLabeledWebhook(t *testing.T) {
	var got *IssueLabeledEvent
	var fired []string
	ctx := &fakeSourceCtx{fakeCtx: newFake(map[string]string{}),
		onTrigger: func(ev, id string) { fired = append(fired, ev+"/"+id) },
		onPayload: func(ev string, p any) {
			if e, ok := p.(*IssueLabeledEvent); ok {
				got = e
			}
		}}
	hook := func(body string) *sokel.WebhookResponse {
		r := handleWebhook(ctx, &sokel.WebhookRequest{
			Headers: map[string]string{"X-Gitlab-Event": "Issue Hook"}, Body: []byte(body)})
		return &r
	}

	// add a new label
	hook(`{"object_attributes":{"iid":7,"action":"update","title":"登录超时",
	  "description":"正文","url":"https://git/x/y/-/issues/7","updated_at":"2026-08-24T10:00:00Z"},
	  "changes":{"labels":{"previous":[{"title":"bug"}],"current":[{"title":"bug"},{"title":"claude"}]}},
	  "labels":[{"title":"bug"},{"title":"claude"}],
	  "user":{"username":"alice"},"project":{"path_with_namespace":"x/y"}}`)
	if got == nil {
		t.Fatal("加标签该触发 issue_labeled")
	}
	if len(got.AddedLabels) != 1 || got.AddedLabels[0] != "claude" {
		t.Errorf("只该报**新增**的那个，got %+v", got.AddedLabels)
	}
	if len(got.Labels) != 2 {
		t.Errorf("当前全部标签也要给（判条件常用），got %+v", got.Labels)
	}
	if got.Description != "正文" || got.Iid != 7 || got.Author != "alice" {
		t.Errorf("正文/编号/操作人都该在: %+v", got)
	}

	// Removing a label is also an update -- it shouldn't be treated as a dispatch signal
	got, fired = nil, nil
	hook(`{"object_attributes":{"iid":7,"action":"update","updated_at":"t2"},
	  "changes":{"labels":{"previous":[{"title":"bug"},{"title":"claude"}],"current":[{"title":"bug"}]}},
	  "project":{"path_with_namespace":"x/y"}}`)
	if got != nil {
		t.Errorf("摘标签不该触发，却报了 %+v", got.AddedLabels)
	}

	// An update like changing the title (no labels in changes) shouldn't trigger either
	hook(`{"object_attributes":{"iid":7,"action":"update","updated_at":"t3"},
	  "changes":{"title":{"previous":"a","current":"b"}},
	  "project":{"path_with_namespace":"x/y"}}`)
	if got != nil {
		t.Error("非标签变更不该触发")
	}
}

// addedLabels' return value: only genuine additions, with a stable order (event_id is built
// from it, so jitter would break dedup).
func TestAddedLabelsStable(t *testing.T) {
	body := map[string]any{"changes": map[string]any{"labels": map[string]any{
		"previous": []any{map[string]any{"title": "a"}},
		"current":  []any{map[string]any{"title": "a"}, map[string]any{"title": "z"}, map[string]any{"title": "m"}},
	}}}
	for i := 0; i < 8; i++ { // map iteration order differs each time; sorting must pin it down
		got := addedLabels(body)
		if len(got) != 2 || got[0] != "m" || got[1] != "z" {
			t.Fatalf("第 %d 次结果不稳定: %+v", i, got)
		}
	}
	if addedLabels(map[string]any{}) != nil {
		t.Error("没有 changes.labels 该回 nil")
	}
}

// Comment-triggered dispatch. This is the most natural shape for "dispatching work", but it
// naturally forms a loop -- a bot replying to an Issue is also a comment, and without a guard
// that's an infinite loop. So author and comment must always be fully populated, so the
// workflow can filter itself out.
func TestIssueCommentedWebhook(t *testing.T) {
	var got *IssueCommentedEvent
	ctx := &fakeSourceCtx{fakeCtx: newFake(map[string]string{}),
		onPayload: func(ev string, p any) {
			if e, ok := p.(*IssueCommentedEvent); ok {
				got = e
			}
		}}
	hook := func(body string) int {
		return handleWebhook(ctx, &sokel.WebhookRequest{
			Headers: map[string]string{"X-Gitlab-Event": "Note Hook"}, Body: []byte(body)}).Status
	}

	if s := hook(`{"object_attributes":{"id":991,"noteable_type":"Issue","note":"/cc 把测试补上",
	  "url":"https://git/x/y/-/issues/9#note_991"},
	  "issue":{"iid":9,"title":"缺测试"},
	  "user":{"username":"alice"},"project":{"path_with_namespace":"x/y"}}`); s != 200 {
		t.Fatalf("status=%d", s)
	}
	if got == nil || got.Comment != "/cc 把测试补上" || got.Iid != 9 || got.Author != "alice" {
		t.Fatalf("评论内容/编号/评论人都该在: %+v", got)
	}

	// A comment on an MR goes through the same Hook -- it shouldn't dispatch Issue work
	got = nil
	hook(`{"object_attributes":{"id":992,"noteable_type":"MergeRequest","note":"看着不错"},
	  "merge_request":{"iid":3},"user":{"username":"bob"},"project":{"path_with_namespace":"x/y"}}`)
	if got != nil {
		t.Errorf("MR 评论不该触发 Issue 评论事件: %+v", got)
	}
}

// Editing an Issue: only send fields that were actually filled in. GitLab would clear the
// title if it got title:"", and this operation's semantics are "blank means no change" --
// sending an empty value would silently wipe out the user's content.
func TestIssueUpdateOnlySendsFilledFields(t *testing.T) {
	var gotBody map[string]any
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.EscapedPath(), r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = io.WriteString(w, `{"state":"closed","labels":["cc-done"],"web_url":"https://git/x/y/-/issues/5"}`)
	}))
	defer srv.Close()
	ctx := newFake(credFor(srv))

	out, err := opIssueUpdate(ctx, &IssueUpdateIn{
		Project: "g/p", Iid: 5, StateEvent: "close",
		AddLabels: []string{"cc-done"}, RemoveLabels: []string{"claude"},
		Title: "", Description: "  ", // blank/whitespace should not be sent
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPut || gotPath != "/api/v4/projects/g%2Fp/issues/5" {
		t.Errorf("方法/路径不对: %s %s", gotMethod, gotPath)
	}
	for _, k := range []string{"title", "description"} {
		if _, has := gotBody[k]; has {
			t.Errorf("没填的 %s 不该发出去（会清空用户内容）: %+v", k, gotBody)
		}
	}
	if gotBody["state_event"] != "close" {
		t.Errorf("state_event 该发: %+v", gotBody)
	}
	// Labels are a comma-joined string, not an array -- GitLab would 400 on an array
	if gotBody["add_labels"] != "cc-done" || gotBody["remove_labels"] != "claude" {
		t.Errorf("标签该拼成逗号串: %+v", gotBody)
	}
	if out.State != "closed" || len(out.Labels) != 1 {
		t.Errorf("出参该解出状态与标签: %+v", out)
	}

	// Nothing filled in = nothing to change; reject outright rather than send an empty request
	if _, err := opIssueUpdate(ctx, &IssueUpdateIn{Project: "g/p", Iid: 5}); err == nil {
		t.Error("一个字段都没给该报错")
	}
}

// Editing a comment: Issue and MR use different path segments; the wrong one is a 404.
func TestNoteUpdateTargets(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.EscapedPath()
		_, _ = io.WriteString(w, `{"id":991}`)
	}))
	defer srv.Close()
	ctx := newFake(credFor(srv))

	if _, err := opNoteUpdate(ctx, &NoteUpdateIn{Project: "g/p", Iid: 5, NoteID: 991, Body: "更新后"}); err != nil {
		t.Fatal(err)
	}
	if path != "/api/v4/projects/g%2Fp/issues/5/notes/991" {
		t.Errorf("默认该走 issues: %s", path)
	}
	if _, err := opNoteUpdate(ctx, &NoteUpdateIn{Project: "g/p", Target: "merge_request",
		Iid: 3, NoteID: 7, Body: "x"}); err != nil {
		t.Fatal(err)
	}
	if path != "/api/v4/projects/g%2Fp/merge_requests/3/notes/7" {
		t.Errorf("MR 该走 merge_requests: %s", path)
	}
}

// Pagination surfacing for lists.
//
// Only returning "items on this page" is a silent truncation: with exactly 50 items, the
// caller has no way to tell whether that's really all of them or the list got cut off (this is
// exactly the kind of issue recorded in the pagination audit). The answer has always been in
// GitLab's response headers; nobody was reading it.
func TestListSurfacesPagination(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Total", "137")
		w.Header().Set("X-Next-Page", "2")
		_, _ = io.WriteString(w, `[{"iid":1,"title":"a","state":"opened","web_url":"u"}]`)
	}))
	defer srv.Close()
	out, err := opIssuesList(newFake(credFor(srv)), &IssuesListIn{Project: "g/p"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Count != 1 {
		t.Errorf("本页条数 got %d", out.Count)
	}
	if out.Total != 137 || !out.HasMore {
		t.Errorf("总数/还有下一页该从响应头取到: total=%d hasMore=%v", out.Total, out.HasMore)
	}
}

// On very large result sets GitLab omits X-Total (computing the total is too expensive), but
// X-Next-Page is still present. So deciding "is there more to page through" must look at
// has_more; looking at total would make it look like there's nothing at all.
func TestPaginationWithoutTotalHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Next-Page", "3") // only a next page, no total
		_, _ = io.WriteString(w, `[{"iid":9}]`)
	}))
	defer srv.Close()
	out, _ := opIssuesList(newFake(credFor(srv)), &IssuesListIn{Project: "g/p"})
	if out.Total != 0 || !out.HasMore {
		t.Errorf("没有 X-Total 时该 total=0 但 has_more=true: %+v", out)
	}
	// last page: neither header present
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[]`)
	}))
	defer srv2.Close()
	out2, _ := opIssuesList(newFake(credFor(srv2)), &IssuesListIn{Project: "g/p"})
	if out2.HasMore {
		t.Error("最后一页不该说还有下一页")
	}
}

// The three comment-event filters. The fixture's field names and shapes were captured against
// real GitLab /events output -- none of these three gotchas could be figured out just by
// reading code:
//
//	target_iid is the comment's own id (observed: 1970); the Issue number is in
//	note.noteable_iid (observed: 4)
//	Issue and MR comments go through the same event
//	relabeling/reassigning also generates a note (system=true)
func TestNoteEventFilters(t *testing.T) {
	var got []*IssueCommentedEvent
	ctx := &fakeSourceCtx{fakeCtx: newFake(map[string]string{}),
		onPayload: func(ev string, p any) {
			if e, ok := p.(*IssueCommentedEvent); ok {
				got = append(got, e)
			}
		}}
	ev := func(js string) {
		var e glEvent
		if err := json.Unmarshal([]byte(js), &e); err != nil {
			t.Fatal(err)
		}
		triggerEvent(ctx, Cred{}, "g/p", e)
	}

	// A real person comments on Issue #4: target_iid is 1970 (the comment id); the number must come from noteable_iid=4
	ev(`{"id":1,"action_name":"commented on","target_type":"Note","target_iid":1970,
	  "author":{"username":"alice"},
	  "note":{"id":1970,"body":"/cc 修一下","noteable_iid":4,"noteable_type":"Issue","system":false}}`)
	if len(got) != 1 || got[0].Iid != 4 {
		t.Fatalf("Issue 编号该取 noteable_iid=4,got %+v", got)
	}
	if got[0].Comment != "/cc 修一下" || got[0].Author != "alice" {
		t.Errorf("正文/作者不对: %+v", got[0])
	}

	// System note (relabeling) -- shouldn't trigger, otherwise touching a label burns money by dispatching work
	got = nil
	ev(`{"id":2,"action_name":"commented on","target_type":"Note",
	  "note":{"body":"added ~claude label","noteable_iid":4,"noteable_type":"Issue","system":true}}`)
	if len(got) != 0 {
		t.Errorf("系统 note 不该触发: %+v", got)
	}

	// A comment on an MR -- shouldn't be treated as an Issue comment
	got = nil
	ev(`{"id":3,"action_name":"commented on","target_type":"Note",
	  "note":{"body":"看着不错","noteable_iid":7,"noteable_type":"MergeRequest","system":false}}`)
	if len(got) != 0 {
		t.Errorf("MR 评论不该触发 Issue 评论事件: %+v", got)
	}
}

// MR changes: a large diff must be truncated per file, not as a whole -- a migration with
// tens of thousands of lines would fill up the context, and the other files' changes shouldn't
// disappear along with it, which would make review miss the parts that actually matter.
func TestMrChangesTruncatesPerFile(t *testing.T) {
	big := strings.Repeat("x", 500)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"title":"重构","source_branch":"f","target_branch":"main","changes":[
		  {"new_path":"big.sql","old_path":"big.sql","diff":"`+big+`"},
		  {"new_path":"small.go","old_path":"small.go","diff":"+ok","new_file":true}]}`)
	}))
	defer srv.Close()
	out, err := opMrChanges(newFake(credFor(srv)), &MrChangesIn{Project: "g/p", Iid: 3, MaxDiffChars: 100})
	if err != nil {
		t.Fatal(err)
	}
	if out.Count != 2 {
		t.Fatalf("两个文件都该在,got %d", out.Count)
	}
	if !out.Truncated {
		t.Error("有文件被截断就该报出来,否则会据此下「改动很小」的错结论")
	}
	if len(out.Changes[0].Diff) > 200 {
		t.Error("大文件该截断")
	}
	if out.Changes[1].Diff != "+ok" || !out.Changes[1].New {
		t.Errorf("小文件不该受影响: %+v", out.Changes[1])
	}
	if out.SourceBranch != "f" || out.TargetBranch != "main" {
		t.Errorf("分支信息该带上: %+v", out)
	}
}

// MR comments and Issue comments go through the same Note event, routed to two different events by noteable_type.
func TestNoteEventRoutesToIssueOrMr(t *testing.T) {
	var issues, mrs int
	ctx := &fakeSourceCtx{fakeCtx: newFake(map[string]string{}),
		onPayload: func(ev string, p any) {
			switch p.(type) {
			case *IssueCommentedEvent:
				issues++
			case *MrCommentedEvent:
				mrs++
			}
		}}
	ev := func(js string) {
		var e glEvent
		if err := json.Unmarshal([]byte(js), &e); err != nil {
			t.Fatal(err)
		}
		triggerEvent(ctx, Cred{}, "g/p", e)
	}
	ev(`{"id":1,"action_name":"commented on","target_type":"Note",
	  "note":{"body":"a","noteable_iid":4,"noteable_type":"Issue"}}`)
	ev(`{"id":2,"action_name":"commented on","target_type":"Note",
	  "note":{"body":"b","noteable_iid":9,"noteable_type":"MergeRequest"}}`)
	if issues != 1 || mrs != 1 {
		t.Fatalf("该各触发一次: issue=%d mr=%d", issues, mrs)
	}
}

// Search: hit shapes differ a lot by scope (code gives path/startline/data, Issue gives
// iid/title), normalized into one struct -- the canvas shouldn't need two separate drill-down
// paths depending on whether you searched code or an Issue.
func TestSearchNormalizesScopes(t *testing.T) {
	var gotPath, gotScope string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotScope = r.URL.EscapedPath(), r.URL.Query().Get("scope")
		w.Header().Set("X-Total", "9")
		switch gotScope {
		case "blobs":
			_, _ = io.WriteString(w, `[{"basename":"server","path":"internal/api/server.go",
			  "startline":42,"ref":"main","data":"func Serve() {","project_id":7}]`)
		default:
			_, _ = io.WriteString(w, `[{"iid":12,"title":"登录超时","description":"复现步骤…","web_url":"u"}]`)
		}
	}))
	defer srv.Close()
	ctx := newFake(credFor(srv))

	code, err := opSearch(ctx, &SearchIn{Query: "func Serve", Scope: "blobs", Project: "g/p"})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v4/projects/g%2Fp/search" {
		t.Errorf("限定项目该走项目搜索: %s", gotPath)
	}
	h := code.Results[0]
	if h.Path != "internal/api/server.go" || h.Line != 42 || h.Ref != "main" {
		t.Errorf("代码命中该给路径/行号/分支: %+v", h)
	}
	if code.Total != 9 {
		t.Errorf("总数该从响应头取: %d", code.Total)
	}

	iss, _ := opSearch(ctx, &SearchIn{Query: "登录", Scope: "issues"})
	if gotPath != "/api/v4/search" {
		t.Errorf("不限定项目该走全局搜索: %s", gotPath)
	}
	if iss.Results[0].Iid != 12 || iss.Results[0].Title != "登录超时" {
		t.Errorf("Issue 命中该给编号/标题: %+v", iss.Results[0])
	}
}

// Inline comments: the three locating shas are fetched by the plugin itself from the MR --
// expecting the caller to fill in three base/start/head shas on the canvas would never work correctly.
func TestMrDiscussionFetchesDiffRefs(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, `{"diff_refs":{"base_sha":"b1","start_sha":"s1","head_sha":"h1"}}`)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = io.WriteString(w, `{"id":"disc1","notes":[{"id":55}]}`)
	}))
	defer srv.Close()

	out, err := opMrDiscussion(newFake(credFor(srv)), &MrDiscussionIn{
		Project: "g/p", Iid: 3, Path: "a/b.go", Line: 10, Body: "这里会空指针"})
	if err != nil {
		t.Fatal(err)
	}
	pos, _ := body["position"].(map[string]any)
	for k, want := range map[string]any{"base_sha": "b1", "start_sha": "s1", "head_sha": "h1",
		"new_path": "a/b.go", "position_type": "text"} {
		if pos[k] != want {
			t.Errorf("position.%s = %v, want %v", k, pos[k], want)
		}
	}
	if _, has := pos["old_line"]; has {
		t.Error("没给原文件行号就不该发 old_line（会把评论定位到错的一侧）")
	}
	if out.DiscussionID != "disc1" || out.NoteID != 55 {
		t.Errorf("出参不对: %+v", out)
	}

	// Neither line number given = can't locate it; reject outright rather than send a request that's bound to fail
	if _, err := opMrDiscussion(newFake(credFor(srv)), &MrDiscussionIn{
		Project: "g/p", Iid: 3, Path: "a.go", Body: "x"}); err == nil {
		t.Error("没给任何行号该报错")
	}
}

// raw's shape is inherently different between the two paths (polling gives an Events API
// event object, webhook gives a Hook request body) -- it can't be normalized, but downstream
// must still be able to tell them apart, otherwise the same {{raw.xxx}} would get nothing once
// the deployment switches paths. So source is required, on both paths without exception.
func TestEventsCarrySource(t *testing.T) {
	var srcs []string
	ctx := &fakeSourceCtx{fakeCtx: newFake(map[string]string{}),
		onPayload: func(ev string, p any) {
			switch e := p.(type) {
			case *CommitPushedEvent:
				srcs = append(srcs, e.Source)
			case *IssueOpenedEvent:
				srcs = append(srcs, e.Source)
			}
		}}

	// webhook path
	handleWebhook(ctx, &sokel.WebhookRequest{
		Headers: map[string]string{"X-Gitlab-Event": "Push Hook"},
		Body: []byte(`{"after":"a1","ref":"refs/heads/main","total_commits_count":1,
		  "project":{"path_with_namespace":"g/p"},"commits":[{"title":"c"}]}`)})
	handleWebhook(ctx, &sokel.WebhookRequest{
		Headers: map[string]string{"X-Gitlab-Event": "Issue Hook"},
		Body: []byte(`{"object_attributes":{"iid":1,"action":"open","title":"t"},
		  "project":{"path_with_namespace":"g/p"}}`)})
	// polling path
	var e glEvent
	if err := json.Unmarshal([]byte(`{"id":9,"action_name":"pushed to","target_type":"",
	  "push_data":{"commit_count":1,"ref":"main","commit_to":"a2","commit_title":"c"},
	  "author":{"username":"dev"}}`), &e); err != nil {
		t.Fatal(err)
	}
	triggerEvent(ctx, Cred{}, "g/p", e)

	if len(srcs) != 3 {
		t.Fatalf("三个事件都该产出,got %d: %+v", len(srcs), srcs)
	}
	for i, s := range srcs {
		if s == "" {
			t.Errorf("第 %d 个事件没填 source——下游就没法知道 raw 是哪种形状", i)
		}
	}
	if srcs[0] != "webhook" || srcs[1] != "webhook" || srcs[2] != "poll" {
		t.Errorf("source 取值不对: %+v", srcs)
	}
}
