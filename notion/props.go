package main

// 属性归一化：Notion 的 25 种属性值 ↔ 平铺的简单值。
//
// 读侧**两份都给**（props 归一化 + properties_raw 原样）：只给原样等于让人和模型对着
// `{"状态":{"status":{"name":"进行中"}}}` 猜路径；只给归一化，则 rollup/formula 这类
// 必然丢信息的类型没有退路。
//
// 写侧只收归一化 —— 但它需要**知道每一列是什么类型**（"进行中" 该包成 select 还是 status
// 还是 rich_text，光看值分辨不出来），所以写之前一定先取一次表结构（带缓存）。
// 这也是为什么写不了的列（formula/rollup/created_time…）能在发请求前就报出来：
// 表结构里写着它算出来的。

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

// —— 读：Notion 属性值 → 简单值 ——

// normalizeProps：整页属性 → 平铺值。
func normalizeProps(raw map[string]any) map[string]any {
	if raw == nil {
		return nil
	}
	out := make(map[string]any, len(raw))
	for name, v := range raw {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		out[name] = normalizeProp(m)
	}
	return out
}

// normalizeProp：一个属性值 → 简单值。认不出的类型返回 nil（原样那份里有）。
func normalizeProp(p map[string]any) any {
	switch str(p["type"]) {
	case "title":
		return plainText(p["title"])
	case "rich_text":
		return plainText(p["rich_text"])
	case "number":
		return p["number"]
	case "select":
		return nameOf(p["select"])
	case "status":
		return nameOf(p["status"])
	case "multi_select":
		return namesOf(p["multi_select"])
	case "date":
		// 形状恒定（start/end 两个键，无值即空串）而不是「有 end 才给对象」——
		// 变形状的字段下游没法写引用，模板里 {{props.截止.start}} 得先猜今天是哪种。
		return dateOf(p["date"])
	case "checkbox":
		return p["checkbox"]
	case "url", "email", "phone_number":
		return p[str(p["type"])]
	case "people":
		// 给名字不给 id：这一列绝大多数时候是拿去显示的。写回时两种都认（见 peopleValue）。
		return peopleNames(p["people"])
	case "files":
		return fileURLs(p["files"])
	case "relation":
		return relationIDs(p["relation"])
	case "created_time", "last_edited_time":
		return p[str(p["type"])]
	case "created_by", "last_edited_by":
		if u, ok := p[str(p["type"])].(map[string]any); ok {
			if n := str(u["name"]); n != "" {
				return n
			}
			return str(u["id"])
		}
		return nil
	case "unique_id":
		if u, ok := p["unique_id"].(map[string]any); ok {
			if pre := str(u["prefix"]); pre != "" {
				return fmt.Sprintf("%s-%v", pre, u["number"])
			}
			return u["number"]
		}
		return nil
	case "formula":
		return innerValue(p["formula"])
	case "rollup":
		if r, ok := p["rollup"].(map[string]any); ok {
			if arr, ok := r["array"].([]any); ok {
				out := make([]any, 0, len(arr))
				for _, it := range arr {
					if m, ok := it.(map[string]any); ok {
						out = append(out, normalizeProp(m))
					}
				}
				return out
			}
			return innerValue(r)
		}
		return nil
	}
	// verification / button 这类：归一化不出有意义的值，去 properties_raw 里取
	return nil
}

// innerValue：formula/rollup 这类「带 type 的单值包装」→ 里面那个值。
func innerValue(v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	switch t := str(m["type"]); t {
	case "date":
		return dateOf(m["date"])
	case "string", "number", "boolean":
		return m[t]
	default:
		return m[t]
	}
}

func plainText(v any) string {
	arr, ok := v.([]any)
	if !ok {
		return ""
	}
	var b strings.Builder
	for _, it := range arr {
		if m, ok := it.(map[string]any); ok {
			b.WriteString(str(m["plain_text"]))
		}
	}
	return b.String()
}

func nameOf(v any) any {
	if m, ok := v.(map[string]any); ok {
		return str(m["name"])
	}
	return nil
}

func namesOf(v any) []string {
	arr, _ := v.([]any)
	out := make([]string, 0, len(arr))
	for _, it := range arr {
		if m, ok := it.(map[string]any); ok {
			out = append(out, str(m["name"]))
		}
	}
	return out
}

func dateOf(v any) map[string]any {
	m, ok := v.(map[string]any)
	if !ok {
		return map[string]any{"start": "", "end": ""}
	}
	return map[string]any{"start": str(m["start"]), "end": str(m["end"])}
}

func peopleNames(v any) []string {
	arr, _ := v.([]any)
	out := make([]string, 0, len(arr))
	for _, it := range arr {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		if n := str(m["name"]); n != "" {
			out = append(out, n)
			continue
		}
		out = append(out, str(m["id"]))
	}
	return out
}

func fileURLs(v any) []string {
	arr, _ := v.([]any)
	out := make([]string, 0, len(arr))
	for _, it := range arr {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		if f, ok := m["file"].(map[string]any); ok {
			out = append(out, str(f["url"]))
			continue
		}
		if e, ok := m["external"].(map[string]any); ok {
			out = append(out, str(e["url"]))
		}
	}
	return out
}

func relationIDs(v any) []string {
	arr, _ := v.([]any)
	out := make([]string, 0, len(arr))
	for _, it := range arr {
		if m, ok := it.(map[string]any); ok {
			out = append(out, str(m["id"]))
		}
	}
	return out
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// —— 表结构（写入的前提，带缓存）——

type propType struct {
	Name    string
	Type    string
	Options []string
}

// writableTypes：能写的类型。不在表里的都是 Notion 算出来的，写了必被拒。
var writableTypes = map[string]bool{
	"title": true, "rich_text": true, "number": true, "select": true, "status": true,
	"multi_select": true, "date": true, "people": true, "files": true, "checkbox": true,
	"url": true, "email": true, "phone_number": true, "relation": true,
}

// dsCacheEntry：一个数据源的对象与解析好的列。
type dsCacheEntry struct {
	ds    dataSource
	props map[string]propType
	at    time.Time
}

var (
	schemaMu    sync.Mutex
	schemaCache = map[string]dsCacheEntry{}
	// schemaTTL：表结构不常改，但改了要能在一次编辑周期内生效。
	// 缓存不是为了省钱，是为了别让一次操作里同一个对象被拉三遍（限流只有 3 次/秒）：
	// 「认出这是不是数据源」「取表结构」「拼属性」问的都是同一个接口。
	schemaTTL = 2 * time.Minute
)

// getDataSource：取数据源（带缓存）。
func getDataSource(ctx plugin.Ctx, dsID string) (dataSource, map[string]propType, error) {
	schemaMu.Lock()
	if e, ok := schemaCache[dsID]; ok && time.Since(e.at) < schemaTTL {
		schemaMu.Unlock()
		return e.ds, e.props, nil
	}
	schemaMu.Unlock()

	var ds dataSource
	if err := callAPI(ctx, reqOpts{method: "GET", path: "/data_sources/" + dsID}, &ds); err != nil {
		return dataSource{}, nil, err
	}
	props := parseProps(ds.Properties)
	schemaMu.Lock()
	schemaCache[dsID] = dsCacheEntry{ds: ds, props: props, at: time.Now()}
	schemaMu.Unlock()
	return ds, props, nil
}

// invalidateDataSource：改过表结构后立刻作废，否则接着写入会按旧列报「没有这一列」。
func invalidateDataSource(dsID string) {
	schemaMu.Lock()
	delete(schemaCache, dsID)
	schemaMu.Unlock()
}

// dataSourceProps：取数据源的列（带缓存）。
func dataSourceProps(ctx plugin.Ctx, dsID string) (map[string]propType, error) {
	_, props, err := getDataSource(ctx, dsID)
	return props, err
}

func parseProps(raw map[string]any) map[string]propType {
	out := make(map[string]propType, len(raw))
	for name, v := range raw {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		t := str(m["type"])
		pt := propType{Name: name, Type: t}
		if cfg, ok := m[t].(map[string]any); ok {
			if opts, ok := cfg["options"].([]any); ok {
				for _, o := range opts {
					if om, ok := o.(map[string]any); ok {
						pt.Options = append(pt.Options, str(om["name"]))
					}
				}
			}
		}
		out[name] = pt
	}
	return out
}

// —— 写：简单值 → Notion 属性值 ——

// buildProps：平铺属性 → Notion 的属性 JSON。
//
// 列名不认识就**当场报错并列出有哪些列**——这个错是给模型看的，一句
// 「property does not exist」它只会再猜一次，给了列名它下一轮就对了。
func buildProps(ctx plugin.Ctx, dsID string, in map[string]any) (map[string]any, error) {
	if len(in) == 0 {
		return nil, nil
	}
	specs, err := dataSourceProps(ctx, dsID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]any, len(in))
	for name, v := range in {
		spec, ok := specs[name]
		if !ok {
			return nil, fmt.Errorf("数据源里没有「%s」这一列；现有的列：%s", name, columnList(specs))
		}
		if !writableTypes[spec.Type] {
			return nil, fmt.Errorf("「%s」是 %s 类型，由 Notion 自己算出来，写不了", name, spec.Type)
		}
		val, err := propValue(ctx, spec, v)
		if err != nil {
			return nil, fmt.Errorf("列「%s」: %w", name, err)
		}
		out[name] = val
	}
	return out, nil
}

func columnList(specs map[string]propType) string {
	names := make([]string, 0, len(specs))
	for n, s := range specs {
		if writableTypes[s.Type] {
			names = append(names, fmt.Sprintf("%s(%s)", n, s.Type))
		}
	}
	sort.Strings(names)
	return strings.Join(names, "、")
}

// propValue：一个值 → 该列类型的 Notion 表示。
func propValue(ctx plugin.Ctx, spec propType, v any) (map[string]any, error) {
	// 显式 null = 清空这一列。各类型的「空」形状不同，统一在这里给。
	if v == nil {
		return clearValue(spec.Type), nil
	}
	switch spec.Type {
	case "title":
		return map[string]any{"title": richText(toStr(v))}, nil
	case "rich_text":
		return map[string]any{"rich_text": richText(toStr(v))}, nil
	case "number":
		f, err := toNumber(v)
		if err != nil {
			return nil, err
		}
		return map[string]any{"number": f}, nil
	case "checkbox":
		b, ok := v.(bool)
		if !ok {
			s := strings.ToLower(strings.TrimSpace(toStr(v)))
			b = s == "true" || s == "1" || s == "yes" || s == "是"
		}
		return map[string]any{"checkbox": b}, nil
	case "select", "status":
		name := toStr(v)
		if name == "" {
			return clearValue(spec.Type), nil
		}
		// 候选值不匹配：select 会被 Notion 自动新建一个选项，而 status **一定报错**。
		// 与其让它去撞，不如在这里说清有哪些候选。
		if len(spec.Options) > 0 && !contains(spec.Options, name) {
			return nil, fmt.Errorf("「%s」不在候选值里；可选：%s", name, strings.Join(spec.Options, "、"))
		}
		return map[string]any{spec.Type: map[string]any{"name": name}}, nil
	case "multi_select":
		names := toStrings(v)
		items := make([]any, 0, len(names))
		for _, n := range names {
			if len(spec.Options) > 0 && !contains(spec.Options, n) {
				return nil, fmt.Errorf("「%s」不在候选值里；可选：%s", n, strings.Join(spec.Options, "、"))
			}
			items = append(items, map[string]any{"name": n})
		}
		return map[string]any{"multi_select": items}, nil
	case "date":
		return dateValue(v)
	case "url", "email", "phone_number":
		s := toStr(v)
		if s == "" {
			return clearValue(spec.Type), nil
		}
		return map[string]any{spec.Type: s}, nil
	case "people":
		items, err := peopleValue(ctx, toStrings(v))
		if err != nil {
			return nil, err
		}
		return map[string]any{"people": items}, nil
	case "relation":
		ids := toStrings(v)
		items := make([]any, 0, len(ids))
		for _, id := range ids {
			items = append(items, map[string]any{"id": notionID(id)}) // 贴链接也认
		}
		return map[string]any{"relation": items}, nil
	case "files":
		urls := toStrings(v)
		items := make([]any, 0, len(urls))
		for _, u := range urls {
			name := u
			if i := strings.LastIndex(strings.TrimRight(u, "/"), "/"); i >= 0 {
				name = u[i+1:]
			}
			items = append(items, map[string]any{"name": name, "external": map[string]any{"url": u}})
		}
		return map[string]any{"files": items}, nil
	}
	return nil, fmt.Errorf("不支持写入 %s 类型", spec.Type)
}

// clearValue：各类型的「空」。
func clearValue(t string) map[string]any {
	switch t {
	case "title":
		return map[string]any{"title": []any{}}
	case "rich_text":
		return map[string]any{"rich_text": []any{}}
	case "multi_select":
		return map[string]any{"multi_select": []any{}}
	case "people":
		return map[string]any{"people": []any{}}
	case "relation":
		return map[string]any{"relation": []any{}}
	case "files":
		return map[string]any{"files": []any{}}
	case "checkbox":
		return map[string]any{"checkbox": false}
	}
	return map[string]any{t: nil}
}

// richText：切成 2000 字一段。Notion 单个富文本对象上限 2000 字符，超了整条请求被拒——
// 而「写长文到某一列」恰恰是最常见的用法（模型的产出动辄几千字）。
func richText(s string) []any {
	if s == "" {
		return []any{}
	}
	const maxRun = 2000
	r := []rune(s)
	out := make([]any, 0, len(r)/maxRun+1)
	for i := 0; i < len(r); i += maxRun {
		j := min(i+maxRun, len(r))
		out = append(out, map[string]any{"type": "text", "text": map[string]any{"content": string(r[i:j])}})
	}
	return out
}

// dateValue：日期。三种写法都认——字符串（"2026-08-20"）、{start,end}、以及读出来的那份原样回写。
func dateValue(v any) (map[string]any, error) {
	switch t := v.(type) {
	case string:
		if strings.TrimSpace(t) == "" {
			return map[string]any{"date": nil}, nil
		}
		return map[string]any{"date": map[string]any{"start": t}}, nil
	case map[string]any:
		start := str(t["start"])
		if start == "" {
			return map[string]any{"date": nil}, nil
		}
		d := map[string]any{"start": start}
		if e := str(t["end"]); e != "" {
			d["end"] = e
		}
		return map[string]any{"date": d}, nil
	}
	return nil, fmt.Errorf("日期要写成 \"2026-08-20\"（或带时间的 ISO8601），或 {\"start\":…,\"end\":…}")
}

// —— 成员：名字 ↔ id ——

var (
	userMu    sync.Mutex
	userCache = map[string]cachedUsers{}
)

type cachedUsers struct {
	byName map[string]string
	at     time.Time
}

// peopleValue：people 列的值。**名字和 id 都认**——读出来的是名字（那一列多半拿去显示），
// 直接把读到的值写回另一页是最自然的用法，不认名字的话这条路就是断的。
func peopleValue(ctx plugin.Ctx, vals []string) ([]any, error) {
	items := make([]any, 0, len(vals))
	var names []string
	for _, v := range vals {
		if id := notionID(v); len(id) == 32 {
			items = append(items, map[string]any{"object": "user", "id": id})
			continue
		}
		names = append(names, v)
	}
	if len(names) == 0 {
		return items, nil
	}
	byName, err := usersByName(ctx)
	if err != nil {
		return nil, err
	}
	for _, n := range names {
		id, ok := byName[n]
		if !ok {
			return nil, fmt.Errorf("工作空间里没有叫「%s」的成员（可用「列成员」看有谁，或直接给用户 id）", n)
		}
		items = append(items, map[string]any{"object": "user", "id": id})
	}
	return items, nil
}

// usersByName：名字 → id（按 token 缓存 10 分钟）。
func usersByName(ctx plugin.Ctx) (map[string]string, error) {
	tok, err := authToken(sokel.CredentialAs[Cred](ctx))
	if err != nil {
		return nil, err
	}
	userMu.Lock()
	if c, ok := userCache[tok]; ok && time.Since(c.at) < 10*time.Minute {
		userMu.Unlock()
		return c.byName, nil
	}
	userMu.Unlock()

	users, err := listAllUsers(ctx, 200)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]string, len(users))
	for _, u := range users {
		if u.Name != "" {
			byName[u.Name] = u.ID
		}
	}
	userMu.Lock()
	userCache[tok] = cachedUsers{byName: byName, at: time.Now()}
	userMu.Unlock()
	return byName, nil
}

// —— 小工具 ——

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func toStr(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return ""
	case float64:
		return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%f", t), "0"), ".")
	}
	return fmt.Sprintf("%v", v)
}

func toStrings(v any) []string {
	switch t := v.(type) {
	case nil:
		return nil
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, it := range t {
			if s := toStr(it); s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		// 单值也认：模型经常给 "A" 而不是 ["A"]，为此报错纯属添堵。
		// 逗号分隔同理——填表的人就是这么写的。
		var out []string
		for _, p := range strings.Split(t, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	return []string{toStr(v)}
}

func toNumber(v any) (float64, error) {
	switch t := v.(type) {
	case float64:
		return t, nil
	case int:
		return float64(t), nil
	case string:
		var f float64
		if _, err := fmt.Sscanf(strings.TrimSpace(t), "%g", &f); err != nil {
			return 0, fmt.Errorf("「%s」不是数字", t)
		}
		return f, nil
	}
	return 0, fmt.Errorf("不是数字")
}
