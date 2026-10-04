package main

// Outbound requests and the three operations. The Discord webhook is the simplest of this batch of
// publishers: one URL is the entire credential.
//
// Two non-obvious points:
//   - **?wait=true**: without it, Discord replies with an empty 204 body and there's no message id,
//     so downstream edit/delete won't work.
//   - **Attachments go through multipart**, and the JSON payload must go in a field named
//     payload_json (not a regular form key).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

const (
	maxContent = 2000
	maxDescLen = 4096
)

var (
	clientMu sync.Mutex
	clients  = map[string]*http.Client{}
)

func clientFor(proxy string) *http.Client {
	proxy = strings.TrimSpace(proxy)
	clientMu.Lock()
	defer clientMu.Unlock()
	if c, ok := clients[proxy]; ok {
		return c
	}
	c := &http.Client{Timeout: 60 * time.Second}
	if proxy != "" {
		if u, err := url.Parse(proxy); err == nil {
			tr := http.DefaultTransport.(*http.Transport).Clone()
			tr.Proxy = http.ProxyURL(u)
			c.Transport = tr
		}
	}
	clients[proxy] = c
	return c
}

func hookOf(ctx plugin.Ctx) (string, *http.Client, error) {
	c := sokel.CredentialAs[Cred](ctx)
	u := strings.TrimRight(strings.TrimSpace(c.WebhookURL), "/")
	if u == "" {
		return "", nil, fmt.Errorf("凭证里没填 Webhook 地址（频道设置 → 整合 → Webhook → 新建）")
	}
	if !strings.Contains(u, "/api/webhooks/") {
		return "", nil, fmt.Errorf("这不像 Webhook 地址：应形如 https://discord.com/api/webhooks/<id>/<token>")
	}
	return u, clientFor(c.Proxy), nil
}

// —— Requests ——

type apiError struct {
	Status  int
	Code    int
	Message string
}

func (e *apiError) Error() string {
	switch e.Status {
	case http.StatusNotFound:
		return "Discord 说这个 Webhook 不存在（404）：它可能被频道管理员删了，重新建一个并更新凭证"
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Sprintf("Discord 拒绝了这次请求（%d %s）：Webhook 地址不完整或已失效", e.Status, e.Message)
	case http.StatusTooManyRequests:
		return "Discord 限流（429）：同一个 Webhook 每 2 秒最多 5 条，降低频率或合并消息"
	case http.StatusBadRequest:
		return fmt.Sprintf("Discord 说这条消息不合法（400 code=%d）：%s", e.Code, e.Message)
	}
	if e.Message != "" {
		return fmt.Sprintf("Discord 报错 %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("Discord 返回 HTTP %d", e.Status)
}

func do(ctx plugin.Ctx, method, uri string, body io.Reader, ctype string, out any) error {
	_, hc, err := hookOf(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, uri, body)
	if err != nil {
		return err
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("连接 Discord 失败（在境外，部署环境可能要在凭证里配出站代理）: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 400 {
		e := &apiError{Status: resp.StatusCode}
		var b struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &b)
		e.Code, e.Message = b.Code, b.Message
		return e
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("Discord 应答无法解析: %w", err)
	}
	return nil
}

// —— Send message ——

type embed struct {
	Title       string       `json:"title,omitempty"`
	Description string       `json:"description,omitempty"`
	URL         string       `json:"url,omitempty"`
	Color       int          `json:"color,omitempty"`
	Fields      []embedField `json:"fields,omitempty"`
	Image       *struct {
		URL string `json:"url"`
	} `json:"image,omitempty"`
	Footer *struct {
		Text string `json:"text"`
	} `json:"footer,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}

type embedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type messageOut struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
	GuildID   string `json:"guild_id"`
}

func opMessageSend(ctx plugin.Ctx, in *DiscordMessageSendIn) (*DiscordMessageSendOut, error) {
	hook, _, err := hookOf(ctx)
	if err != nil {
		return nil, err
	}
	content := strings.TrimSpace(in.Content)
	if n := len([]rune(content)); n > maxContent {
		return nil, fmt.Errorf("正文 %d 个字符，超过 Discord 的 %d 上限（长内容放进卡片正文，那儿能到 4096）", n, maxContent)
	}
	payload := map[string]any{}
	if content != "" {
		payload["content"] = content
	}
	if u := strings.TrimSpace(in.Username); u != "" {
		payload["username"] = u
	}
	if e := buildEmbed(in); e != nil {
		payload["embeds"] = []any{e}
	}
	if payload["content"] == nil && payload["embeds"] == nil && len(in.Files) == 0 {
		return nil, fmt.Errorf("正文、卡片、附件至少要有一样")
	}

	// wait=true: without it, Discord replies with an empty 204 body and there's no message id, so
	// downstream edit/delete won't work.
	uri := hook + "?wait=true"
	if t := strings.TrimSpace(in.ThreadID); t != "" {
		uri += "&thread_id=" + url.QueryEscape(t)
	}

	var body io.Reader
	ctype := "application/json"
	if len(in.Files) > 0 {
		mb, mc, err := multipartPayload(ctx, payload, in.Files)
		if err != nil {
			return nil, err
		}
		body, ctype = bytes.NewReader(mb), mc
	} else {
		raw, _ := json.Marshal(payload)
		body = bytes.NewReader(raw)
	}

	var out messageOut
	if err := do(ctx, http.MethodPost, uri, body, ctype, &out); err != nil {
		return nil, err
	}
	return &DiscordMessageSendOut{
		ID: out.ID, ChannelID: out.ChannelID, URL: messageURL(out),
	}, nil
}

func opMessageDelete(ctx plugin.Ctx, in *DiscordMessageDeleteIn) (*DiscordMessageDeleteOut, error) {
	hook, _, err := hookOf(ctx)
	if err != nil {
		return nil, err
	}
	id := strings.TrimSpace(in.MessageID)
	if id == "" {
		return nil, fmt.Errorf("消息 id 是空的")
	}
	uri := hook + "/messages/" + id
	if t := strings.TrimSpace(in.ThreadID); t != "" {
		uri += "?thread_id=" + url.QueryEscape(t)
	}
	if err := do(ctx, http.MethodDelete, uri, nil, "", nil); err != nil {
		return nil, err
	}
	return &DiscordMessageDeleteOut{Deleted: true}, nil
}

func opHealthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	hook, _, err := hookOf(ctx)
	if err != nil {
		return &HealthCheckOut{OK: false, Message: err.Error()}, nil
	}
	// GET the webhook itself: if it's still alive, it returns its info; if deleted, 404.
	// **No test message is sent** — a health check shouldn't leave a trace in the channel.
	var out struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		ChannelID string `json:"channel_id"`
	}
	if err := do(ctx, http.MethodGet, hook, nil, "", &out); err != nil {
		return &HealthCheckOut{OK: false, Message: err.Error()}, nil
	}
	if out.ID == "" {
		return &HealthCheckOut{OK: false, Message: "Discord 没返回 Webhook 信息"}, nil
	}
	return &HealthCheckOut{OK: true, Channel: out.ChannelID, Message: "Webhook「" + out.Name + "」正常"}, nil
}

// —— Embed cards ——

func buildEmbed(in *DiscordMessageSendIn) *embed {
	title, desc := strings.TrimSpace(in.Title), strings.TrimSpace(in.Description)
	if title == "" && desc == "" && len(in.Fields) == 0 && strings.TrimSpace(in.ImageURL) == "" {
		return nil
	}
	if n := len([]rune(desc)); n > maxDescLen {
		desc = string([]rune(desc)[:maxDescLen])
	}
	e := &embed{Title: title, Description: desc, URL: strings.TrimSpace(in.URL),
		Color: parseColor(in.Color), Timestamp: time.Now().UTC().Format(time.RFC3339)}
	// Sort fields by key: map iteration order is random, so without sorting the same input would
	// produce a differently ordered card each time.
	for _, k := range sortedKeys(in.Fields) {
		e.Fields = append(e.Fields, embedField{Name: k, Value: in.Fields[k], Inline: true})
	}
	if u := strings.TrimSpace(in.ImageURL); u != "" {
		e.Image = &struct {
			URL string `json:"url"`
		}{URL: u}
	}
	if f := strings.TrimSpace(in.Footer); f != "" {
		e.Footer = &struct {
			Text string `json:"text"`
		}{Text: f}
	}
	return e
}

// parseColor: #2b6cb0 / 2b6cb0 → decimal. Returns 0 (Discord's default color) if unrecognized.
func parseColor(s string) int {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if s == "" {
		return 0
	}
	n, err := strconv.ParseInt(s, 16, 32)
	if err != nil {
		return 0
	}
	return int(n)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ { // small map, insertion sort is enough
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// multipartPayload: attachments + JSON payload. The JSON must go in a field named payload_json.
func multipartPayload(ctx plugin.Ctx, payload map[string]any, files []*plugin.File) ([]byte, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	raw, _ := json.Marshal(payload)
	if err := w.WriteField("payload_json", string(raw)); err != nil {
		return nil, "", err
	}
	for i, f := range files {
		if f == nil || f.ID == "" {
			continue
		}
		data, err := ctx.Fetch(f)
		if err != nil {
			return nil, "", fmt.Errorf("取第 %d 个附件失败: %w", i+1, err)
		}
		name := f.Name
		if name == "" {
			name = fmt.Sprintf("file%d", i)
		}
		part, err := w.CreateFormFile(fmt.Sprintf("files[%d]", i), name)
		if err != nil {
			return nil, "", err
		}
		if _, err := part.Write(data); err != nil {
			return nil, "", err
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}

func messageURL(m messageOut) string {
	if m.ID == "" || m.ChannelID == "" {
		return ""
	}
	guild := m.GuildID
	if guild == "" {
		guild = "@me"
	}
	return "https://discord.com/channels/" + guild + "/" + m.ChannelID + "/" + m.ID
}
