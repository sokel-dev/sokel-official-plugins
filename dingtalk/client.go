package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const maxResponse = 4 << 20

type cachedToken struct {
	value   string
	expires time.Time
}
type tokenCache struct {
	mu      sync.Mutex
	entries map[[32]byte]cachedToken
}

var tokens = &tokenCache{entries: make(map[[32]byte]cachedToken)}

type client struct {
	base  string
	http  *http.Client
	cred  Cred
	cache *tokenCache
}

func newClient(base string, cred Cred) *client {
	return &client{base: strings.TrimRight(base, "/"), cred: cred, cache: tokens, http: &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (c *client) request(ctx context.Context, path, token string, body any, out any) error {
	base, err := url.Parse(c.base)
	if err != nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Scheme != "https" && base.Scheme != "http") {
		return errors.New("invalid API base URL")
	}
	host := base.Hostname()
	loopback := host == "localhost"
	if ip := net.ParseIP(host); ip != nil {
		loopback = ip.IsLoopback()
	}
	if base.Scheme != "https" && !loopback {
		return errors.New("API base URL must use HTTPS; HTTP is allowed only on loopback")
	}
	b, err := json.Marshal(body)
	if err != nil {
		return errors.New("could not encode request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(b))
	if err != nil {
		return errors.New("could not create request")
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("x-acs-dingtalk-access-token", token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("DingTalk request failed; verify delivery before retrying a write")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return errors.New("could not read DingTalk response")
	}
	if len(raw) > maxResponse {
		return errors.New("DingTalk response exceeds 4 MiB")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("DingTalk HTTP %d; check permissions or rate limits before retrying", resp.StatusCode)
	}
	var envelope struct {
		Code    json.RawMessage `json:"code"`
		ErrCode *int            `json:"errcode"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return errors.New("invalid DingTalk JSON response")
	}
	if len(envelope.Code) > 0 && string(envelope.Code) != "0" && string(envelope.Code) != "null" && string(envelope.Code) != `""` {
		return errors.New("DingTalk rejected the request; verify credentials and permissions")
	}
	if envelope.ErrCode != nil && *envelope.ErrCode != 0 {
		return fmt.Errorf("DingTalk business error %d", *envelope.ErrCode)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return errors.New("unexpected DingTalk response shape")
	}
	return nil
}
func (c *client) accessToken(ctx context.Context, force bool) (string, error) {
	if strings.TrimSpace(c.cred.AppKey) == "" || strings.TrimSpace(c.cred.AppSecret) == "" || strings.TrimSpace(c.cred.RobotCode) == "" {
		return "", errors.New("app_key, app_secret and robot_code are required")
	}
	key := sha256.Sum256([]byte(c.base + "\x00" + c.cred.AppKey + "\x00" + c.cred.AppSecret))
	if c.cache != nil && !force {
		c.cache.mu.Lock()
		v, ok := c.cache.entries[key]
		c.cache.mu.Unlock()
		if ok && time.Now().Before(v.expires) {
			return v.value, nil
		}
	}
	var out struct {
		AccessToken string `json:"accessToken"`
		ExpireIn    int    `json:"expireIn"`
	}
	err := c.request(ctx, "/v1.0/oauth2/accessToken", "", map[string]string{"appKey": c.cred.AppKey, "appSecret": c.cred.AppSecret}, &out)
	if err != nil {
		return "", err
	}
	if out.AccessToken == "" || out.ExpireIn <= 0 {
		return "", errors.New("DingTalk returned no valid token")
	}
	if c.cache != nil {
		c.cache.mu.Lock()
		if c.cache.entries == nil {
			c.cache.entries = make(map[[32]byte]cachedToken)
		}
		for k, v := range c.cache.entries {
			if time.Now().After(v.expires) {
				delete(c.cache.entries, k)
			}
		}
		if len(c.cache.entries) < 1024 {
			ttl := out.ExpireIn
			if ttl > 86400 {
				ttl = 86400
			}
			c.cache.entries[key] = cachedToken{out.AccessToken, time.Now().Add(time.Duration(ttl)*time.Second - time.Minute)}
		}
		c.cache.mu.Unlock()
	}
	return out.AccessToken, nil
}
func messagePayload(kind, text, title string) (map[string]any, error) {
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("text is required")
	}
	if kind == "" {
		kind = "text"
	}
	var param map[string]string
	var key string
	switch kind {
	case "text":
		key = "sampleText"
		param = map[string]string{"content": text}
	case "markdown":
		if strings.TrimSpace(title) == "" {
			return nil, errors.New("Markdown requires title")
		}
		key = "sampleMarkdown"
		param = map[string]string{"text": text, "title": title}
	default:
		return nil, errors.New("message_type must be text or markdown")
	}
	raw, _ := json.Marshal(param)
	if len(raw) > 15000 {
		return nil, errors.New("encoded message exceeds 15000 bytes")
	}
	return map[string]any{"msgKey": key, "msgParam": string(raw)}, nil
}
func (c *client) GroupMessageSend(ctx context.Context, in *GroupMessageSendIn) (*GroupMessageSendOut, error) {
	if strings.TrimSpace(in.ConversationID) == "" {
		return nil, errors.New("conversation_id is required")
	}
	body, err := messagePayload(in.MessageType, in.Text, in.Title)
	if err != nil {
		return nil, err
	}
	token, err := c.accessToken(ctx, false)
	if err != nil {
		return nil, err
	}
	body["openConversationId"] = in.ConversationID
	body["robotCode"] = c.cred.RobotCode
	var out struct {
		Key string `json:"processQueryKey"`
	}
	if err = c.request(ctx, "/v1.0/robot/groupMessages/send", token, body, &out); err != nil {
		return nil, err
	}
	if out.Key == "" {
		return nil, errors.New("DingTalk returned no delivery receipt")
	}
	return &GroupMessageSendOut{ProcessQueryKey: out.Key}, nil
}
func (c *client) DirectMessageSend(ctx context.Context, in *DirectMessageSendIn) (*DirectMessageSendOut, error) {
	if len(in.UserIDs) < 1 || len(in.UserIDs) > 20 {
		return nil, errors.New("user_ids must contain 1 to 20 users")
	}
	seen := map[string]bool{}
	for _, id := range in.UserIDs {
		if strings.TrimSpace(id) == "" || seen[id] {
			return nil, errors.New("user_ids must be nonempty and unique")
		}
		seen[id] = true
	}
	body, err := messagePayload(in.MessageType, in.Text, in.Title)
	if err != nil {
		return nil, err
	}
	token, err := c.accessToken(ctx, false)
	if err != nil {
		return nil, err
	}
	body["userIds"] = in.UserIDs
	body["robotCode"] = c.cred.RobotCode
	var out struct {
		Key     string   `json:"processQueryKey"`
		Invalid []string `json:"invalidStaffIdList"`
		Limited []string `json:"flowControlledStaffIdList"`
	}
	if err = c.request(ctx, "/v1.0/robot/oToMessages/batchSend", token, body, &out); err != nil {
		return nil, err
	}
	if out.Key == "" {
		return nil, errors.New("DingTalk returned no delivery receipt")
	}
	return &DirectMessageSendOut{ProcessQueryKey: out.Key, InvalidUserIDs: out.Invalid, RateLimitedUserIDs: out.Limited}, nil
}
func (c *client) MessageFileURL(ctx context.Context, in *MessageFileURLIn) (*MessageFileURLOut, error) {
	if strings.TrimSpace(in.DownloadCode) == "" {
		return nil, errors.New("download_code is required")
	}
	token, err := c.accessToken(ctx, false)
	if err != nil {
		return nil, err
	}
	var out struct {
		URL string `json:"downloadUrl"`
	}
	if err = c.request(ctx, "/v1.0/robot/messageFiles/download", token, map[string]string{"downloadCode": in.DownloadCode, "robotCode": c.cred.RobotCode}, &out); err != nil {
		return nil, err
	}
	u, err := url.Parse(out.URL)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errors.New("DingTalk returned no valid download URL")
	}
	return &MessageFileURLOut{DownloadURL: out.URL}, nil
}
func (c *client) HealthCheck(ctx context.Context, _ *HealthCheckIn) (*HealthCheckOut, error) {
	_, err := c.accessToken(ctx, true)
	if err != nil {
		return &HealthCheckOut{OK: false, Message: err.Error()}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Token issuance succeeded; recipient permissions are checked when sending."}, nil
}
