package main

// Gmail API 出站。
//
// 鉴权：**平台注入的 access_token**（本插件自有凭证的 access_token 字段）。
// 插件既没有 client_secret 也不经手 refresh_token——换 token 是平台的事，
// 这里拿到的永远是一个现成可用、随时可能被换掉的短期 token。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-official-plugins/gmail/schema"
)

const gmailBase = "https://gmail.googleapis.com/gmail/v1/users/me"

// httpClient：出站客户端。超时给足——附件可能几十 MB。
var httpClient = &http.Client{Timeout: 120 * time.Second}

// apiError：带上 HTTP 状态码，让调用方能区分「授权失效」与「参数错」。
type apiError struct {
	Status int
	Body   string
}

func (e *apiError) Error() string {
	switch e.Status {
	case http.StatusUnauthorized:
		// 401 在这条链路上几乎只有一个含义，直接说出来，省得人去猜作用域还是网络
		return "Gmail 拒绝访问（401）：授权已失效，请到凭证页重新授权"
	case http.StatusForbidden:
		return "Gmail 拒绝访问（403）：可能是未启用 Gmail API 或作用域不足 —— " + trunc(e.Body, 200)
	case http.StatusTooManyRequests:
		return "Gmail 限流（429）：请调大轮询间隔或减少一次拉取的邮件数"
	}
	return fmt.Sprintf("Gmail 返回 %d: %s", e.Status, trunc(e.Body, 300))
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// gmailGet：GET 一个 Gmail 端点并解进 out。token 由调用方从凭证取。
func gmailGet(ctx context.Context, token, path string, q url.Values, out any) error {
	if token == "" {
		return fmt.Errorf("缺少 Google 授权：请到凭证页点「授权」完成一次 Google 同意")
	}
	u := gmailBase + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("连接 Gmail 失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if resp.StatusCode >= 400 {
		return &apiError{Status: resp.StatusCode, Body: string(body)}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("Gmail 应答无法解析: %w", err)
	}
	return nil
}

// listQuery：把节点入参拼成 messages.list 的查询串。
//
// max_results 兜底与封顶都在这儿：不给上限的话，一次 500 封再逐封拉详情
// 就是把当天的配额烧光（Gmail 每天有配额，且 messages.get 按封计费）。
func listQuery(query, labelIDs string, maxResults int) url.Values {
	q := url.Values{}
	if query != "" {
		q.Set("q", query)
	}
	for _, l := range strings.Split(labelIDs, ",") {
		if l = strings.TrimSpace(l); l != "" {
			q.Add("labelIds", l)
		}
	}
	switch {
	case maxResults <= 0:
		maxResults = 20
	case maxResults > 100:
		maxResults = 100
	}
	q.Set("maxResults", fmt.Sprint(maxResults))
	return q
}

// messageSummary：列表里每一项（with_detail=false 时只有这两个字段）。
type messageSummary struct {
	ID       string `json:"id"`
	ThreadID string `json:"threadId"`
}

type listResponse struct {
	Messages           []messageSummary `json:"messages"`
	NextPageToken      string           `json:"nextPageToken"`
	ResultSizeEstimate int              `json:"resultSizeEstimate"`
}

// toDetail：一封邮件 → 契约里的元素类型（正文/附件/头都摊平，下游按契约直接引用）。
//
// 直接产 schema.MessageItem 而不是 map：字段名由契约定死，拼错编译期就报。
// 早先是 map，于是还得有个 remapDetail 把键名再搬一遍——两处手写迟早对不上。
func toDetail(m gmailMessage) schema.MessageItem {
	text, html := extractBodies(m.Payload)
	return schema.MessageItem{
		ID: m.ID, ThreadID: m.ThreadID,
		Subject:     header(m.Payload, "Subject"),
		From:        header(m.Payload, "From"),
		To:          header(m.Payload, "To"),
		Date:        header(m.Payload, "Date"),
		Text:        text,
		HTML:        html,
		Snippet:     m.Snippet,
		Attachments: extractAttachments(m.Payload),
		LabelIDs:    m.LabelIDs,
	}
}
