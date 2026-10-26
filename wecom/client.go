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
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const weComAPIBase = "https://qyapi.weixin.qq.com"

// Delivery reports API acceptance, not whether the recipient read the message.
// errcode=0 can still include rejected recipients; callers must inspect these fields.
type Delivery struct {
	MsgID          string `json:"msg_id"`
	InvalidUser    string `json:"invalid_user"`
	InvalidParty   string `json:"invalid_party"`
	InvalidTag     string `json:"invalid_tag"`
	UnlicensedUser string `json:"unlicensed_user"`
}

type weComToken struct {
	value   string
	expires time.Time
}

type weComRefresh struct {
	done  chan struct{}
	token string
	err   error
}

type weComVerification struct {
	done chan struct{}
	err  error
}

// WeComClient owns a credential-isolated token cache. The API host is deliberately
// not configurable through workflow inputs or credentials.
type WeComClient struct {
	httpClient *http.Client
	baseURL    string
	now        func() time.Time
	mu         sync.Mutex
	tokens     map[[32]byte]weComToken
	refreshes  map[[32]byte]*weComRefresh
	verified   map[[32]byte]time.Time
	verifying  map[[32]byte]*weComVerification
}

func NewWeComClient() *WeComClient {
	return &WeComClient{
		httpClient: &http.Client{
			Timeout: 8 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		baseURL:   weComAPIBase,
		now:       time.Now,
		tokens:    make(map[[32]byte]weComToken),
		refreshes: make(map[[32]byte]*weComRefresh),
		verified:  make(map[[32]byte]time.Time),
		verifying: make(map[[32]byte]*weComVerification),
	}
}

func normalizedWeComCred(cred Cred) (Cred, int64, error) {
	cred.CorpID = strings.TrimSpace(cred.CorpID)
	cred.AgentID = strings.TrimSpace(cred.AgentID)
	cred.Secret = strings.TrimSpace(cred.Secret)
	if cred.CorpID == "" || cred.AgentID == "" || cred.Secret == "" {
		return cred, 0, errors.New("企业微信凭证缺少 CorpID、AgentID 或 Secret")
	}
	agentID, err := strconv.ParseInt(cred.AgentID, 10, 64)
	if err != nil || agentID <= 0 {
		return cred, 0, errors.New("企业微信 AgentID 必须是正整数")
	}
	return cred, agentID, nil
}

func weComCredentialKey(cred Cred) [32]byte {
	// Encoding an array avoids delimiter collisions without storing the secret as a map key.
	b, _ := json.Marshal([3]string{cred.CorpID, cred.AgentID, cred.Secret})
	return sha256.Sum256(b)
}

// VerifyApplication authenticates access to the claimed application before a
// caller opens persisted data keyed by CorpID and AgentID. Merely obtaining a
// token is insufficient: a different application in the same corporation has a
// valid Secret too. Official document/path/90227 specifies that an enterprise
// credential can only retrieve its own application through /cgi-bin/agent/get.
// Successful verification is cached for at most five minutes per full credential.
func (c *WeComClient) VerifyApplication(ctx context.Context, cred Cred) error {
	cred, agentID, err := normalizedWeComCred(cred)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	key := weComCredentialKey(cred)
	c.mu.Lock()
	if expires, ok := c.verified[key]; ok && c.now().Before(expires) {
		c.mu.Unlock()
		return nil
	}
	if pending, ok := c.verifying[key]; ok {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-pending.done:
			return pending.err
		}
	}
	pending := &weComVerification{done: make(chan struct{})}
	c.verifying[key] = pending
	c.mu.Unlock()

	started := c.now()
	err = c.verifyApplication(ctx, cred, agentID)
	c.mu.Lock()
	if err == nil {
		c.verified[key] = started.Add(5 * time.Minute)
	} else {
		delete(c.verified, key)
	}
	pending.err = err
	delete(c.verifying, key)
	close(pending.done)
	c.mu.Unlock()
	return err
}

func (c *WeComClient) verifyApplication(ctx context.Context, cred Cred, agentID int64) error {
	b, err := c.authenticated(ctx, cred, http.MethodGet, "/cgi-bin/agent/get", url.Values{
		"agentid": {strconv.FormatInt(agentID, 10)},
	}, nil)
	if err != nil {
		return err
	}
	var out struct {
		AgentID *int64 `json:"agentid"`
	}
	if err := json.Unmarshal(b, &out); err != nil || out.AgentID == nil || *out.AgentID != agentID {
		return errors.New("企业微信应用验证失败：响应中的 AgentID 与凭证不一致或格式错误")
	}
	return nil
}

func (c *WeComClient) AccessToken(ctx context.Context, cred Cred) (string, error) {
	cred, _, err := normalizedWeComCred(cred)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	key := weComCredentialKey(cred)
	c.mu.Lock()
	if cached, ok := c.tokens[key]; ok && c.now().Before(cached.expires) {
		c.mu.Unlock()
		return cached.value, nil
	}
	if pending, ok := c.refreshes[key]; ok {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-pending.done:
			return pending.token, pending.err
		}
	}
	pending := &weComRefresh{done: make(chan struct{})}
	c.refreshes[key] = pending
	c.mu.Unlock()

	token, err := c.fetchToken(ctx, cred)
	c.mu.Lock()
	if err == nil {
		c.tokens[key] = token
	}
	pending.token, pending.err = token.value, err
	delete(c.refreshes, key)
	close(pending.done)
	c.mu.Unlock()
	return token.value, err
}

func (c *WeComClient) fetchToken(ctx context.Context, cred Cred) (weComToken, error) {
	started := c.now()
	b, err := c.request(ctx, http.MethodGet, "/cgi-bin/gettoken", url.Values{
		"corpid": {cred.CorpID}, "corpsecret": {cred.Secret},
	}, nil)
	if err != nil {
		return weComToken{}, err
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return weComToken{}, errors.New("企业微信 token 响应字段格式错误")
	}
	// Bound the duration before multiplication to prevent overflow on bad responses.
	if strings.TrimSpace(out.AccessToken) == "" || out.ExpiresIn <= 0 || out.ExpiresIn > 30*24*60*60 {
		return weComToken{}, errors.New("企业微信 token 响应缺少有效 access_token 或 expires_in")
	}
	ttl := time.Duration(out.ExpiresIn) * time.Second
	margin := min(2*time.Minute, ttl/10)
	return weComToken{value: out.AccessToken, expires: started.Add(ttl - margin)}, nil
}

func (c *WeComClient) invalidateToken(cred Cred, rejected string) {
	cred, _, _ = normalizedWeComCred(cred)
	key := weComCredentialKey(cred)
	c.mu.Lock()
	defer c.mu.Unlock()
	// A late failure must not evict a token already refreshed by another caller.
	if cached, ok := c.tokens[key]; ok && cached.value == rejected {
		delete(c.tokens, key)
		delete(c.verified, key)
	}
}

func (c *WeComClient) authenticated(ctx context.Context, cred Cred, method, path string, query url.Values, body []byte) ([]byte, error) {
	if query == nil {
		query = make(url.Values)
	}
	for attempt := 0; attempt < 2; attempt++ {
		token, err := c.AccessToken(ctx, cred)
		if err != nil {
			return nil, err
		}
		query.Set("access_token", token)
		b, err := c.request(ctx, method, path, query, body)
		if isWeComTokenError(err) {
			c.invalidateToken(cred, token)
			if attempt == 0 {
				continue
			}
		}
		return b, err
	}
	panic("unreachable")
}

type weComAPIError struct {
	code   int
	status int
}

func (e *weComAPIError) Error() string {
	if e.status != 0 {
		return fmt.Sprintf("企业微信接口返回 HTTP %d", e.status)
	}
	hint := "请根据官方错误码检查应用配置和权限"
	switch e.code {
	case 40001, 40014, 42001:
		hint = "访问令牌无效或已过期，请检查应用 Secret"
	case 40013:
		hint = "请检查企业 CorpID"
	case 40029:
		hint = "网页授权 code 无效或已使用，请重新打开告警链接"
	case 40003, 81013:
		hint = "接收成员不存在、不可见或无接口权限，请检查应用可见范围"
	case 60020:
		hint = "请把插件服务器的公网出口 IP 加入应用可信 IP"
	case 50001:
		hint = "请检查网页授权回调地址与应用可信域名是否匹配"
	case 45009:
		hint = "接口调用超过频率限制，请稍后再试"
	case 46003:
		hint = "当前应用尚未创建自定义菜单，请先设置菜单"
	}
	// Upstream errmsg may echo secrets or a complete request URL; never surface it.
	return fmt.Sprintf("企业微信错误码 %d：%s", e.code, hint)
}

func isWeComTokenError(err error) bool {
	var apiErr *weComAPIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.status == http.StatusUnauthorized || apiErr.code == 40001 || apiErr.code == 40014 || apiErr.code == 42001
}

func (c *WeComClient) request(ctx context.Context, method, path string, query url.Values, body []byte) ([]byte, error) {
	requestURL := c.baseURL + path
	if len(query) > 0 {
		requestURL += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, requestURL, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("无法构造企业微信请求")
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("企业微信请求已取消：%w", ctx.Err())
		}
		var networkErr net.Error
		if errors.As(err, &networkErr) && networkErr.Timeout() {
			return nil, errors.New("企业微信请求超时；发送结果未知，请先核实后再重试")
		}
		// net/http's error includes the URL, which contains access_token/code/secret.
		return nil, errors.New("企业微信网络请求失败；发送结果未知，请检查网络后再核实")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &weComAPIError{status: resp.StatusCode}
	}
	const maxResponseBytes = 1024 * 1024
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, errors.New("读取企业微信响应失败；发送结果未知")
	}
	if len(b) > maxResponseBytes {
		return nil, errors.New("企业微信响应超过大小限制；发送结果未知")
	}
	var envelope struct {
		ErrCode *int `json:"errcode"`
	}
	if err := json.Unmarshal(b, &envelope); err != nil {
		return nil, errors.New("企业微信响应不是包含 errcode 的有效 JSON；发送结果未知")
	}
	if envelope.ErrCode != nil && *envelope.ErrCode != 0 {
		return nil, &weComAPIError{code: *envelope.ErrCode}
	}
	if method == http.MethodGet && path == "/cgi-bin/menu/get" {
		// Document 90232 describes the success body as the menu/create payload,
		// which may omit errcode. Accept only an explicit array of button objects;
		// a present but null errcode must never activate this compatibility path.
		object, err := apiObject(b)
		if err != nil {
			return nil, errors.New("企业微信菜单响应不是有效的 JSON 对象")
		}
		errorCodeFields := 0
		for field := range object {
			if strings.EqualFold(field, "errcode") {
				errorCodeFields++
				if envelope.ErrCode == nil || errorCodeFields > 1 {
					return nil, errors.New("企业微信菜单响应 errcode 格式错误")
				}
			}
		}
		var buttons []map[string]json.RawMessage
		if err := json.Unmarshal(object["button"], &buttons); err != nil || buttons == nil {
			return nil, errors.New("企业微信菜单响应缺少有效 button 数组")
		}
		for _, button := range buttons {
			if button == nil {
				return nil, errors.New("企业微信菜单响应包含无效按钮对象")
			}
		}
		return b, nil
	}
	if envelope.ErrCode == nil {
		return nil, errors.New("企业微信响应不是包含 errcode 的有效 JSON；发送结果未知")
	}
	return b, nil
}

type weComMessage struct {
	ToUser   string         `json:"touser"`
	MsgType  string         `json:"msgtype"`
	AgentID  int64          `json:"agentid"`
	Text     *weComText     `json:"text,omitempty"`
	TextCard *weComTextCard `json:"textcard,omitempty"`
}

type weComText struct {
	Content string `json:"content"`
}

type weComTextCard struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Button      string `json:"btntxt,omitempty"`
}

func weComRecipients(users []string) (string, error) {
	if len(users) == 0 || len(users) > 1000 {
		return "", errors.New("接收成员数量必须为 1–1000")
	}
	for _, user := range users {
		// Local member IDs: official document/path/90195 specifies 1–64 ASCII
		// bytes, an alphanumeric first character, then alphanumerics or _-@.
		if len(user) == 0 || len(user) > 64 {
			return "", errors.New("接收成员 UserID 必须为 1–64 字节")
		}
		for i := 0; i < len(user); i++ {
			ch := user[i]
			alphanumeric := ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9'
			if !alphanumeric && (i == 0 || !strings.ContainsRune("_-@.", rune(ch))) {
				return "", errors.New("成员 UserID 须以数字或字母开头，且仅含数字、字母、下划线、连字符、@ 和点；不支持 @all")
			}
		}
	}
	return strings.Join(users, "|"), nil
}

func (c *WeComClient) SendText(ctx context.Context, cred Cred, users []string, text string) (Delivery, error) {
	if strings.TrimSpace(text) == "" || !utf8.ValidString(text) || len(text) > 2048 {
		return Delivery{}, errors.New("文本消息不能为空，且 UTF-8 长度不能超过 2048 字节")
	}
	return c.sendMessage(ctx, cred, users, weComMessage{MsgType: "text", Text: &weComText{Content: text}})
}

// SendCard receives an already escaped description. WeCom allows a small HTML
// subset here; the caller is responsible for escaping all user-provided content.
func (c *WeComClient) SendCard(ctx context.Context, cred Cred, users []string, title, description, link, button string) (Delivery, error) {
	if strings.TrimSpace(title) == "" || !utf8.ValidString(title) || utf8.RuneCountInString(title) > 128 {
		return Delivery{}, errors.New("卡片标题不能为空，且不能超过 128 个字符")
	}
	if strings.TrimSpace(description) == "" || !utf8.ValidString(description) || utf8.RuneCountInString(description) > 512 {
		return Delivery{}, errors.New("卡片描述不能为空，且不能超过 512 个字符（含 HTML）")
	}
	u, err := url.Parse(link)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || len(link) > 2048 {
		return Delivery{}, errors.New("卡片链接必须是有效的 HTTP(S) 地址，且不能超过 2048 字节")
	}
	if !utf8.ValidString(button) || utf8.RuneCountInString(button) > 4 {
		return Delivery{}, errors.New("卡片按钮文字不能超过 4 个字符")
	}
	card := &weComTextCard{Title: title, Description: description, URL: link, Button: button}
	return c.sendMessage(ctx, cred, users, weComMessage{MsgType: "textcard", TextCard: card})
}

func (c *WeComClient) sendMessage(ctx context.Context, cred Cred, users []string, message weComMessage) (Delivery, error) {
	cred, agentID, err := normalizedWeComCred(cred)
	if err != nil {
		return Delivery{}, err
	}
	targets, err := weComRecipients(users)
	if err != nil {
		return Delivery{}, err
	}
	message.ToUser, message.AgentID = targets, agentID
	body, err := json.Marshal(message)
	if err != nil {
		return Delivery{}, errors.New("无法编码企业微信消息")
	}
	b, err := c.authenticated(ctx, cred, http.MethodPost, "/cgi-bin/message/send", nil, body)
	if err != nil {
		return Delivery{}, err
	}
	var out struct {
		MsgID          string `json:"msgid"`
		InvalidUser    string `json:"invaliduser"`
		InvalidParty   string `json:"invalidparty"`
		InvalidTag     string `json:"invalidtag"`
		UnlicensedUser string `json:"unlicenseduser"`
	}
	if err := json.Unmarshal(b, &out); err != nil || strings.TrimSpace(out.MsgID) == "" {
		return Delivery{}, errors.New("企业微信发送响应缺少有效 msgid 或字段格式错误；发送结果未知")
	}
	return Delivery{out.MsgID, out.InvalidUser, out.InvalidParty, out.InvalidTag, out.UnlicensedUser}, nil
}

// UserID uses the current self-built-app identity endpoint documented at
// https://developer.work.weixin.qq.com/document/path/91023 (updated 2026-04-22).
// External openid/external_userid values never establish member identity.
func (c *WeComClient) UserID(ctx context.Context, cred Cred, code string) (string, error) {
	if strings.TrimSpace(code) == "" || len(code) > 512 {
		return "", errors.New("网页授权 code 不能为空，且不能超过 512 字节")
	}
	b, err := c.authenticated(ctx, cred, http.MethodGet, "/cgi-bin/auth/getuserinfo", url.Values{"code": {code}}, nil)
	if err != nil {
		return "", err
	}
	var out struct {
		UserID string `json:"userid"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", errors.New("企业微信身份响应字段格式错误")
	}
	if strings.TrimSpace(out.UserID) == "" {
		return "", errors.New("当前微信身份不是本企业成员，请使用已绑定企业通讯录的微信账号")
	}
	return out.UserID, nil
}
