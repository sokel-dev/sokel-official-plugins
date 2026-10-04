package main

// TuShare Pro 的出口层：一个 POST 打天下 + 列式响应还原成记录。
//
// 与巨潮很不一样的两点，插件的形状基本由它们决定：
//   - 所有接口共用一个端点，靠请求体里的 api_name 区分；鉴权是 token，不是签名。
//   - 响应是**列式**的（fields 一行列名 + items 一堆数组），不是对象数组。

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

// tsResponse code 非 0 即业务失败，msg 是给人看的（多半是「积分不够」「没权限」）。
type tsResponse struct {
	RequestID string  `json:"request_id"`
	Code      int     `json:"code"`
	Msg       string  `json:"msg"`
	Data      *tsData `json:"data"`
}

// tsData 列式结果：fields 是列名，items 每行一个值数组，顺序与 fields 对齐。
type tsData struct {
	Fields  []string `json:"fields"`
	Items   [][]any  `json:"items"`
	HasMore bool     `json:"has_more"`
}

// call 调一次 TuShare。重试有界（三次退避）：一次调用就是画布上的一步，
// 拉不到就让这步失败、让定时器带着原游标再来。
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
		// 这里的 msg 会原样呈给用户，多半是「抱歉，您没有访问该接口的权限」这类，
		// 换成自己的措辞只会丢信息。
		return nil, fmt.Errorf("TuShare 接口 %s 报错[%d]：%s", apiName, resp.Code, resp.Msg)
	}
	if resp.Data == nil {
		return &tsData{}, nil
	}
	return resp.Data, nil
}

// decodeRows 把列式结果还原成记录。
//
// 走一趟 JSON 而不是逐字段反射赋值：类型转换（数字/字符串/null）交给 encoding/json，
// 它对不上会报错——而手写转换里对不上的那一格通常是**静默**填零值。
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

// transientErr 值得重试的错误（网络抖动 / 5xx / 429）。
// 业务错误（没权限、积分不够）重试多少次都是同一个结果。
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
