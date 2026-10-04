package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The platform has no "remote dropdown, pick a database" control, so every id has to be pasted in,
// and what a person has on hand is the browser address-bar link. **A database link's ?v= is the
// view id** — querying with it as the database id gets you an object_not_found with no clue why.
func TestNotionID(t *testing.T) {
	const id = "1234567890abcdef1234567890abcdef"
	cases := []struct {
		name, in, want string
	}{
		{"裸 id", id, id},
		{"带横线的 uuid", "12345678-90ab-cdef-1234-567890abcdef", id},
		{"页面链接", "https://www.notion.so/myws/项目周报-" + id, id},
		{"带 query 的页面链接", "https://www.notion.so/项目周报-" + id + "?pvs=4", id},
		// The view id lives in the query — must never be picked up
		{"数据库链接带视图 id", "https://www.notion.so/myws/" + id + "?v=ffffffffffffffffffffffffffffffff", id},
		{"链接里两段 hex 取最后一段", "https://www.notion.so/" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + "/x-" + id, id},
		{"空", "", ""},
		// Unrecognized — hand it to Notion as-is and let its error speak (don't second-guess it into empty)
		{"认不出", "我的项目库", "我的项目库"},
	}
	for _, c := range cases {
		if got := notionID(c.in); got != c.want {
			t.Errorf("%s: notionID(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

// Read-side normalization: 25 property value types -> flat values. This is the sole source of
// truth for downstream references and model reads; if the shape changes, downstream reference
// paths break.
func TestNormalizeProp(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]any
		want any
	}{
		{"标题", prop("title", "title", []any{rt("周报")}), "周报"},
		{"富文本拼接多段", prop("rich_text", "rich_text", []any{rt("上"), rt("下")}), "上下"},
		{"数字", prop("number", "number", 42.0), 42.0},
		{"单选", prop("select", "select", map[string]any{"name": "进行中"}), "进行中"},
		{"状态", prop("status", "status", map[string]any{"name": "完成"}), "完成"},
		{"多选", prop("multi_select", "multi_select", []any{
			map[string]any{"name": "A"}, map[string]any{"name": "B"}}), []string{"A", "B"}},
		{"复选框", prop("checkbox", "checkbox", true), true},
		{"链接", prop("url", "url", "https://x.com"), "https://x.com"},
		// Constant shape: give an end key even when there's no end, otherwise downstream
		// reference paths are a gamble
		{"日期只有开始", prop("date", "date", map[string]any{"start": "2026-08-20"}),
			map[string]any{"start": "2026-08-20", "end": ""}},
		{"日期区间", prop("date", "date", map[string]any{"start": "2026-08-20", "end": "2026-08-25"}),
			map[string]any{"start": "2026-08-20", "end": "2026-08-25"}},
		{"空日期", prop("date", "date", nil), map[string]any{"start": "", "end": ""}},
		// People give the name (this column is mostly used for display); writing back accepts
		// both name and id, see peopleValue
		{"人", prop("people", "people", []any{
			map[string]any{"id": "u1", "name": "小明"}, map[string]any{"id": "u2"}}), []string{"小明", "u2"}},
		{"文件取地址", prop("files", "files", []any{
			map[string]any{"name": "a.pdf", "file": map[string]any{"url": "https://f/a.pdf"}},
			map[string]any{"name": "b", "external": map[string]any{"url": "https://e/b"}}}),
			[]string{"https://f/a.pdf", "https://e/b"}},
		{"关联给 id", prop("relation", "relation", []any{map[string]any{"id": "p1"}}), []string{"p1"}},
		// Formula/rollup: unwrap to the inner value instead of dumping the wrapper to downstream as-is
		{"公式取里层", prop("formula", "formula", map[string]any{"type": "number", "number": 7.0}), 7.0},
		{"汇总取里层", prop("rollup", "rollup", map[string]any{"type": "number", "number": 3.0}), 3.0},
		{"唯一 id 带前缀", prop("unique_id", "unique_id", map[string]any{"prefix": "TASK", "number": 12.0}), "TASK-12"},
		// Unrecognized types return nil: it's in properties_raw, no need to guess here
		{"按钮类归一化不出", prop("button", "button", map[string]any{}), nil},
	}
	for _, c := range cases {
		got := normalizeProp(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %#v, want %#v", c.name, got, c.want)
		}
	}
}

func prop(typ, key string, v any) map[string]any {
	return map[string]any{"type": typ, key: v}
}

func rt(s string) map[string]any {
	return map[string]any{"plain_text": s}
}

// Write side: the same string could need wrapping as select, status, or rich_text — the value
// alone can't tell you which — so it must always be assembled using the schema's declared type.
func TestPropValue(t *testing.T) {
	cases := []struct {
		name string
		spec propType
		in   any
		want map[string]any
	}{
		{"单选", propType{Type: "select"}, "进行中",
			map[string]any{"select": map[string]any{"name": "进行中"}}},
		{"状态（同样的字符串，不同的包装）", propType{Type: "status"}, "进行中",
			map[string]any{"status": map[string]any{"name": "进行中"}}},
		{"复选框认字符串", propType{Type: "checkbox"}, "true", map[string]any{"checkbox": true}},
		{"数字认字符串", propType{Type: "number"}, "42", map[string]any{"number": 42.0}},
		{"日期字符串", propType{Type: "date"}, "2026-08-20",
			map[string]any{"date": map[string]any{"start": "2026-08-20"}}},
		{"日期区间", propType{Type: "date"}, map[string]any{"start": "a", "end": "b"},
			map[string]any{"date": map[string]any{"start": "a", "end": "b"}}},
		// A date that was read back (end is an empty string) must not turn into an empty range
		// when written straight back
		{"读出来的日期原样回写", propType{Type: "date"}, map[string]any{"start": "a", "end": ""},
			map[string]any{"date": map[string]any{"start": "a"}}},
		{"显式 null 清空", propType{Type: "rich_text"}, nil, map[string]any{"rich_text": []any{}}},
		{"关联认链接", propType{Type: "relation"}, "https://notion.so/x-1234567890abcdef1234567890abcdef",
			map[string]any{"relation": []any{map[string]any{"id": "1234567890abcdef1234567890abcdef"}}}},
		// Models often give "A" instead of ["A"]; erroring over this would just be friction
		{"多选认单值", propType{Type: "multi_select"}, "A",
			map[string]any{"multi_select": []any{map[string]any{"name": "A"}}}},
		{"多选认逗号分隔", propType{Type: "multi_select"}, "A,B",
			map[string]any{"multi_select": []any{map[string]any{"name": "A"}, map[string]any{"name": "B"}}}},
	}
	for _, c := range cases {
		got, err := propValue(nil, c.spec, c.in)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %#v, want %#v", c.name, got, c.want)
		}
	}
}

// A value not among the candidates: Notion silently creates a new option for select, but errors
// outright for status. Both should be caught before the request is sent, with the valid options
// spelled out — this error is meant for the model to read.
func TestPropValueRejectsUnknownOption(t *testing.T) {
	_, err := propValue(nil, propType{Type: "status", Options: []string{"待办", "完成"}}, "进行中")
	if err == nil {
		t.Fatal("不在候选值里也放行了")
	}
	if !strings.Contains(err.Error(), "待办") || !strings.Contains(err.Error(), "完成") {
		t.Errorf("错误里要列出可选项, got %v", err)
	}
}

// A single rich-text object caps out at 2000 characters; go over and Notion rejects the whole
// request — and "writing a model's long-form output into a column" is exactly the most common use
// case.
func TestRichTextChunks(t *testing.T) {
	long := strings.Repeat("字", 4500)
	parts := richText(long)
	if len(parts) != 3 {
		t.Fatalf("4500 字应切成 3 段, got %d", len(parts))
	}
	total := 0
	for _, p := range parts {
		m := p.(map[string]any)["text"].(map[string]any)
		s := m["content"].(string)
		if n := len([]rune(s)); n > 2000 {
			t.Errorf("有一段 %d 字，超过 2000", n)
		}
		total += len([]rune(s))
	}
	if total != 4500 {
		t.Errorf("切完少了字: %d", total)
	}
	if len(richText("")) != 0 {
		t.Error("空串应产出空数组（而不是一个空的富文本对象）")
	}
}

// object_not_found is almost never a wrong id — it's a page that hasn't been shared with the
// integration. Notion's own message never mentions this, so without a translation everyone has to
// run into it themselves.
func TestErrorMentionsSharing(t *testing.T) {
	e := &apiError{Status: 404, Code: "object_not_found", Message: "Could not find page with ID x"}
	if !strings.Contains(e.Error(), "连接") {
		t.Errorf("错误里要说清怎么把页面交给集成, got %v", e)
	}
	e2 := &apiError{Status: 401, Code: "unauthorized", Message: "API token is invalid"}
	if !strings.Contains(e2.Error(), "授权") && !strings.Contains(e2.Error(), "密钥") {
		t.Errorf("401 要指向令牌本身, got %v", e2)
	}
}

// The token has two sources: the internal integration secret takes priority, falling back to the
// access_token injected by authorization. If neither is set, spell out both paths.
func TestAuthToken(t *testing.T) {
	if tok, _ := authToken(Cred{Token: "ntn_a", AccessToken: "b"}); tok != "ntn_a" {
		t.Errorf("内部集成密钥优先, got %q", tok)
	}
	if tok, _ := authToken(Cred{AccessToken: "b"}); tok != "b" {
		t.Errorf("没填密钥时用授权令牌, got %q", tok)
	}
	_, err := authToken(Cred{})
	if err == nil || !strings.Contains(err.Error(), "授权") {
		t.Errorf("两个都没有时要说清两条路, got %v", err)
	}
}

// Hitting the rate limit is the normal case, not an exception (3 req/sec): wait for Retry-After
// and try again, instead of throwing the error at the user.
func TestRetryOnRateLimit(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		if r.Header.Get("Notion-Version") != notionVersion {
			t.Errorf("没带版本头：Notion 会按最老的版本回，数据源与 markdown 接口都不存在")
		}
		_, _ = w.Write([]byte(`{"id":"p1"}`))
	}))
	defer srv.Close()

	var out struct {
		ID string `json:"id"`
	}
	if err := doWithRetry(context.Background(), srv.Client(), "tok-retry", http.MethodGet, srv.URL, nil, &out); err != nil {
		t.Fatal(err)
	}
	if hits != 2 || out.ID != "p1" {
		t.Errorf("应重试一次后成功: hits=%d out=%+v", hits, out)
	}
}

// A 4xx must surface Notion's code (the translation depends on it).
func TestAPIErrorDecoded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"object":"error","status":404,"code":"object_not_found","message":"no"}`))
	}))
	defer srv.Close()
	err := doWithRetry(context.Background(), srv.Client(), "tok-404", http.MethodGet, srv.URL, nil, nil)
	ae, ok := err.(*apiError)
	if !ok || ae.Code != "object_not_found" {
		t.Fatalf("要解出 code, got %#v", err)
	}
	if !isNotFound(err) {
		t.Error("isNotFound 认不出它 —— 「贴的是库不是表」那条兜底会失效")
	}
}

// One credential can watch multiple tables, each tracked separately: sharing a single timestamp
// would let heavy changes on one table drag another table's progress forward too, and that
// table's rows would never fire.
func TestCursorsRoundTrip(t *testing.T) {
	in := map[string]string{"ds1": "2026-08-12T10:00:00Z", "ds2": "2026-08-11T09:00:00Z"}
	got := parseCursors(dumpCursors(in))
	if !reflect.DeepEqual(got, in) {
		t.Errorf("往返丢了: %v", got)
	}
	// Dirty data (whatever a person typed in by hand) must not crash the source — falling back to
	// "first start" is fine
	if len(parseCursors("坏掉的")) != 0 || len(parseCursors("")) != 0 {
		t.Error("解不开的游标应退化成空")
	}
}

func TestPollInterval(t *testing.T) {
	cases := map[string]time.Duration{
		"":     defaultPollSeconds * time.Second,
		"abc":  defaultPollSeconds * time.Second,
		"0":    defaultPollSeconds * time.Second,
		"120":  120 * time.Second,
		"1":    minPollSeconds * time.Second, // Faster than the rate limit itself is pointless
		"-100": defaultPollSeconds * time.Second,
	}
	for in, want := range cases {
		if got := pollInterval(in); got != want {
			t.Errorf("pollInterval(%q) = %v, want %v", in, got, want)
		}
	}
}

// Creation and modification mean completely different things to a workflow, yet Notion doesn't
// give us this distinction.
func TestIsCreated(t *testing.T) {
	cases := []struct {
		name            string
		created, edited string
		want            bool
	}{
		{"刚建的", "2026-08-12T10:00:00Z", "2026-08-12T10:00:00Z", true},
		{"建完顺手填了属性", "2026-08-12T10:00:00Z", "2026-08-12T10:00:30Z", true},
		{"隔天改的", "2026-08-11T10:00:00Z", "2026-08-12T10:00:00Z", false},
		{"缺时间戳", "", "2026-08-12T10:00:00Z", false},
	}
	for _, c := range cases {
		if got := isCreated(notionPage{CreatedTime: c.created, LastEditedTime: c.edited}); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

// Keeping a cap matters: pulling tens of thousands of rows at once would burn through the rate
// limit and bloat the run record.
func TestClamp(t *testing.T) {
	if clamp(0, 100, 1000) != 100 || clamp(5000, 100, 1000) != 1000 || clamp(7, 100, 1000) != 7 {
		t.Error("clamp 的默认值/上限没生效")
	}
}

func TestSplitList(t *testing.T) {
	got := splitList(" ds1, ds2\nds3 ")
	if !reflect.DeepEqual(got, []string{"ds1", "ds2", "ds3"}) {
		t.Errorf("got %#v", got)
	}
	if len(splitList("")) != 0 {
		t.Error("空配置应得到空列表（= 不用事件，不是报错）")
	}
}

// The title column's name differs per database ("名称", "Name", "任务"); writing a title means
// looking it up from the schema.
func TestTitleColumn(t *testing.T) {
	specs := map[string]propType{"任务": {Type: "title"}, "状态": {Type: "status"}}
	if got := titleColumn(specs); got != "任务" {
		t.Errorf("got %q", got)
	}
	if titleColumn(map[string]propType{"状态": {Type: "status"}}) != "" {
		t.Error("没有标题列时该返回空，由调用方决定怎么办")
	}
}

// A page's title lives in the properties column with type=title, not under some fixed key.
func TestPageTitle(t *testing.T) {
	props := map[string]any{
		"状态": prop("status", "status", map[string]any{"name": "完成"}),
		"任务": prop("title", "title", []any{rt("修登录 bug")}),
	}
	if got := pageTitle(props); got != "修登录 bug" {
		t.Errorf("got %q", got)
	}
}

// A parent's id key name changes with its kind; reading it off a fixed key is bound to come back empty.
func TestParentOf(t *testing.T) {
	kind, id := parentOf(map[string]any{"type": "data_source_id", "data_source_id": "ds1"})
	if kind != "data_source" || id != "ds1" {
		t.Errorf("got %q %q", kind, id)
	}
	if kind, _ := parentOf(map[string]any{"type": "workspace", "workspace": true}); kind != "workspace" {
		t.Errorf("工作空间父认不出: %q", kind)
	}
	if kind, id := parentOf(nil); kind != "" || id != "" {
		t.Errorf("空 parent 不该崩: %q %q", kind, id)
	}
}
