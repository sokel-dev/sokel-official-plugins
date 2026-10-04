package main

// The outbound layer for TuShare Pro: one POST handles everything, and the columnar response is
// restored into records.
//
// Two ways this is quite different from Juchao, and they basically shape this plugin:
//   - Every endpoint shares one single HTTP endpoint, distinguished by api_name in the request
//     body; auth is a token, not a signature.
//   - The response is **columnar** (one fields row of column names + items as a bunch of arrays),
//     not an array of objects.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.tushare.pro"

type client struct {
	baseURL string
	token   string
	hc      *http.Client
}

func newClient(token, baseURL string) *client {
	if baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/"); baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &client{baseURL: baseURL, token: token, hc: &http.Client{Timeout: 2 * time.Minute}}
}

type tsRequest struct {
	APIName string            `json:"api_name"`
	Token   string            `json:"token"`
	Params  map[string]string `json:"params"`
	Fields  string            `json:"fields,omitempty"`
}

// tsResponse: a non-zero code means a business-level failure; msg is meant for humans (usually
// "insufficient points" or "no permission").
type tsResponse struct {
	RequestID string  `json:"request_id"`
	Code      int     `json:"code"`
	Msg       string  `json:"msg"`
	Data      *tsData `json:"data"`
}

// tsData is the columnar result: fields holds the column names, items holds one value array per
// row, in the same order as fields.
type tsData struct {
	Fields  []string `json:"fields"`
	Items   [][]any  `json:"items"`
	HasMore bool     `json:"has_more"`
}

// call makes one TuShare request. Retries are bounded (three attempts with backoff): one call is
// one step on the canvas, so if it can't be fetched, let this step fail and let the scheduler come
// back with the original cursor.
func (c *client) call(ctx context.Context, apiName string, params map[string]string, fields string) (*tsData, error) {
	var lastErr error
	for attempt := range 3 {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(1<<attempt) * time.Second):
			}
		}
		data, err := c.doCall(ctx, apiName, params, fields)
		if err == nil {
			return data, nil
		}
		if !retryable(err) {
			return nil, err
		}
		lastErr = err
	}
	return nil, lastErr
}

func (c *client) doCall(ctx context.Context, apiName string, params map[string]string, fields string) (*tsData, error) {
	if c.token == "" {
		return nil, fmt.Errorf("缺少 TuShare token（在插件凭证里配置）")
	}
	body, err := json.Marshal(tsRequest{APIName: apiName, Token: c.token, Params: params, Fields: fields})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := c.hc.Do(req)
	if err != nil {
		return nil, transientErr{fmt.Errorf("请求 TuShare 失败: %w", err)}
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, transientErr{fmt.Errorf("读取 TuShare 响应失败: %w", err)}
	}
	if res.StatusCode != http.StatusOK {
		err := fmt.Errorf("TuShare 返回 HTTP %d: %s", res.StatusCode, truncate(string(raw), 200))
		if res.StatusCode >= 500 || res.StatusCode == http.StatusTooManyRequests {
			return nil, transientErr{err}
		}
		return nil, err
	}

	var resp tsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("解析 TuShare 响应失败: %w；原文 %s", err, truncate(string(raw), 200))
	}
	if resp.Code != 0 {
		// msg is surfaced to the user verbatim; it's usually something like "sorry, you don't have
		// access to this endpoint" — rewording it in our own words would only lose information.
		return nil, fmt.Errorf("TuShare 接口 %s 报错[%d]：%s", apiName, resp.Code, resp.Msg)
	}
	if resp.Data == nil {
		return &tsData{}, nil
	}
	return resp.Data, nil
}

// decodeRows restores the columnar result into records.
//
// It round-trips through JSON instead of assigning fields via reflection field by field: type
// conversion (number/string/null) is left to encoding/json, which errors on a mismatch — whereas a
// hand-written conversion usually fills the mismatched cell with the zero value **silently**.
func decodeRows[R any](d *tsData) ([]R, error) {
	if d == nil || len(d.Items) == 0 {
		return nil, nil
	}
	objs := make([]map[string]any, 0, len(d.Items))
	for _, row := range d.Items {
		obj := make(map[string]any, len(d.Fields))
		for i, name := range d.Fields {
			if i < len(row) {
				obj[name] = row[i]
			}
		}
		objs = append(objs, obj)
	}
	b, err := json.Marshal(objs)
	if err != nil {
		return nil, err
	}
	var out []R
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("TuShare 返回的字段类型与契约对不上: %w", err)
	}
	return out, nil
}

// transientErr marks an error worth retrying (network blips / 5xx / 429).
// Business errors (no permission, insufficient points) give the same result no matter how many
// times they're retried.
type transientErr struct{ error }

func retryable(err error) bool {
	_, ok := err.(transientErr)
	return ok
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
