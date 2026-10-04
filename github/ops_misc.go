package main

// 搜索 / 保底直调 / 健康检查。

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/sokel-dev/sokel-official-plugins/github/schema"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

func opSearch(ctx plugin.Ctx, in *SearchIn) (*SearchOut, error) {
	kind := orDefault(in.Type, "issues")
	limit := in.Limit
	if limit <= 0 {
		limit = 30
	}
	if limit > 100 {
		limit = 100
	}
	raw, _, err := ghCall(ctx, http.MethodGet, "/search/"+kind, map[string]any{
		"q": in.Query, "per_page": limit,
	})
	if err != nil {
		return nil, err
	}
	m := digObj(raw)
	out := &SearchOut{
		Total: num(m, "total_count"),
		// incomplete_results = GitHub 搜索超时了，只返回了一部分。
		// 不透出的话，调用方会把「部分结果」当成「全部结果」——静默的漏。
		Incomplete: boolean(m, "incomplete_results"),
	}
	for _, it := range arr(m, "items") {
		h, _ := it.(map[string]any)
		if h == nil {
			continue
		}
		hit := schema.SearchHit{
			Title:  firstNonEmpty(str(h, "title"), str(h, "name"), str(h, "full_name")),
			Number: num(h, "number"), State: str(h, "state"),
			Author: nested(h, "user", "login"), Path: str(h, "path"),
			URL: str(h, "html_url"),
		}
		if hit.Repo = str(obj(h, "repository"), "full_name"); hit.Repo == "" {
			hit.Repo = repoFromIssueURL(str(h, "html_url"))
		}
		hit.Snippet = clip(firstNonEmpty(str(h, "body"), str(h, "description")), 500)
		out.Results = append(out.Results, hit)
	}
	out.Count = len(out.Results)
	return out, nil
}

// repoFromIssueURL 从 https://github.com/o/r/issues/1 反推 o/r。
// Issue 搜索的结果里没有 repository 字段（只有 repository_url），这是最省事的还原。
func repoFromIssueURL(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return ""
	}
	seg := strings.Split(strings.Trim(p.Path, "/"), "/")
	if len(seg) >= 2 {
		return seg[0] + "/" + seg[1]
	}
	return ""
}

func opCall(ctx plugin.Ctx, in *CallIn) (*CallOut, error) {
	path := strings.TrimSpace(in.Path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	// 有人会把完整地址粘进来——截掉前缀比报错友好，且不会有歧义。
	for _, pfx := range []string{"https://api.github.com", "http://api.github.com"} {
		path = strings.TrimPrefix(path, pfx)
	}
	method := orDefault(in.Method, http.MethodGet)
	var params map[string]any
	if method == http.MethodGet || method == http.MethodDelete {
		params = map[string]any{}
		for k, v := range in.Query {
			params[k] = v
		}
	} else if in.Body != nil {
		if m, ok := in.Body.(map[string]any); ok {
			params = m
		} else {
			return nil, fmt.Errorf("请求体要是一个 JSON 对象，当前是 %T", in.Body)
		}
	}
	raw, h, err := ghCall(ctx, method, path, params)
	if err != nil {
		return nil, err
	}
	out := &CallOut{Status: 200, HasMore: hasNext(h)}
	if len(raw) == 0 {
		return out, nil
	}
	if list := digList(raw); list != nil {
		items := make([]any, 0, len(list))
		for _, m := range list {
			items = append(items, m)
		}
		out.Data = items
		return out, nil
	}
	if m := digObj(raw); m != nil {
		out.Data = m
		return out, nil
	}
	out.Data = string(raw)
	return out, nil
}

// opHealthCheck 凭证页那个「测试」按钮调的就是它。
//
// **凭证不可用要回 ok=false，不能抛错**：抛错的话平台只能说「调用失败」，
// 分不出是令牌没填、过期，还是网络不通——而这三者的下一步完全不同。
func opHealthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	cred := credOf(ctx)
	if strings.TrimSpace(cred.Token) == "" {
		return &HealthCheckOut{OK: false, Message: "凭证里没有访问令牌"}, nil
	}
	raw, h, err := ghCall(ctx, http.MethodGet, "/user", nil)
	if err != nil {
		return &HealthCheckOut{OK: false, Message: err.Error()}, nil
	}
	login := str(digObj(raw), "login")
	out := &HealthCheckOut{OK: true, Login: login}
	if v := strings.TrimSpace(h.Get("X-OAuth-Scopes")); v != "" {
		for _, s := range strings.Split(v, ",") {
			if s = strings.TrimSpace(s); s != "" {
				out.Scopes = append(out.Scopes, s)
			}
		}
	}
	if v := strings.TrimSpace(h.Get("X-RateLimit-Remaining")); v != "" {
		out.RateRemaining, _ = strconv.Atoi(v)
	}
	where := "github.com"
	if b := strings.TrimSpace(cred.BaseURL); b != "" {
		where = b
	}
	out.Message = fmt.Sprintf("%s 上以 %s 的身份连通", where, login)
	if len(out.Scopes) == 0 {
		// 细粒度令牌不回 X-OAuth-Scopes——不说明的话会被当成「令牌没权限」。
		out.Message += "（细粒度令牌不回报 scope，这不代表没权限）"
	}
	return out, nil
}

// urlPathEscape 转义单个路径片段。
func urlPathEscape(s string) string { return url.PathEscape(s) }
