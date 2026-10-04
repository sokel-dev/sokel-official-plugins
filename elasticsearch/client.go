package main

// ES REST 调用层。裸 HTTP：JSON in / JSON out，没有 SDK 也没有版本锁。
//
// 认证两种：API Key（`Authorization: ApiKey <encoded>`）优先，其次 basic。
// 两个都不填也允许——自建集群关掉安全模块（或 OpenSearch 的 demo 配置）是常见形态。

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

func credOf(ctx plugin.Ctx) Cred {
	var c Cred
	sokel.BindCredential(ctx, &c)
	return c
}

func baseOf(c Cred) (string, error) {
	b := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if b == "" {
		return "", fmt.Errorf("凭证缺地址——填集群地址，如 https://es.internal:9200")
	}
	if !strings.HasPrefix(b, "http://") && !strings.HasPrefix(b, "https://") {
		b = "http://" + b // 只写了 host:port 的按明文补全，报错里也能看出补成了什么
	}
	return b, nil
}

// 跳过证书校验要换一个 Transport，但**别每次调用都新建 http.Client**——
// 那样连接池不复用，每次请求都重新握 TLS。按「跳不跳」两种形态各留一个。
var (
	clientOnce   sync.Once
	strictClient *http.Client
	looseClient  *http.Client
)

func httpClientFor(c Cred) *http.Client {
	clientOnce.Do(func() {
		strictClient = &http.Client{Timeout: 120 * time.Second}
		looseClient = &http.Client{
			Timeout: 120 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // 凭证显式选择
			},
		}
	})
	if strings.TrimSpace(c.TLSInsecure) == "on" {
		return looseClient
	}
	return strictClient
}

// esCall 一次 ES 调用。body 为 nil 表示不带请求体。
// 返回原始字节 + HTTP 状态码——状态码要交给调用方判：ES 用 404 表达
// 「文档/索引不存在」，那在很多操作里是正常分支而不是错误。
func esCall(ctx plugin.Ctx, method, path string, body any) ([]byte, int, error) {
	cred := credOf(ctx)
	base, err := baseOf(cred)
	if err != nil {
		return nil, 0, err
	}
	var rd io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("请求体序列化失败: %w", err)
		}
		rd = bytes.NewReader(buf)
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, rd)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	applyAuth(req, cred)

	resp, err := httpClientFor(cred).Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("连不上 %s: %w（检查地址、网络放行、证书）", base, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("读应答失败: %w", err)
	}
	return raw, resp.StatusCode, nil
}

func applyAuth(req *http.Request, c Cred) {
	if k := strings.TrimSpace(c.APIKey); k != "" {
		req.Header.Set("Authorization", "ApiKey "+k)
		return
	}
	if u := strings.TrimSpace(c.Username); u != "" {
		req.SetBasicAuth(u, c.Password)
	}
}

// esJSON 调用并把成功应答解成 map；**非 2xx 一律报错**，且把 ES 的
// error.reason 原文带出来——那句话往往直接说明了问题（字段类型不对、
// 索引只读、分片没分配），翻译一遍反而丢信息。
func esJSON(ctx plugin.Ctx, method, path string, body any) (map[string]any, error) {
	raw, code, err := esCall(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	if code >= 400 {
		return nil, esError(code, raw)
	}
	var out map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, fmt.Errorf("应答不是 JSON（HTTP %d）: %s", code, clip(string(raw), 300))
		}
	}
	return out, nil
}

func esError(code int, raw []byte) error {
	var e struct {
		Error struct {
			Type   string `json:"type"`
			Reason string `json:"reason"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &e) == nil && e.Error.Reason != "" {
		return fmt.Errorf("ES 报错 HTTP %d：%s（%s）", code, e.Error.Reason, e.Error.Type)
	}
	return fmt.Errorf("ES 报错 HTTP %d：%s", code, clip(string(raw), 300))
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// —— 取值帮手：只用于**成功**应答的解构 ——

func mapAt(m map[string]any, keys ...string) map[string]any {
	cur := m
	for _, k := range keys {
		next, _ := cur[k].(map[string]any)
		if next == nil {
			return nil
		}
		cur = next
	}
	return cur
}

func str(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func num(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

func flt(m map[string]any, key string) float64 {
	f, _ := m[key].(float64)
	return f
}

func boolAt(m map[string]any, key string) bool {
	b, _ := m[key].(bool)
	return b
}

func listAt(m map[string]any, key string) []any {
	l, _ := m[key].([]any)
	return l
}

// indexPath 索引名进路径。逗号分隔的多索引与通配符原样保留（ES 认），
// 只挡空值与斜杠——后者会把路径切出一段，等于调到别的接口上去。
func indexPath(index string) (string, error) {
	idx := strings.TrimSpace(index)
	if idx == "" {
		return "", fmt.Errorf("没给索引名")
	}
	if strings.Contains(idx, "/") {
		return "", fmt.Errorf("索引名 %q 里不能有斜杠", idx)
	}
	return idx, nil
}

// esNDJSON _bulk 专用：请求体是 NDJSON 不是 JSON，Content-Type 也得换，
// 否则 ES 回 406 说不支持的媒体类型。
func esNDJSON(ctx plugin.Ctx, path, body string) (map[string]any, error) {
	cred := credOf(ctx)
	base, err := baseOf(cred)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-ndjson")
	req.Header.Set("Accept", "application/json")
	applyAuth(req, cred)
	resp, err := httpClientFor(cred).Do(req)
	if err != nil {
		return nil, fmt.Errorf("连不上 %s: %w", base, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, esError(resp.StatusCode, raw)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("_bulk 应答解析失败: %s", clip(string(raw), 300))
	}
	return out, nil
}
