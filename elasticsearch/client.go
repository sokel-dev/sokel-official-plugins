package main

// ES REST call layer. Plain HTTP: JSON in / JSON out, no SDK and no version lock.
//
// Two auth modes: API Key (`Authorization: ApiKey <encoded>`) takes priority, basic auth
// otherwise. Leaving both empty is also allowed — a self-hosted cluster with security
// disabled (or OpenSearch's demo config) is a common setup.

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
		b = "http://" + b // plain host:port gets http:// prepended; errors show what it was completed to
	}
	return b, nil
}

// Skipping cert verification needs a different Transport, but **don't build a new
// http.Client on every call** — that would stop the connection pool from being reused and
// force a fresh TLS handshake on every request. Keep one client for each of the two modes.
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
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // explicit credential choice
			},
		}
	})
	if strings.TrimSpace(c.TLSInsecure) == "on" {
		return looseClient
	}
	return strictClient
}

// esCall makes one ES call. A nil body means no request body is sent.
// It returns the raw bytes plus the HTTP status code — the caller must interpret the
// status code: ES uses 404 to mean "document/index not found", which is a normal branch
// in many operations rather than an error.
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

// esJSON makes the call and decodes a successful response into a map; **any non-2xx is
// always an error**, and ES's own error.reason text is passed through verbatim — it
// usually states the problem directly (wrong field type, read-only index, unassigned
// shards), and paraphrasing it would only lose information.
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

// —— value accessors: for decoding **successful** responses only ——

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

// indexPath puts an index name into a path. Comma-separated multi-index lists and
// wildcards are passed through as-is (ES understands them); it only rejects empty values
// and slashes — a slash would cut the path into another segment and hit a different
// endpoint.
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

// esNDJSON is for _bulk only: the request body is NDJSON, not JSON, so the Content-Type
// must change too, or ES replies 406 with an unsupported media type error.
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
