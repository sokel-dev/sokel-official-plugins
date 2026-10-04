// gen-schema：把 catalog/tushare-apis.json 里的接口生成成契约声明与注册表。
//
//	catalog/tushare-apis.json     （cmd/catalog 抓的，进版本库）
//	  ↓ 本工具
//	schema/gen_apis_NN.go         每个接口一个 Schema 类型 + 一个记录类型
//	gen_catalog.go                接口名 → 注册函数 的表（主包）
//	  ↓ sokel-gen
//	zz_types.go / zz_register.go  In/Out 与 OnXxx
//
// 全部生成、按需激活：注册与否由 TUSHARE_APIS 决定（见 README）。
//
//	go run ./cmd/gen-schema
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

const apisPerFile = 60

func main() {
	in := flag.String("catalog", "catalog/tushare-apis.json", "接口规格 JSON")
	dir := flag.String("dir", ".", "插件根目录")
	flag.Parse()

	if err := run(*in, *dir); err != nil {
		fmt.Fprintln(os.Stderr, "gen-schema:", err)
		os.Exit(1)
	}
}

// API / Param 与 cmd/catalog 的输出一一对应，刻意各写一份：
// 中间那份 JSON 才是两道工序的接口。
type API struct {
	APIName  string  `json:"api_name"`
	Title    string  `json:"title"`
	DocID    int     `json:"doc_id"`
	Category string  `json:"category"`
	Describe string  `json:"describe"`
	Inputs   []Param `json:"inputs"`
	Outputs  []Param `json:"outputs"`
}

type Param struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Desc        string `json:"desc"`
	DefaultShow bool   `json:"default_show"`
}

type gen struct {
	api        API
	opID       string
	schemaType string
	recordType string
	handler    string
	fields     string // 请求里显式要的全列清单
	inputs     []genParam
	outputs    []genField
}

type genParam struct {
	sokelName, upstream, label, desc string
	required                         bool
}

type genField struct {
	goName, sokelName, jsonKey, goType, label, desc string
}

func run(catalogPath, dir string) error {
	b, err := os.ReadFile(catalogPath)
	if err != nil {
		return err
	}
	var apis []API
	if err := json.Unmarshal(b, &apis); err != nil {
		return err
	}

	merged, notes := mergeByName(apis)
	for _, n := range notes {
		fmt.Fprintln(os.Stderr, "  注意:", n)
	}

	gens, err := plan(merged)
	if err != nil {
		return err
	}
	if err := writeSchemas(dir, gens); err != nil {
		return err
	}
	if err := writeCatalogTable(dir, gens); err != nil {
		return err
	}
	fmt.Printf("gen-schema: 已生成 %d 个接口的契约\n", len(gens))
	return nil
}

// mergeByName 同一个 api_name 出现在多个文档页时合并成一个操作。
//
// TuShare 确实有这种情况：stk_mins 同时挂在「股票历史分钟」与「ETF历史分钟」两页，
// 是同一个接口的两种用法。但也有 index_daily 这种——两页字段未必一致。
// 故合并时**对比字段集**，不一致就打印出来让人看见，而不是悄悄取其一。
func mergeByName(apis []API) ([]API, []string) {
	byName := map[string]*API{}
	var order []string
	var notes []string
	for _, a := range apis {
		if a.APIName == "" {
			continue
		}
		key := strings.ToLower(a.APIName)
		prev, ok := byName[key]
		if !ok {
			cp := a
			byName[key] = &cp
			order = append(order, key)
			continue
		}
		if names(prev.Outputs) != names(a.Outputs) {
			notes = append(notes, fmt.Sprintf(
				"%s 在 doc_id=%d 与 %d 的出参不一致（%d vs %d 列），取列多的那个",
				a.APIName, prev.DocID, a.DocID, len(prev.Outputs), len(a.Outputs)))
			if len(a.Outputs) > len(prev.Outputs) {
				cat, title := prev.Category, prev.Title
				*prev = a
				prev.Category, prev.Title = cat, title
			}
		}
		prev.Category += "; " + a.Category
	}
	out := make([]API, 0, len(order))
	for _, k := range order {
		out = append(out, *byName[k])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].APIName < out[j].APIName })
	return out, notes
}

func names(ps []Param) string {
	var b strings.Builder
	for _, p := range ps {
		b.WriteString(p.Name)
		b.WriteByte(',')
	}
	return b.String()
}

func plan(apis []API) ([]gen, error) {
	var out []gen
	seenType := map[string]string{}
	for _, api := range apis {
		if len(api.Outputs) == 0 {
			continue
		}
		base := camel(api.APIName)
		g := gen{
			api: api, opID: strings.ToLower(api.APIName),
			schemaType: "Api" + base, recordType: base + "Record", handler: camel(strings.ToLower(api.APIName)),
		}
		if prev, ok := seenType[g.schemaType]; ok {
			return nil, fmt.Errorf("类型名撞车: %s 与 %s 都算出 %q", prev, api.APIName, g.schemaType)
		}
		seenType[g.schemaType] = api.APIName

		g.inputs = planInputs(api.Inputs)
		g.outputs = planOutputs(api.Outputs)
		if len(g.outputs) == 0 {
			continue
		}
		cols := make([]string, 0, len(g.outputs))
		for _, f := range g.outputs {
			cols = append(cols, f.jsonKey)
		}
		g.fields = strings.Join(cols, ",")
		out = append(out, g)
	}
	return out, nil
}

// planInputs 入参一律声明为字符串：生成的入参结构里数值是值类型，
// 「没填」与「填了 0」会得到同一个 Go 零值，而那两件事对上游是不同的请求。
// 原始类型写进说明，用户看得见。
func planInputs(params []Param) []genParam {
	var out []genParam
	seen := map[string]bool{}
	for _, p := range params {
		sokelName := sanitize(p.Name)
		if sokelName == "" || seen[sokelName] {
			continue
		}
		seen[sokelName] = true
		desc := p.Desc
		if p.Type != "" && p.Type != "str" {
			desc = strings.TrimSpace(desc + "（类型 " + p.Type + "）")
		}
		out = append(out, genParam{
			sokelName: sokelName, upstream: p.Name,
			label: firstNonEmpty(p.Desc, p.Name), desc: desc, required: p.Required,
		})
	}
	return out
}

func planOutputs(params []Param) []genField {
	var out []genField
	seen := map[string]bool{}
	for _, p := range params {
		sokelName := strings.ToLower(sanitize(p.Name))
		goName := exportIdent(p.Name)
		if sokelName == "" || goName == "" || seen[sokelName] {
			continue
		}
		seen[sokelName] = true
		out = append(out, genField{
			goName: goName, sokelName: sokelName, jsonKey: tagSafe(p.Name),
			goType: goTypeOf(p.Type), label: firstNonEmpty(tagSafe(p.Desc), p.Name), desc: tagSafe(p.Desc),
		})
	}
	return out
}

// goTypeOf TuShare 文档里的类型词汇：str / int / float / datetime …
func goTypeOf(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "int", "integer", "bigint", "long":
		return "int64"
	case "float", "double", "number", "decimal":
		return "float64"
	default:
		return "string"
	}
}

// ===== 渲染 =====

func writeSchemas(dir string, gens []gen) error {
	schemaDir := filepath.Join(dir, "schema")
	old, _ := filepath.Glob(filepath.Join(schemaDir, "gen_apis_*.go"))
	for _, f := range old {
		if err := os.Remove(f); err != nil {
			return err
		}
	}
	for start := 0; start < len(gens); start += apisPerFile {
		end := min(start+apisPerFile, len(gens))
		var b strings.Builder
		fmt.Fprintf(&b, `// Code generated by gen-schema. DO NOT EDIT.
//
// TuShare 接口 %d–%d 的契约。改这里没用——改 catalog/tushare-apis.json 或生成器，
// 然后 go run ./cmd/gen-schema && go generate ./...

package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)
`, start+1, end)
		for _, g := range gens[start:end] {
			renderSchema(&b, g)
		}
		if err := writeGo(filepath.Join(schemaDir, fmt.Sprintf("gen_apis_%02d.go", start/apisPerFile+1)), b.String()); err != nil {
			return err
		}
	}
	return nil
}

func renderSchema(b *strings.Builder, g gen) {
	label := firstNonEmpty(g.api.Title, g.api.APIName)

	fmt.Fprintf(b, "\n// %s %s（%s）\n", g.schemaType, label, g.api.APIName)
	if g.api.Category != "" {
		fmt.Fprintf(b, "// 目录：%s\n", g.api.Category)
	}
	fmt.Fprintf(b, "type %s struct{}\n\n", g.schemaType)
	fmt.Fprintf(b, "func (%s) Meta() contract.Meta {\n\treturn contract.Meta{ID: %q, Label: %q, Desc: %q, TimeoutSec: 90}\n}\n\n",
		g.schemaType, g.opID, label, oneLine(g.api.Describe))

	fmt.Fprintf(b, "func (%s) Inputs() []contract.FieldSpec {\n\treturn []contract.FieldSpec{\n", g.schemaType)
	for _, p := range g.inputs {
		fmt.Fprintf(b, "\t\tfield.String(%q).Label(%q)", p.sokelName, oneLine(p.label))
		if p.desc != "" {
			fmt.Fprintf(b, ".Desc(%q)", oneLine(p.desc))
		}
		if !p.required {
			b.WriteString(".Optional()")
		}
		b.WriteString(",\n")
	}
	b.WriteString("\t}\n}\n\n")

	fmt.Fprintf(b, "func (%s) Outputs() []contract.FieldSpec {\n\treturn []contract.FieldSpec{\n", g.schemaType)
	fmt.Fprintf(b, "\t\tfield.Array(\"items\", []%s{}).Label(%q),\n", g.recordType, label)
	b.WriteString("\t\tfield.Int(\"count\").Label(\"本批条数\"),\n\t}\n}\n\n")

	fmt.Fprintf(b, "// %s %s 的一条记录。\n", g.recordType, label)
	fmt.Fprintf(b, "type %s struct {\n", g.recordType)
	for _, f := range g.outputs {
		fmt.Fprintf(b, "\t%s %s `json:%q sokel:%q label:%q", f.goName, f.goType, f.jsonKey, f.sokelName, f.label)
		if d := oneLine(f.desc); d != "" && d != f.label {
			fmt.Fprintf(b, " desc:%q", d)
		}
		b.WriteString("`\n")
	}
	b.WriteString("}\n")
}

func writeCatalogTable(dir string, gens []gen) error {
	var b strings.Builder
	b.WriteString(`// Code generated by gen-schema. DO NOT EDIT.
//
// 接口名 → 注册函数。全部生成、按需激活：main() 只注册 TUSHARE_APIS 选中的那些。

package main

import (
	"github.com/sokel-dev/sokel-official-plugins/tushare/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

// catalogOp 一个目录接口的注册入口与它所属的目录（白名单按目录匹配时用）。
type catalogOp struct {
	Category string
	Register func(plugin.Host)
}

var catalogOps = map[string]catalogOp{
`)
	for _, g := range gens {
		fmt.Fprintf(&b, "\t%q: {Category: %q, Register: func(h plugin.Host) {\n", g.opID, g.api.Category)
		fmt.Fprintf(&b, "\t\tOn%s(h, func(ctx plugin.Ctx, in *%sIn) (*%sOut, error) {\n", g.handler, g.handler, g.handler)
		fmt.Fprintf(&b, "\t\t\trecs, err := queryCatalog[schema.%s](ctx, %q, in, %q, %s)\n",
			g.recordType, g.api.APIName, g.fields, aliasLiteral(g.inputs))
		b.WriteString("\t\t\tif err != nil {\n\t\t\t\treturn nil, err\n\t\t\t}\n")
		fmt.Fprintf(&b, "\t\t\treturn &%sOut{Items: recs, Count: len(recs)}, nil\n", g.handler)
		b.WriteString("\t\t})\n\t}},\n")
	}
	b.WriteString("}\n")
	return writeGo(filepath.Join(dir, "gen_catalog.go"), b.String())
}

func writeGo(path, src string) error {
	formatted, err := format.Source([]byte(src))
	if err != nil {
		// 生成了非法 Go：原样落盘，好让人打开看到底哪儿错了。
		_ = os.WriteFile(path, []byte(src), 0o644)
		return fmt.Errorf("%s 生成了非法 Go 代码: %w", path, err)
	}
	return os.WriteFile(path, formatted, 0o644)
}

func aliasLiteral(params []genParam) string {
	var pairs []string
	for _, p := range params {
		if p.sokelName != p.upstream {
			pairs = append(pairs, fmt.Sprintf("%q: %q", p.sokelName, p.upstream))
		}
	}
	if len(pairs) == 0 {
		return "nil"
	}
	return "map[string]string{" + strings.Join(pairs, ", ") + "}"
}

// ===== 名字处理 =====

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if out != "" && unicode.IsDigit(rune(out[0])) {
		out = "f" + out
	}
	return out
}

func camel(s string) string {
	var b strings.Builder
	upper := true
	for _, r := range s {
		switch {
		case r == '_' || r == '-' || r == '.':
			upper = true
		case upper:
			b.WriteRune(unicode.ToUpper(r))
			upper = false
		default:
			b.WriteRune(r)
		}
	}
	return sanitize(b.String())
}

func exportIdent(s string) string {
	s = sanitize(s)
	if s == "" {
		return ""
	}
	return string(unicode.ToUpper(rune(s[0]))) + s[1:]
}

// tagSafe 结构体标签是反引号原始字符串，里面出不了引号也出不了反引号。
func tagSafe(s string) string {
	return strings.NewReplacer(`"`, "", "`", "", `\`, "").Replace(s)
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
