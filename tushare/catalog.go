package main

// 目录接口的运行时：参数装配 + 白名单。与其它增量流插件同一套形状，
// 差别只在上游怎么调（一个端点 + api_name，响应是列式的）。

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

// queryCatalog 按入参调一次 TuShare 并把列式结果还原成记录。
//
// 这里用反射，是因为两百多个操作的入参结构各不相同而装配逻辑完全一样。
// 范围限得很死：只读生成物里的字符串字段（生成器保证入参全是 string）。
//
// fields 是**生成期算好的全列清单**：TuShare 不传 fields 只回默认列，
// 而契约里声明了全部列——不显式要，画布上就会有一半字段永远是空的。
func queryCatalog[R any](ctx plugin.Ctx, apiName string, in any, fields string, alias map[string]string) ([]R, error) {
	params := map[string]string{}
	v := reflect.ValueOf(in)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	t := v.Type()
	for i := range t.NumField() {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("sokel"), ",")
		if name == "" {
			continue
		}
		f := v.Field(i)
		if f.Kind() != reflect.String {
			return nil, fmt.Errorf("入参 %s 不是字符串（生成器与运行时不一致）", name)
		}
		val := strings.TrimSpace(f.String())
		if val == "" {
			continue
		}
		if up, ok := alias[name]; ok {
			name = up
		}
		params[name] = val
	}

	data, err := clientOf(ctx).call(ctx, apiName, params, fields)
	if err != nil {
		return nil, err
	}
	return decodeRows[R](data)
}

// registerCatalog 按白名单注册目录接口，返回注册了几个。默认全部注册（TUSHARE_APIS=none 关掉）。
func registerCatalog(h plugin.Host, spec string) int {
	match := catalogMatcher(spec)
	n := 0
	for id, op := range catalogOps {
		if match(id, op.Category) {
			op.Register(h)
			n++
		}
	}
	return n
}

// catalogMatcher 解析 TUSHARE_APIS：
//
//	空 / *    全开（2026-09-17 起默认全开：用户要的是「TuShare 全部接口都能用」，按需收窄才是例外）
//	none      一个都不开
//	daily,trade_cal     按接口名精确匹配
//	行情数据            带 / 或含中文时按目录路径包含匹配（整段一起开）
func catalogMatcher(spec string) func(id, category string) bool {
	spec = strings.TrimSpace(spec)
	if spec == "" || spec == "*" {
		return func(string, string) bool { return true }
	}
	if strings.EqualFold(spec, "none") {
		return func(string, string) bool { return false }
	}
	names := map[string]bool{}
	var categories []string
	for _, raw := range strings.Split(spec, ",") {
		item := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(raw), "/*"))
		if item == "" {
			continue
		}
		if strings.Contains(item, "/") || !isASCII(item) {
			categories = append(categories, item)
			continue
		}
		names[strings.ToLower(item)] = true
	}
	return func(id, category string) bool {
		if names[id] {
			return true
		}
		for _, c := range categories {
			if strings.Contains(category, c) {
				return true
			}
		}
		return false
	}
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}
