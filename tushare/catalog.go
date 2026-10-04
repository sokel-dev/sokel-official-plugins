package main

// Runtime for catalog endpoints: parameter assembly + allowlist. Same shape as the other
// incremental-stream plugins; the only difference is how the upstream is called (one endpoint
// plus api_name, with a columnar response).

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

// queryCatalog calls TuShare once with the given inputs and restores the columnar result into records.
//
// It uses reflection because the input structs differ across the 200+ operations while the
// assembly logic is exactly the same. The scope is kept narrow: it only reads string fields off
// the generated types (the generator guarantees every input is a string).
//
// fields is **the full column list computed at generation time**: TuShare only returns the default
// columns unless fields is passed, while the contract declares every column — without asking for
// them explicitly, half the fields would always be empty on the canvas.
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

// registerCatalog registers catalog endpoints per the allowlist and returns how many got registered.
// Everything is registered by default (set TUSHARE_APIS=none to turn it off).
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

// catalogMatcher parses TUSHARE_APIS:
//
//	empty / *           everything on (default on since 2026-09-17: the user wants "every TuShare
//	                     endpoint usable", narrowing it down on demand is the exception)
//	none                nothing on
//	daily,trade_cal      exact match by endpoint name
//	行情数据             when it contains "/" or Chinese characters, matches by category path
//	                     substring (turns on the whole group at once)
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
