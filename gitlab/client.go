package main

// GitLab REST v4 call layer. Plain HTTP: PRIVATE-TOKEN header + JSON, no SDK needed.
//
// Self-hosted CE uses the same API as gitlab.com -- the only difference is base_url, set in the credential.

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

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

func credOf(ctx plugin.Ctx) Cred {
	var c Cred
	sokel.BindCredential(ctx, &c)
	return c
}

func baseOf(cred Cred) string {
	b := strings.TrimRight(strings.TrimSpace(cred.BaseURL), "/")
	if b == "" {
		b = "https://gitlab.com"
	}
	return b + "/api/v4"
}

var httpClient = &http.Client{Timeout: 50 * time.Second}

// glCall makes one API call. Whether query or body is used is decided by method (GET uses query).
// Returns the raw bytes plus response headers (pagination info lives in the headers).
func glCall(ctx plugin.Ctx, method, path string, params map[string]any) ([]byte, http.Header, error) {
	cred := credOf(ctx)
	token := strings.TrimSpace(cred.Token)
	if token == "" {
		return nil, nil, fmt.Errorf("凭证缺访问令牌——GitLab 头像 → Preferences → Access Tokens 创建（scope 至少 api）")
	}
	full := baseOf(cred) + path
	var rd io.Reader
	if method == http.MethodGet || method == http.MethodDelete {
		if len(params) > 0 {
			q := url.Values{}
			for k, v := range params {
				if v == nil {
					continue
				}
				s := fmt.Sprintf("%v", v)
				if s != "" {
					q.Set(k, s)
				}
			}
			if enc := q.Encode(); enc != "" {
				if strings.Contains(full, "?") {
					full += "&" + enc
				} else {
					full += "?" + enc
				}
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
	req.Header.Set("PRIVATE-TOKEN", token)
	if rd != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("连接 GitLab 失败: %w（自建实例确认插件容器能达 base_url，自签证书要挂进系统信任）", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode >= 300 {
		return nil, resp.Header, glErr(resp.StatusCode, raw, path)
	}
	return raw, resp.Header, nil
}

// glErr maps common errors to next steps. GitLab's error body is {"message": ...} (message may be a string/object/array).
func glErr(code int, raw []byte, path string) error {
	var e struct {
		Message any    `json:"message"`
		Error   string `json:"error"`
	}
	_ = json.Unmarshal(raw, &e)
	msg := e.Error
	if msg == "" && e.Message != nil {
		msg = fmt.Sprintf("%v", e.Message)
	}
	switch code {
	case http.StatusUnauthorized:
		return fmt.Errorf("GitLab 不认这个 token（401）——过期或被吊销，重新生成一个（scope 至少 api）")
	case http.StatusForbidden:
		return fmt.Errorf("token 权限不够（403：%s）——scope 要含 api，且账号要有该项目的对应角色", msg)
	case http.StatusNotFound:
		// GitLab also returns 404 for projects you lack access to (to prevent probing) -- spell this out.
		return fmt.Errorf("找不到（404：%s）——路径 %s 核对；**没权限的项目 GitLab 也回 404**，确认 token 的账号在项目里", msg, path)
	case http.StatusMethodNotAllowed:
		return fmt.Errorf("方法不对（405）——%s 核对 API 文档", path)
	case http.StatusConflict:
		return fmt.Errorf("冲突（409：%s）", msg)
	case 422:
		return fmt.Errorf("GitLab 拒绝了参数（422：%s）", msg)
	}
	return fmt.Errorf("GitLab 返回 HTTP %d：%s", code, msg)
}

// pid handles the two project forms: a numeric id is passed through, a path is URL-encoded.
func pid(project string) (string, error) {
	p := strings.TrimSpace(project)
	if p == "" {
		return "", fmt.Errorf("项目是空的——数字 ID 或路径（如 backend/server）")
	}
	return url.PathEscape(p), nil
}

// digList parses a JSON array response.
func digList(raw []byte) []map[string]any {
	var out []map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

func str(m map[string]any, k string) string {
	if v, ok := m[k]; ok && v != nil {
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

// nested reads the string at m[k1][k2] (things like author.name).
func nested(m map[string]any, k1, k2 string) string {
	if mm, ok := m[k1].(map[string]any); ok {
		return str(mm, k2)
	}
	return ""
}

// digObj turns a single-object response into a map.
func digObj(raw []byte) map[string]any {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m
}

// digStrings reads a string-array field (things like labels).
func digStrings(v any) []string {
	arr, _ := v.([]any)
	out := make([]string, 0, len(arr))
	for _, it := range arr {
		if s, ok := it.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// pageInfo reads the total count and "is there another page" from GitLab's pagination
// response headers.
//
// A list that only returns "items on this page" is a silent truncation: with exactly 50
// items, the caller has no way to tell whether that's really all of them or the list got cut
// off. GitLab puts the answer in the headers (X-Total / X-Next-Page); glCall has always
// returned the header, it just never got read. On very large result sets GitLab omits X-Total
// (computing the total is too expensive), so total=0 then, but X-Next-Page is still present --
// so "is there another page" is more reliable than the total; use it to decide on paging.
func pageInfo(h http.Header) (total int, hasMore bool) {
	if h == nil {
		return 0, false
	}
	if v := strings.TrimSpace(h.Get("X-Total")); v != "" {
		total, _ = strconv.Atoi(v)
	}
	return total, strings.TrimSpace(h.Get("X-Next-Page")) != ""
}

// clip truncates long text. A single search snippet/body can easily be several KB, and dozens
// of hits can fill up downstream context.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
