package main

// httptest 假 ES 打穿关键路径。真集群联调走 operation:test。
//
// 盯的是几个**不看请求原文就发现不了**的地方：_bulk 的 NDJSON 形态、
// 删除类操作的闸、切别名的原子性、以及 total 的下界语义。

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

// _bulk 的请求体是 **NDJSON**（一行动作一行文档、末尾必须换行），不是 JSON 数组——
// 按数组发过去 ES 回 400。id_field 要落到动作行的 _id 上，那是「重跑不写重复」的根。
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
	// 批量是**部分成功**的：一条失败不影响另一条，两个计数都要对。
	if out.Indexed != 1 || out.Failed != 1 {
		t.Errorf("该是成功 1 失败 1，got indexed=%d failed=%d", out.Indexed, out.Failed)
	}
	if len(out.Errors) != 1 || !strings.Contains(out.Errors[0], "字段类型不对") {
		t.Errorf("失败原因该带上 ES 的 reason 原文，got %+v", out.Errors)
	}
}

// id_field 指的字段缺了要**当场报错**，不能悄悄写成随机 ID——那样重跑就写出重复数据。
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

// 两道闸：按查询删除不给查询 = 删光索引；删索引带通配 = 删一大片。
// 都必须在**发请求之前**拦住。
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

// 搜索：total 的 relation=gte 意思是「至少这么多」（ES 默认只精确到 10000），
// 这件事必须出到契约里——否则「一共就 10000 条」会一路传进报表。
// 顺带钉住排序转换与「给了聚合、取几条留空 → size=0」的互动。
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
	// 给了聚合、取几条留空 → 只回聚合（size=0）
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
	// 简式查询要包成 query_string，不能原样塞进 query
	q := body["query"].(map[string]any)
	if _, ok := q["query_string"]; !ok {
		t.Errorf("简式查询该包成 query_string，got %v", q)
	}
}

// 取几条给了具体值时以它为准（别被聚合那条规则盖掉）。
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

// 文档不存在（404 无 error 体）是正常分支；**索引不存在**（404 带 error 体）是配置错，
// 必须报出来——两者都回 404，混为一谈的话查不到东西还以为是没数据。
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

// 切别名必须是**一次 _aliases 请求里 remove+add**：分两次调用中间那一瞬，
// 别名会指向空或同时指向两个索引，查询方正好撞上就读到错的数据。
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

// 建索引遇到「已存在」是幂等结果不是错误（重跑建索引的流程很常见）。
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

// ES 的报错原文（error.reason）必须原样带出来——它往往直接说明了问题，
// 翻译一遍反而丢信息。
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

// 认证：API Key 优先于用户名密码（两个都填时不能发 basic）。
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

// 体检：连不上回 ok=false + 说明，**不是** error（抛错在界面上只剩一个红叉）。
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

// 索引列表跳过系统索引（.kibana 那些不该摆到画布上），并解出 _cat 的字符串数字。
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
