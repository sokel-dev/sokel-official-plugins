package main

// GitLab REST v4 调用层。纯 HTTP：PRIVATE-TOKEN 头 + JSON，无 SDK 可省。
//
// 自建 CE 与 gitlab.com 同一套 API——差别只在 base_url，凭证里配。

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

// glCall 一次 API 调用。query 与 body 二选一由 method 决定（GET 走 query）。
// 返回原始字节 + 应答头（分页信息在头里）。
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

// glErr 高频错误 → 下一步。GitLab 的报错体是 {"message": ...}（message 可能是串/对象/数组）。
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
		// GitLab 对无权限的项目也回 404（防探测）——话要说全。
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

// pid 项目双形态：数字原样，路径 URL 编码。
func pid(project string) (string, error) {
	p := strings.TrimSpace(project)
	if p == "" {
		return "", fmt.Errorf("项目是空的——数字 ID 或路径（如 backend/server）")
	}
	return url.PathEscape(p), nil
}

// digList 解 JSON 数组应答。
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

// nested 取 m[k1][k2] 的字符串（author.name 这类）。
func nested(m map[string]any, k1, k2 string) string {
	if mm, ok := m[k1].(map[string]any); ok {
		return str(mm, k2)
	}
	return ""
}

// digObj 单对象应答 → map。
func digObj(raw []byte) map[string]any {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m
}

// digStrings 字符串数组字段（labels 这类）。
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

// pageInfo 从 GitLab 的分页响应头取总数与「还有没有下一页」。
//
// 列表只回「本页条数」是**静默截断**：正好 50 条时,调用方无从知道是刚好这么多
// 还是被截了。GitLab 把答案放在头里（X-Total / X-Next-Page）,glCall 一直在返回 header,
// 只是从来没人读。超大结果集上 GitLab 会省略 X-Total（算总数太贵）,那时 total=0,
// 但 X-Next-Page 仍在——所以「还有没有下一页」比总数可靠,判翻页要看它。
func pageInfo(h http.Header) (total int, hasMore bool) {
	if h == nil {
		return 0, false
	}
	if v := strings.TrimSpace(h.Get("X-Total")); v != "" {
		total, _ = strconv.Atoi(v)
	}
	return total, strings.TrimSpace(h.Get("X-Next-Page")) != ""
}

// clip 截断长文本。搜索片段/正文一条动辄几 KB，几十条命中就能把下游上下文吃满。
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
