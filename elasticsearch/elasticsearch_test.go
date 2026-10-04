package main

// httptest-based fake ES exercises the critical paths. Live-cluster integration goes
// through operation:test.
//
// What's being watched here are a few things **you can't catch without looking at the
// raw request**: _bulk's NDJSON shape, the gate on delete-type operations, the atomicity
// of alias switching, and total's lower-bound semantics.

import (
	"context"
	"encoding/json"
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

func (f *fakeCtx) Credential() map[string]string { return f.cred }
func (f *fakeCtx) Upload(string, string, []byte) (*plugin.File, error) {
	return &plugin.File{ID: "f"}, nil
}
func (f *fakeCtx) UploadReader(string, string, io.Reader) (*plugin.File, error) {
	return &plugin.File{ID: "f"}, nil
}
func (f *fakeCtx) Fetch(*plugin.File) ([]byte, error) { return nil, nil }

func ctxFor(srv *httptest.Server) *fakeCtx {
	return &fakeCtx{Context: context.Background(), cred: map[string]string{"base_url": srv.URL}}
}

// _bulk's request body is **NDJSON** (one action line, one document line, trailing
// newline required), not a JSON array — sending an array gets a 400 from ES. id_field
// must land on the action line's _id, which is the basis for "a rerun doesn't write
// duplicates".
func TestBulkIsNDJSONWithIDs(t *testing.T) {
	var gotBody, gotType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody, gotType = string(b), r.Header.Get("Content-Type")
		_, _ = io.WriteString(w, `{"took":7,"items":[
			{"index":{"_id":"a","result":"created"}},
			{"index":{"_id":"b","error":{"type":"mapper_parsing_exception","reason":"字段类型不对"}}}]}`)
	}))
	defer srv.Close()

	out, err := opBulkIndex(ctxFor(srv), &BulkIndexIn{
		Index: "logs", IDField: "sku",
		Documents: []any{
			map[string]any{"sku": "a", "n": 1},
			map[string]any{"sku": "b", "n": 2},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotType != "application/x-ndjson" {
		t.Errorf("Content-Type 该是 x-ndjson，got %q", gotType)
	}
	lines := strings.Split(strings.TrimRight(gotBody, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("两篇文档该是 4 行（动作/文档 各两行），got %d 行:\n%s", len(lines), gotBody)
	}
	if !strings.HasSuffix(gotBody, "\n") {
		t.Error("NDJSON 末尾必须有换行，否则 ES 回 400")
	}
	var action map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &action); err != nil {
		t.Fatalf("动作行不是 JSON: %s", lines[0])
	}
	idx, _ := action["index"].(map[string]any)
	if idx["_id"] != "a" || idx["_index"] != "logs" {
		t.Errorf("动作行该带 _id=a/_index=logs，got %+v", idx)
	}
	// Bulk is **partial success**: one failure doesn't affect the other, both counters must be correct.
	if out.Indexed != 1 || out.Failed != 1 {
		t.Errorf("该是成功 1 失败 1，got indexed=%d failed=%d", out.Indexed, out.Failed)
	}
	if len(out.Errors) != 1 || !strings.Contains(out.Errors[0], "字段类型不对") {
		t.Errorf("失败原因该带上 ES 的 reason 原文，got %+v", out.Errors)
	}
}

// Missing the field named by id_field must **error out immediately**, not silently fall
// back to a random ID — that would make a rerun write duplicate data.
func TestBulkMissingIDFieldFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("不该发出请求")
	}))
	defer srv.Close()
	_, err := opBulkIndex(ctxFor(srv), &BulkIndexIn{
		Index: "logs", IDField: "sku", Documents: []any{map[string]any{"n": 1}},
	})
	if err == nil || !strings.Contains(err.Error(), "sku") {
		t.Fatalf("该报出缺哪个字段，got %v", err)
	}
}

// Two gates: delete-by-query with no query = wipe the whole index; delete-index with a
// wildcard = wipe a large chunk. Both must be blocked **before the request is sent**.
func TestDangerousOpsAreGated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("危险操作不该发出请求: %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()
	ctx := ctxFor(srv)

	if _, err := opDeleteByQuery(ctx, &DeleteByQueryIn{Index: "logs"}); err == nil {
		t.Error("按查询删除不给查询该被拒")
	}
	for _, name := range []string{"*", "_all", "logs-*", "a,b"} {
		if _, err := opIndexDelete(ctx, &IndexDeleteIn{Name: name}); err == nil {
			t.Errorf("删索引不该接受 %q", name)
		}
	}
}

// Search: relation=gte on total means "at least this many" (ES only counts exactly up
// to 10000 by default), and this must be surfaced in the contract — otherwise "there are
// only 10000 in total" would propagate straight into reports. Also pins down the sort
// conversion and the "aggs given, size left empty → size=0" interaction.
func TestSearchTotalLowerBoundAndBody(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = io.WriteString(w, `{"took":12,"hits":{"total":{"value":10000,"relation":"gte"},
			"hits":[{"_id":"1","_index":"logs","_score":1.5,"_source":{"msg":"x"}}]},
			"aggregations":{"by_host":{"buckets":[]}}}`)
	}))
	defer srv.Close()

	out, err := opSearch(ctxFor(srv), &SearchIn{
		Index: "logs", Q: "status:error", Sort: []string{"@timestamp:desc"},
		Aggs: map[string]any{"by_host": map[string]any{"terms": map[string]any{"field": "host.keyword"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Total != 10000 || !out.TotalIsLowerBound {
		t.Errorf("relation=gte 该翻成「总数是下界」，got total=%d lower=%v", out.Total, out.TotalIsLowerBound)
	}
	if out.Count != 1 || out.Hits[0].Source["msg"] != "x" {
		t.Errorf("命中文档该解出 _source，got %+v", out.Hits)
	}
	if out.Aggregations == nil {
		t.Error("聚合结果该透出来")
	}
	// aggs given, size left empty → return only the aggregations (size=0)
	if size, ok := body["size"].(float64); !ok || size != 0 {
		t.Errorf("给了聚合且取几条留空时该发 size=0，got %v", body["size"])
	}
	sorts, _ := body["sort"].([]any)
	if len(sorts) != 1 {
		t.Fatalf("排序该转成数组，got %v", body["sort"])
	}
	s0, _ := sorts[0].(map[string]any)
	ts, _ := s0["@timestamp"].(map[string]any)
	if ts["order"] != "desc" {
		t.Errorf(`"@timestamp:desc" 该转成 {"@timestamp":{"order":"desc"}}，got %v`, sorts[0])
	}
	// the shorthand query must be wrapped as query_string, not stuffed into query as-is
	q := body["query"].(map[string]any)
	if _, ok := q["query_string"]; !ok {
		t.Errorf("简式查询该包成 query_string，got %v", q)
	}
}

// When size is given an explicit value, that value wins (don't let the aggs rule override it).
func TestSearchExplicitSizeWins(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = io.WriteString(w, `{"hits":{"total":{"value":3,"relation":"eq"},"hits":[]}}`)
	}))
	defer srv.Close()
	if _, err := opSearch(ctxFor(srv), &SearchIn{
		Index: "logs", Size: 5, Aggs: map[string]any{"a": map[string]any{}},
	}); err != nil {
		t.Fatal(err)
	}
	if size, _ := body["size"].(float64); size != 5 {
		t.Errorf("显式给的 size 该赢，got %v", body["size"])
	}
}

// A missing document (404 with no error body) is a normal branch; a **missing index**
// (404 with an error body) is a config error and must be reported — both return 404, and
// conflating them makes "couldn't find it" look like "no data".
func TestDocGetMissVsIndexMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		if strings.HasPrefix(r.URL.Path, "/ghost/") {
			_, _ = io.WriteString(w, `{"error":{"type":"index_not_found_exception","reason":"no such index [ghost]"},"status":404}`)
			return
		}
		_, _ = io.WriteString(w, `{"_index":"logs","_id":"1","found":false}`)
	}))
	defer srv.Close()
	ctx := ctxFor(srv)

	out, err := opDocGet(ctx, &DocGetIn{Index: "logs", ID: "1"})
	if err != nil {
		t.Fatalf("文档不存在不该报错: %v", err)
	}
	if out.Found {
		t.Error("该回 found=false")
	}
	if _, err := opDocGet(ctx, &DocGetIn{Index: "ghost", ID: "1"}); err == nil {
		t.Fatal("索引不存在该报错（否则会被当成「没这条数据」）")
	}
}

// Switching an alias must be **remove+add inside a single _aliases request**: in the
// instant between two separate calls, the alias would point to nothing or to both
// indices, and a query that lands right then reads the wrong data.
func TestAliasSwitchIsAtomic(t *testing.T) {
	var actionsBody map[string]any
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/_alias/"):
			_, _ = io.WriteString(w, `{"logs-v1":{"aliases":{"logs":{}}}}`)
		case r.URL.Path == "/_aliases":
			calls++
			_ = json.NewDecoder(r.Body).Decode(&actionsBody)
			_, _ = io.WriteString(w, `{"acknowledged":true}`)
		}
	}))
	defer srv.Close()

	out, err := opAliasSwitch(ctxFor(srv), &AliasSwitchIn{Alias: "logs", ToIndex: "logs-v2"})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("_aliases 该只调一次（原子切换），got %d 次", calls)
	}
	actions, _ := actionsBody["actions"].([]any)
	if len(actions) != 2 {
		t.Fatalf("该同时含 remove 与 add，got %v", actions)
	}
	first, _ := actions[0].(map[string]any)
	if _, ok := first["remove"]; !ok {
		t.Errorf("remove 该排在 add 之前，got %v", actions[0])
	}
	if len(out.RemovedFrom) != 1 || out.RemovedFrom[0] != "logs-v1" {
		t.Errorf("该报出从哪些索引上摘掉了，got %+v", out.RemovedFrom)
	}
}

// Index-create hitting "already exists" is an idempotent result, not an error (rerunning an index-create flow is common).
func TestIndexCreateAlreadyExists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"type":"resource_already_exists_exception","reason":"index [logs/x] already exists"},"status":400}`)
	}))
	defer srv.Close()
	out, err := opIndexCreate(ctxFor(srv), &IndexCreateIn{Name: "logs"})
	if err != nil {
		t.Fatalf("已存在不该报错: %v", err)
	}
	if out.Created || !out.Existed {
		t.Errorf("该回 created=false existed=true，got %+v", out)
	}
}

// ES's own error text (error.reason) must be passed through verbatim — it usually
// states the problem directly, and paraphrasing it would only lose information.
func TestErrorSurfacesESReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"type":"search_phase_execution_exception","reason":"Fielddata is disabled on [host]"},"status":400}`)
	}))
	defer srv.Close()
	_, err := opSearch(ctxFor(srv), &SearchIn{Index: "logs"})
	if err == nil || !strings.Contains(err.Error(), "Fielddata is disabled") {
		t.Fatalf("该带上 ES 的 reason 原文，got %v", err)
	}
}

// Auth: API Key takes priority over username/password (must not send basic auth when both are filled in).
func TestAuthPrefersAPIKey(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, `{"count":0}`)
	}))
	defer srv.Close()
	ctx := &fakeCtx{Context: context.Background(), cred: map[string]string{
		"base_url": srv.URL, "username": "elastic", "password": "p", "api_key": "KEY123",
	}}
	if _, err := opCount(ctx, &CountIn{Index: "logs"}); err != nil {
		t.Fatal(err)
	}
	if auth != "ApiKey KEY123" {
		t.Errorf("该用 ApiKey，got %q", auth)
	}
}

// Health check: a failed connection returns ok=false + a message, **not** an error
// (an error return would leave nothing but a red X in the UI).
func TestHealthCheckReportsInsteadOfErroring(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			_, _ = io.WriteString(w, `{"cluster_name":"prod","version":{"number":"8.17.2"}}`)
		case "/_cluster/health":
			_, _ = io.WriteString(w, `{"status":"green"}`)
		}
	}))
	defer srv.Close()
	out, err := opHealthCheck(ctxFor(srv), &HealthCheckIn{})
	if err != nil || !out.OK {
		t.Fatalf("该体检通过: %+v err=%v", out, err)
	}
	if out.Version != "8.17.2" || out.Distribution != "elasticsearch" || out.Status != "green" {
		t.Errorf("版本/发行版/状态都该报出来，got %+v", out)
	}

	dead := &fakeCtx{Context: context.Background(), cred: map[string]string{"base_url": "http://127.0.0.1:1"}}
	bad, err := opHealthCheck(dead, &HealthCheckIn{})
	if err != nil {
		t.Fatalf("连不上该回 ok=false 而不是报错: %v", err)
	}
	if bad.OK || bad.Message == "" {
		t.Fatalf("该给出不可用的说明: %+v", bad)
	}
}

// The index list skips system indices (.kibana and the like don't belong on the canvas), and parses _cat's stringified numbers.
func TestIndicesListSkipsSystem(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[
			{"index":".kibana_1","health":"green","status":"open","docs.count":"5","store.size":"1kb"},
			{"index":"logs","health":"yellow","status":"open","docs.count":"1234","store.size":"3.4mb"}]`)
	}))
	defer srv.Close()
	out, err := opIndicesList(ctxFor(srv), &IndicesListIn{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Count != 1 || out.Indices[0].Name != "logs" {
		t.Fatalf("系统索引该被跳过，got %+v", out.Indices)
	}
	if out.Indices[0].Docs != 1234 || out.Indices[0].StoreSize != "3.4mb" {
		t.Errorf("文档数/占用该解出来，got %+v", out.Indices[0])
	}
}
