package main

// GitHub call layer: REST v3 + GraphQL v4. Plain HTTP, no SDK needed.
//
// See the top comment in schema/projects.go for why both transports coexist (Projects V2 is
// GraphQL-only). github.com and GitHub Enterprise Server differ only in the base:
//
//	github.com  ->  https://api.github.com          GraphQL: https://api.github.com/graphql
//	GHES        ->  https://host/api/v3             GraphQL: https://host/api/graphql
//
// Note that GHES's GraphQL endpoint is /api/graphql, **not** /api/v3/graphql — building it the
// same way as the REST path gives a 404, and that 404 looks like "you lack permission" here,
// which makes it hard to think of "the endpoint is wrong" as the cause.

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

// restBase returns the REST root address.
func restBase(cred Cred) string {
	b := strings.TrimRight(strings.TrimSpace(cred.BaseURL), "/")
	if b == "" {
		return "https://api.github.com"
	}
	// the user may have filled in /api/v3 as well — dedupe it, don't end up with /api/v3/api/v3
	b = strings.TrimSuffix(b, "/api/v3")
	return b + "/api/v3"
}

// graphQLBase returns the GraphQL endpoint. See the file's top comment: GHES uses /api/graphql,
// not REST's /api/v3.
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

// ghCall makes one REST call. GET/DELETE go through the query string, everything else through a
// JSON body. Returns the raw bytes plus the response headers (pagination and rate-limit info
// both live in the headers).
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
	// Declare the API version explicitly: without it, GitHub uses "the current default", and
	// behavior will silently change whenever that default moves on.
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

// ghErr maps frequent errors to what to do next.
//
// GitHub's 403 has **two completely different meanings**: insufficient permissions, and rate
// limiting. Tell them apart via X-RateLimit-Remaining: 0 means rate-limited. Conflating the two
// sends users off repeatedly re-checking their token's scopes when all they needed to do was
// wait a few minutes — this is the single most time-wasting bit of misdirection when
// integrating with GitHub.
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
		// GitHub also returns 404 for private resources you lack access to (anti-enumeration)
		// — the message needs to say this explicitly, or users will keep re-checking the path.
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

// resetHint reports the time until the rate limit resets. X-RateLimit-Reset is Unix seconds.
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

// ghGraphQL makes one GraphQL call.
//
// GraphQL errors **don't use the HTTP status code**: a failed query still returns 200, with the
// error in the response body's `errors` field. Without parsing `errors`, a misspelled field name
// shows up as "returned empty data" — which looks like "this board is empty".
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
		// The two most common board-related causes: the token lacks the project scope, or the
		// wrong org/user was specified.
		if strings.Contains(joined, "INSUFFICIENT_SCOPES") || strings.Contains(joined, "read:project") {
			return nil, fmt.Errorf("令牌缺看板权限——经典令牌要加 project（或 read:project）scope，"+
				"细粒度令牌要给 Projects 读写。原文：%s", joined)
		}
		return nil, fmt.Errorf("GraphQL 报错：%s", joined)
	}
	return out.Data, nil
}

// —— parsing helpers ——

// repoSplit splits owner/repo. GitHub requires this shape throughout; rejecting early is better
// than letting a bad URL produce a 404 downstream.
func repoSplit(repo string) (string, string, error) {
	r := strings.Trim(strings.TrimSpace(repo), "/")
	// tolerate a full repo URL: https://github.com/owner/repo(.git)
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

// repoPath builds the /repos/owner/repo prefix.
func repoPath(repo string) (string, error) {
	o, r, err := repoSplit(repo)
	if err != nil {
		return "", err
	}
	return "/repos/" + o + "/" + r, nil
}

// hasNext determines whether there's a next page from the Link header.
//
// GitHub **does not return a total count** (except on search endpoints); it only gives
// rel="next" in the Link header. Using "this page's count == page size" as the criterion is
// wrong: it over-fetches one extra page when the total divides evenly, and misses the case
// where the last page happens to be full — so only the Link header is trusted.
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

// nested reads the string at m[k1][k2] (things like user.login).
func nested(m map[string]any, k1, k2 string) string {
	return str(obj(m, k1), k2)
}

// namesOf extracts one field from an array of objects (e.g. labels' `name`, assignees' `login`).
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

// clip truncates long text. Issue bodies/search snippets routinely run to several KB each; a
// few dozen of them can fill up the downstream context.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// perPage builds the page size and page number params. GitHub's per_page cap is 100.
func perPage(page int) map[string]any {
	if page < 1 {
		page = 1
	}
	return map[string]any{"per_page": 50, "page": page}
}

// isPullRequest reports whether this Issue is actually a PR.
//
// GitHub's /issues endpoint **returns PRs mixed in**; the criterion is whether the
// pull_request key is present (not the title, not the URL). See the first point in the schema
// package's top comment.
func isPullRequest(m map[string]any) bool {
	_, ok := m["pull_request"]
	return ok
}

// toIssue converts one Issue/PR's JSON into the contract shape.
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

// toPR converts one PR's JSON into the contract shape.
//
// mergeable is deliberately a string, not a bool: GitHub computes it asynchronously and it's
// null while not yet computed — with a bool, null would collapse to false, which looks like
// "has conflicts" when it actually means "not known yet".
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
