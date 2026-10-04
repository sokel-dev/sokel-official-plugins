package main

// Property normalization: Notion's 25 property value types <-> flat simple values.
//
// The read side **gives both** (normalized props + raw properties_raw): giving only the raw form
// means people and models have to guess paths against `{"状态":{"status":{"name":"进行中"}}}`;
// giving only the normalized form leaves no fallback for types like rollup/formula where
// normalization inevitably loses information.
//
// The write side only accepts normalized values — but it needs to **know each column's type**
// ("进行中" could need wrapping as select, status, or rich_text, and the value alone can't tell you
// which), so it always fetches the schema first (cached) before writing. This is also why an
// unwritable column (formula/rollup/created_time...) can be reported before the request is even
// sent: the schema already says it's computed.

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

// —— Read: Notion property value -> simple value ——

// normalizeProps converts a whole page's properties to flat values.
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

// normalizeProp converts one property value to a simple value. Unrecognized types return nil
// (it's available in the raw copy).
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
		// A constant shape (always a start/end pair, empty string if unset) rather than "only
		// give an object when there's an end" — a field whose shape changes can't be referenced
		// downstream; a template with {{props.截止.start}} would first have to guess which shape
		// it is today.
		return dateOf(p["date"])
	case "checkbox":
		return p["checkbox"]
	case "url", "email", "phone_number":
		return p[str(p["type"])]
	case "people":
		// Gives the name, not the id: this column is used for display the vast majority of the
		// time. Writing back accepts either form (see peopleValue).
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
	// Types like verification / button: normalization yields nothing meaningful; fetch from
	// properties_raw instead
	return nil
}

// innerValue unwraps a "type-tagged single-value wrapper" (as used by formula/rollup) to the
// value inside.
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

// —— Schema (a prerequisite for writing, cached) ——

type propType struct {
	Name    string
	Type    string
	Options []string
}

// writableTypes lists the types that can be written. Anything not in here is computed by Notion,
// and writing to it is always rejected.
var writableTypes = map[string]bool{
	"title": true, "rich_text": true, "number": true, "select": true, "status": true,
	"multi_select": true, "date": true, "people": true, "files": true, "checkbox": true,
	"url": true, "email": true, "phone_number": true, "relation": true,
}

// dsCacheEntry holds one data source's object plus its parsed columns.
type dsCacheEntry struct {
	ds    dataSource
	props map[string]propType
	at    time.Time
}

var (
	schemaMu    sync.Mutex
	schemaCache = map[string]dsCacheEntry{}
	// schemaTTL: a schema doesn't change often, but when it does the change needs to take effect
	// within one editing cycle. The cache isn't about saving money — it's to avoid fetching the
	// same object three times within a single operation (the rate limit is only 3 req/sec):
	// "is this a data source", "get its schema", and "assemble properties" all hit the same
	// endpoint.
	schemaTTL = 2 * time.Minute
)

// getDataSource fetches a data source (cached).
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

// invalidateDataSource evicts the cache entry right after a schema change; otherwise a subsequent
// write would report "no such column" against the stale columns.
func invalidateDataSource(dsID string) {
	schemaMu.Lock()
	delete(schemaCache, dsID)
	schemaMu.Unlock()
}

// dataSourceProps fetches a data source's columns (cached).
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

// —— Write: simple value -> Notion property value ——

// buildProps converts flat properties to Notion's property JSON.
//
// An unrecognized column name **errors immediately and lists the available columns** — this error
// is meant for the model to read. A bare "property does not exist" would only make it guess again,
// but given the column names it gets it right on the next try.
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

// propValue converts one value to that column type's Notion representation.
func propValue(ctx plugin.Ctx, spec propType, v any) (map[string]any, error) {
	// Explicit null = clear this column. Each type's "empty" shape differs, given uniformly here.
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
		// A value not among the candidates: Notion auto-creates a new option for select, but
		// status **always errors**. Better to spell out the candidates here than let it hit that wall.
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
			items = append(items, map[string]any{"id": notionID(id)}) // A pasted link is accepted too
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

// clearValue is each type's "empty" representation.
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

// richText splits text into 2000-character chunks. A single rich-text object caps out at 2000
// characters; go over and the whole request is rejected — and "write long-form text into a column"
// is exactly the most common use case (model output easily runs to thousands of characters).
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

// dateValue handles a date. All three forms are accepted — a string ("2026-08-20"), {start,end},
// and a previously-read value written straight back.
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

// —— Members: name <-> id ——

var (
	userMu    sync.Mutex
	userCache = map[string]cachedUsers{}
)

type cachedUsers struct {
	byName map[string]string
	at     time.Time
}

// peopleValue builds a people column's value. **Both name and id are accepted** — what's read back
// is the name (that column is mostly used for display), and writing a read value straight back to
// another page is the most natural usage; refusing names would break that path.
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

// usersByName maps name -> id (cached per token for 10 minutes).
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

// —— Small helpers ——

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
		// A single value is accepted too: models often give "A" instead of ["A"], and erroring
		// over this would just be friction. Same reasoning for comma-separated — that's just how
		// people fill in forms.
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
