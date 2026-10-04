package main

// Outbound requests to the Gmail API.
//
// Auth: **the access_token injected by the platform** (the access_token field of this plugin's
// own credential). The plugin has neither a client_secret nor ever handles a refresh_token —
// rotating the token is the platform's job; what we get here is always a ready-to-use,
// short-lived token that can be swapped out at any time.

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

// httpClient is the outbound client. Timeout is generous — attachments can be tens of MB.
var httpClient = &http.Client{Timeout: 120 * time.Second}

// apiError carries the HTTP status code so the caller can distinguish "authorization expired"
// from "bad parameters."
type apiError struct {
	Status int
	Body   string
}

func (e *apiError) Error() string {
	switch e.Status {
	case http.StatusUnauthorized:
		// On this path, 401 almost always means one specific thing, so say it directly
		// instead of leaving people to guess whether it's scopes or the network
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

// gmailGet does a GET against a Gmail endpoint and decodes into out. The caller is responsible
// for getting the token from the credential.
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

// listQuery builds the messages.list query string from the node's inputs.
//
// The default and the cap for max_results both live here: without a cap, listing 500 messages
// and then fetching each one's detail would burn through the whole day's quota (Gmail has a
// daily quota, and messages.get is billed per message).
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

// messageSummary is one entry in a list (only these two fields when with_detail=false).
type messageSummary struct {
	ID       string `json:"id"`
	ThreadID string `json:"threadId"`
}

type listResponse struct {
	Messages           []messageSummary `json:"messages"`
	NextPageToken      string           `json:"nextPageToken"`
	ResultSizeEstimate int              `json:"resultSizeEstimate"`
}

// toDetail converts one message into the contract's element type (body/attachments/headers all
// flattened, so downstream consumers can reference them directly per the contract).
//
// Producing a schema.MessageItem directly instead of a map: field names are fixed by the
// contract, so a typo is caught at compile time. It used to be a map, which meant there also
// had to be a remapDetail to shuffle the keys over again — two hand-written copies were bound
// to drift apart eventually.
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
