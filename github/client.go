package main

// GitHub 调用层：REST v3 + GraphQL v4。纯 HTTP，无 SDK 可省。
//
// 两条传输并存的理由见 schema/projects.go 顶注（Projects V2 只有 GraphQL）。
// github.com 与 GitHub Enterprise Server 的差别只在 base：
//
//	github.com  →  https://api.github.com          GraphQL: https://api.github.com/graphql
//	GHES        →  https://host/api/v3             GraphQL: https://host/api/graphql
//
// 注意 GHES 的 GraphQL 端点是 /api/graphql 而**不是** /api/v3/graphql——照着 REST 的
// 形状拼会得到 404，而 404 在这里看起来像「你没权限」，很难往「端点拼错了」上想。

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-official-plugins/github/schema"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

func credOf(ctx plugin.Ctx) Cred {
	var c Cred
	sokel.BindCredential(ctx, &c)
	return c
}

// restBase REST 根地址。
func restBase(cred Cred) string {
	b := strings.TrimRight(strings.TrimSpace(cred.BaseURL), "/")
	if b == "" {
		return "https://api.github.com"
	}
	// 用户可能连 /api/v3 一起填了——去重，别拼成 /api/v3/api/v3
	b = strings.TrimSuffix(b, "/api/v3")
	return b + "/api/v3"
}

// graphQLBase GraphQL 端点。见文件顶注：GHES 走 /api/graphql，不跟 REST 的 /api/v3。
func graphQLBase(cred Cred) string {
	b := strings.TrimRight(strings.TrimSpace(cred.BaseURL), "/")
	if b == "" {
		return "https://api.github.com/graphql"
	}
	b = strings.TrimSuffix(b, "/api/v3")
	return b + "/api/graphql"
}

var httpClient = &http.Client{Timeout: 50 * time.Second}

func tokenOf(ctx plugin.Ctx) (string, error) {
	t := strings.TrimSpace(credOf(ctx).Token)
	if t == "" {
		return "", fmt.Errorf("凭证缺访问令牌——GitHub 头像 → Settings → Developer settings → " +
			"Personal access tokens 创建（经典令牌勾 repo；细粒度令牌按仓库授权）")
	}
	return t, nil
}

// ghCall 一次 REST 调用。GET/DELETE 走 query，其余走 JSON body。
// 返回原始字节 + 应答头（分页与速率信息都在头里）。
func ghCall(ctx plugin.Ctx, method, path string, params map[string]any) ([]byte, http.Header, error) {
	cred := credOf(ctx)
	token, err := tokenOf(ctx)
	if err != nil {
		return nil, nil, err
	}
	full := restBase(cred) + path
	var rd io.Reader
	if method == http.MethodGet || method == http.MethodDelete {
		if q := encodeQuery(params); q != "" {
			if strings.Contains(full, "?") {
				full += "&" + q
			} else {
				full += "?" + q
			}
		}
	} else if params != nil {
		b, _ := json.Marshal(params)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, full, rd)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	// 显式声明 API 版本：不带的话 GitHub 用「当前默认」，哪天默认换代了行为会静默变。
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Accept", "application/vnd.github+json")
	if rd != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("连接 GitHub 失败: %w"+
			"（GHES 确认插件容器能达 base_url，自签证书要挂进系统信任）", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if resp.StatusCode >= 300 {
		return nil, resp.Header, ghErr(resp.StatusCode, raw, path, resp.Header)
	}
	return raw, resp.Header, nil
}

func encodeQuery(params map[string]any) string {
	if len(params) == 0 {
		return ""
	}
	q := url.Values{}
	for k, v := range params {
		if v == nil {
			continue
		}
		if s := fmt.Sprintf("%v", v); s != "" {
			q.Set(k, s)
		}
	}
	return q.Encode()
}

// ghErr 高频错误 → 下一步该干什么。
//
// GitHub 的 403 有**两种完全不同的含义**：权限不够，和速率限制。区分靠 X-RateLimit-Remaining：
// 为 0 就是被限流了。混为一谈的话，用户会拿着「权限不够」去反复检查令牌 scope，
// 而其实只要等几分钟——这是接 GitHub 最费时间的一个误导。
func ghErr(code int, raw []byte, path string, h http.Header) error {
	var e struct {
		Message string `json:"message"`
		Errors  []struct {
			Resource string `json:"resource"`
			Field    string `json:"field"`
			Code     string `json:"code"`
			Message  string `json:"message"`
		} `json:"errors"`
		DocumentationURL string `json:"documentation_url"`
	}
	_ = json.Unmarshal(raw, &e)
	msg := e.Message
	if len(e.Errors) > 0 {
		var parts []string
		for _, it := range e.Errors {
			d := strings.TrimSpace(it.Message)
			if d == "" {
				d = strings.TrimSpace(it.Field + " " + it.Code)
			}
			if d != "" {
				parts = append(parts, d)
			}
		}
		if len(parts) > 0 {
			msg += "：" + strings.Join(parts, "; ")
		}
	}
	switch code {
	case http.StatusUnauthorized:
		return fmt.Errorf("GitHub 不认这个令牌（401）——过期或被吊销，重新生成一个")
	case http.StatusForbidden:
		if remain := strings.TrimSpace(h.Get("X-RateLimit-Remaining")); remain == "0" {
			return fmt.Errorf("被 GitHub 限流了（403，不是权限问题）——本小时配额用完，%s 后恢复；"+
				"认证请求每小时 5000 次，降低轮询频率或改用 Webhook", resetHint(h))
		}
		if strings.Contains(strings.ToLower(msg), "secondary rate") {
			return fmt.Errorf("触发了 GitHub 的二级速率限制（403）——短时间内写操作太密集，"+
				"隔一会儿重试；批量场景请在节点间加等待。原文：%s", msg)
		}
		return fmt.Errorf("令牌权限不够（403：%s）——经典令牌要勾 repo（私有仓库）与 workflow（触发 Actions）；"+
			"细粒度令牌要在该仓库上给对应的读写权限", msg)
	case http.StatusNotFound:
		// GitHub 对无权限的私有资源同样回 404（防探测）——话要说全，否则用户会一直核对路径。
		return fmt.Errorf("找不到（404：%s）——核对 %s；**没权限的私有仓库 GitHub 也回 404**，"+
			"确认令牌的账号在这个仓库里、且细粒度令牌授权了它", msg, path)
	case http.StatusConflict:
		return fmt.Errorf("冲突（409：%s）——常见于合并时目标分支已变，或分支已存在", msg)
	case 422:
		return fmt.Errorf("GitHub 拒绝了参数（422：%s）", msg)
	case 429:
		return fmt.Errorf("请求太频繁（429）——%s 后重试", resetHint(h))
	}
	return fmt.Errorf("GitHub 返回 HTTP %d：%s", code, msg)
}

// resetHint 限流恢复时间。X-RateLimit-Reset 是 Unix 秒。
func resetHint(h http.Header) string {
	if v := strings.TrimSpace(h.Get("Retry-After")); v != "" {
		return v + " 秒"
	}
	if v := strings.TrimSpace(h.Get("X-RateLimit-Reset")); v != "" {
		if ts, err := strconv.ParseInt(v, 10, 64); err == nil {
			if d := time.Until(time.Unix(ts, 0)); d > 0 {
				return d.Round(time.Second).String()
			}
		}
	}
	return "稍"
}

// —— GraphQL ——

// ghGraphQL 一次 GraphQL 调用。
//
// GraphQL 的错误**不走 HTTP 状态码**：查询失败照样 200，错误在响应体的 errors 里。
// 不解 errors 的话，一个拼错的字段名会表现成「返回了空数据」——看起来像「这个看板是空的」。
func ghGraphQL(ctx plugin.Ctx, query string, vars map[string]any) (map[string]any, error) {
	cred := credOf(ctx)
	token, err := tokenOf(ctx)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, graphQLBase(cred), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连接 GitHub GraphQL 失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode >= 300 {
		return nil, ghErr(resp.StatusCode, raw, "/graphql", resp.Header)
	}
	var out struct {
		Data   map[string]any `json:"data"`
		Errors []struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("GraphQL 响应解析失败: %w", err)
	}
	if len(out.Errors) > 0 {
		var msgs []string
		for _, e := range out.Errors {
			msgs = append(msgs, e.Message)
		}
		joined := strings.Join(msgs, "; ")
		// 看板相关最常见的两种：令牌少 project scope、或组织/个人认错了。
		if strings.Contains(joined, "INSUFFICIENT_SCOPES") || strings.Contains(joined, "read:project") {
			return nil, fmt.Errorf("令牌缺看板权限——经典令牌要加 project（或 read:project）scope，"+
				"细粒度令牌要给 Projects 读写。原文：%s", joined)
		}
		return nil, fmt.Errorf("GraphQL 报错：%s", joined)
	}
	return out.Data, nil
}

// —— 解析小工具 ——

// repoSplit 拆 owner/repo。GitHub 全程要求这个形态，早点拒比让 URL 拼出个 404 强。
func repoSplit(repo string) (string, string, error) {
	r := strings.Trim(strings.TrimSpace(repo), "/")
	// 容忍整条仓库地址：https://github.com/owner/repo(.git)
	if i := strings.Index(r, "github.com/"); i >= 0 {
		r = r[i+len("github.com/"):]
	}
	r = strings.TrimSuffix(r, ".git")
	parts := strings.Split(r, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("仓库要写成 owner/repo（如 sokel-dev/sokel-plugin-sdk），当前是 %q", repo)
	}
	return url.PathEscape(parts[0]), url.PathEscape(parts[1]), nil
}

// repoPath /repos/owner/repo 前缀。
func repoPath(repo string) (string, error) {
	o, r, err := repoSplit(repo)
	if err != nil {
		return "", err
	}
	return "/repos/" + o + "/" + r, nil
}

// hasNext 从 Link 头判断还有没有下一页。
//
// GitHub **不回总数**（搜索接口除外），只在 Link 里给 rel="next"。
// 拿「本页条数 == 每页大小」当判据是错的：正好整除时会多请求一页，
// 而最后一页恰好满时又会漏判——所以只认 Link。
func hasNext(h http.Header) bool {
	for _, link := range h.Values("Link") {
		for _, seg := range strings.Split(link, ",") {
			if strings.Contains(seg, `rel="next"`) {
				return true
			}
		}
	}
	return false
}

func digList(raw []byte) []map[string]any {
	var out []map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

func digObj(raw []byte) map[string]any {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m
}

func str(m map[string]any, k string) string {
	if v, ok := m[k]; ok && v != nil {
		if s, ok := v.(string); ok {
			return s
		}
		return fmt.Sprintf("%v", v)
	}
	return ""
}

func num(m map[string]any, k string) int {
	if v, ok := m[k].(float64); ok {
		return int(v)
	}
	return 0
}

func boolean(m map[string]any, k string) bool {
	v, _ := m[k].(bool)
	return v
}

func obj(m map[string]any, k string) map[string]any {
	mm, _ := m[k].(map[string]any)
	return mm
}

func arr(m map[string]any, k string) []any {
	a, _ := m[k].([]any)
	return a
}

// nested 取 m[k1][k2] 的字符串（user.login 这类）。
func nested(m map[string]any, k1, k2 string) string {
	return str(obj(m, k1), k2)
}

// namesOf 从对象数组里抽某个字段（labels 的 name、assignees 的 login）。
func namesOf(v any, key string) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, it := range list {
		switch t := it.(type) {
		case string:
			out = append(out, t)
		case map[string]any:
			if s := str(t, key); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// clip 截断长文本。Issue 正文/搜索片段动辄几 KB，几十条就能把下游上下文吃满。
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// perPage 每页条数与页码。GitHub 的 per_page 上限是 100。
func perPage(page int) map[string]any {
	if page < 1 {
		page = 1
	}
	return map[string]any{"per_page": 50, "page": page}
}

// isPullRequest 这条 Issue 其实是不是 PR。
//
// GitHub 的 /issues 接口**会把 PR 一起返回**，判据是有没有 pull_request 这个键
// （不是看标题、不是看 URL）。见 schema 包顶注第一条。
func isPullRequest(m map[string]any) bool {
	_, ok := m["pull_request"]
	return ok
}

// toIssue 把一条 Issue/PR 的 JSON 转成契约形状。
func toIssue(m map[string]any) schema.Issue {
	return schema.Issue{
		Number:    num(m, "number"),
		Title:     str(m, "title"),
		Body:      clip(str(m, "body"), 4000),
		State:     str(m, "state"),
		IsPR:      isPullRequest(m),
		Author:    nested(m, "user", "login"),
		Assignees: namesOf(m["assignees"], "login"),
		Labels:    namesOf(m["labels"], "name"),
		Milestone: nested(m, "milestone", "title"),
		Comments:  num(m, "comments"),
		URL:       str(m, "html_url"),
		CreatedAt: str(m, "created_at"),
		UpdatedAt: str(m, "updated_at"),
		ClosedAt:  str(m, "closed_at"),
	}
}

// toPR 把一条 PR 的 JSON 转成契约形状。
//
// mergeable 刻意是字符串而不是布尔：GitHub 异步计算它，未算完时是 null——
// 用布尔的话 null 会落成 false，看起来像「有冲突」，而其实是「还不知道」。
func toPR(m map[string]any) schema.PR {
	mergeable := "unknown"
	if v, ok := m["mergeable"].(bool); ok {
		mergeable = strconv.FormatBool(v)
	}
	return schema.PR{
		Number:    num(m, "number"),
		Title:     str(m, "title"),
		Body:      clip(str(m, "body"), 4000),
		State:     str(m, "state"),
		Merged:    boolean(m, "merged") || str(m, "merged_at") != "",
		Draft:     boolean(m, "draft"),
		Author:    nested(m, "user", "login"),
		Base:      nested(m, "base", "ref"),
		Head:      nested(m, "head", "ref"),
		HeadSHA:   nested(m, "head", "sha"),
		Labels:    namesOf(m["labels"], "name"),
		Assignees: namesOf(m["assignees"], "login"),
		Reviewers: namesOf(m["requested_reviewers"], "login"),
		Mergeable: mergeable,
		Additions: num(m, "additions"),
		Deletions: num(m, "deletions"),
		URL:       str(m, "html_url"),
		CreatedAt: str(m, "created_at"),
		UpdatedAt: str(m, "updated_at"),
	}
}
