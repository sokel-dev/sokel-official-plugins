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

// 平台没有「远程下拉选一个数据库」那种控件，所有 id 只能靠贴，而人手上有的是浏览器地址栏
// 里那条链接。**数据库链接的 ?v= 是视图 id**——把它当数据库 id 拿去查会得到
// object_not_found，而错因完全看不出来。
func TestNotionID(t *testing.T) {
	const id = "1234567890abcdef1234567890abcdef"
	cases := []struct {
		name, in, want string
	}{
		{"裸 id", id, id},
		{"带横线的 uuid", "12345678-90ab-cdef-1234-567890abcdef", id},
		{"页面链接", "https://www.notion.so/myws/项目周报-" + id, id},
		{"带 query 的页面链接", "https://www.notion.so/项目周报-" + id + "?pvs=4", id},
		// 视图 id 在 query 里，绝不能取它
		{"数据库链接带视图 id", "https://www.notion.so/myws/" + id + "?v=ffffffffffffffffffffffffffffffff", id},
		{"链接里两段 hex 取最后一段", "https://www.notion.so/" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + "/x-" + id, id},
		{"空", "", ""},
		// 认不出就原样交给 Notion，让它的错误去说话（别自作主张改成空）
		{"认不出", "我的项目库", "我的项目库"},
	}
	for _, c := range cases {
		if got := notionID(c.in); got != c.want {
			t.Errorf("%s: notionID(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

// 读侧归一化：25 种属性值 → 平铺值。这一份是下游引用与模型读取的唯一依据，
// 形状变了下游的引用路径就断了。
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
		// 形状恒定：没有 end 也给 end 键，否则下游的引用路径要看运气
		{"日期只有开始", prop("date", "date", map[string]any{"start": "2026-08-20"}),
			map[string]any{"start": "2026-08-20", "end": ""}},
		{"日期区间", prop("date", "date", map[string]any{"start": "2026-08-20", "end": "2026-08-25"}),
			map[string]any{"start": "2026-08-20", "end": "2026-08-25"}},
		{"空日期", prop("date", "date", nil), map[string]any{"start": "", "end": ""}},
		// 人给名字（这一列多半拿去显示）；写回时名字与 id 都认，见 peopleValue
		{"人", prop("people", "people", []any{
			map[string]any{"id": "u1", "name": "小明"}, map[string]any{"id": "u2"}}), []string{"小明", "u2"}},
		{"文件取地址", prop("files", "files", []any{
			map[string]any{"name": "a.pdf", "file": map[string]any{"url": "https://f/a.pdf"}},
			map[string]any{"name": "b", "external": map[string]any{"url": "https://e/b"}}}),
			[]string{"https://f/a.pdf", "https://e/b"}},
		{"关联给 id", prop("relation", "relation", []any{map[string]any{"id": "p1"}}), []string{"p1"}},
		// 公式/汇总：拆到里层那个值，而不是把包装原样丢给下游
		{"公式取里层", prop("formula", "formula", map[string]any{"type": "number", "number": 7.0}), 7.0},
		{"汇总取里层", prop("rollup", "rollup", map[string]any{"type": "number", "number": 3.0}), 3.0},
		{"唯一 id 带前缀", prop("unique_id", "unique_id", map[string]any{"prefix": "TASK", "number": 12.0}), "TASK-12"},
		// 认不出的类型返回 nil：properties_raw 那份里有，不必在这里瞎猜
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

// 写侧：同一个字符串该包成 select 还是 status 还是 rich_text，光看值分辨不出来，
// 所以一定按表结构里的类型来拼。
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
		// 读出来的日期（end 为空串）直接回写不能变成一个空区间
		{"读出来的日期原样回写", propType{Type: "date"}, map[string]any{"start": "a", "end": ""},
			map[string]any{"date": map[string]any{"start": "a"}}},
		{"显式 null 清空", propType{Type: "rich_text"}, nil, map[string]any{"rich_text": []any{}}},
		{"关联认链接", propType{Type: "relation"}, "https://notion.so/x-1234567890abcdef1234567890abcdef",
			map[string]any{"relation": []any{map[string]any{"id": "1234567890abcdef1234567890abcdef"}}}},
		// 模型经常给 "A" 而不是 ["A"]，为此报错纯属添堵
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

// 候选值不匹配：select 会被 Notion 悄悄新建一个选项，status 则直接报错。
// 两种都该在发请求前就拦住并说清可选项——这个错是给模型看的。
func TestPropValueRejectsUnknownOption(t *testing.T) {
	_, err := propValue(nil, propType{Type: "status", Options: []string{"待办", "完成"}}, "进行中")
	if err == nil {
		t.Fatal("不在候选值里也放行了")
	}
	if !strings.Contains(err.Error(), "待办") || !strings.Contains(err.Error(), "完成") {
		t.Errorf("错误里要列出可选项, got %v", err)
	}
}

// 单个富文本对象上限 2000 字符，超了整条请求被 Notion 拒——
// 而「把模型产出的长文写进某一列」恰恰是最常见的用法。
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

// object_not_found 十有八九不是 id 写错，而是页面没交给集成——
// Notion 的原文一个字都不提这件事，翻译不做的话每个人都要自己撞一次。
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

// 令牌两条来源：内部集成密钥优先，其次是授权注入的 access_token。都没有就说清两条路。
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

// 撞限流不是异常而是常态（3 次/秒）：按 Retry-After 等一等再来，而不是把错抛给用户。
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

// 4xx 要把 Notion 的 code 带出来（翻译依赖它）。
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

// 一个凭证可以盯多张表，各盯各的进度：共用一个时间戳的话，
// 一张表被大量修改会把另一张表的进度也推过去，那些行永远不会触发。
func TestCursorsRoundTrip(t *testing.T) {
	in := map[string]string{"ds1": "2026-08-12T10:00:00Z", "ds2": "2026-08-11T09:00:00Z"}
	got := parseCursors(dumpCursors(in))
	if !reflect.DeepEqual(got, in) {
		t.Errorf("往返丢了: %v", got)
	}
	// 脏数据（人手填了什么）不能让源崩掉，退化成「首次启动」即可
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
		"1":    minPollSeconds * time.Second, // 比限流还快没有意义
		"-100": defaultPollSeconds * time.Second,
	}
	for in, want := range cases {
		if got := pollInterval(in); got != want {
			t.Errorf("pollInterval(%q) = %v, want %v", in, got, want)
		}
	}
}

// 新增与修改对工作流的意义完全不同，而 Notion 不给这个区分。
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

// 上限压得住：一次拉几万行会把限流吃光，也会把运行记录撑爆。
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

// 标题列的列名每个库都不同（「名称」「Name」「任务」），写标题时要按表结构找。
func TestTitleColumn(t *testing.T) {
	specs := map[string]propType{"任务": {Type: "title"}, "状态": {Type: "status"}}
	if got := titleColumn(specs); got != "任务" {
		t.Errorf("got %q", got)
	}
	if titleColumn(map[string]propType{"状态": {Type: "status"}}) != "" {
		t.Error("没有标题列时该返回空，由调用方决定怎么办")
	}
}

// 页面标题在 properties 里那个 type=title 的列上，不是某个固定键。
func TestPageTitle(t *testing.T) {
	props := map[string]any{
		"状态": prop("status", "status", map[string]any{"name": "完成"}),
		"任务": prop("title", "title", []any{rt("修登录 bug")}),
	}
	if got := pageTitle(props); got != "修登录 bug" {
		t.Errorf("got %q", got)
	}
}

// parent 的 id 键名跟着类别变，写死一个键去取必然取空。
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
