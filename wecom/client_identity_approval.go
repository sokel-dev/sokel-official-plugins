package main

import (
	"context"
	"crypto/rand"
	"crypto/sha1" // SHA-1 is required by the upstream JS-SDK signing protocol.
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// WeComJSConfig contains public signatures only. The tickets and access token
// remain on the server. Both signatures use exactly SignedURL without a fragment.
type WeComJSConfig struct {
	CorpID         string `json:"corp_id"`
	AgentID        int64  `json:"agent_id"`
	NonceStr       string `json:"nonce_str"`
	Timestamp      int64  `json:"timestamp"`
	Signature      string `json:"signature"`
	AgentSignature string `json:"agent_signature"`
	SignedURL      string `json:"signed_url"`
}

type weComJSTicketKey struct {
	credential [32]byte
	agent      bool
}

// WeComIdentityClient owns the additional identity ticket cache while sharing
// the application's credential-isolated access token cache and HTTP transport.
type WeComIdentityClient struct {
	client    *WeComClient
	mu        sync.Mutex
	tickets   map[weComJSTicketKey]weComToken
	refreshes map[weComJSTicketKey]*weComRefresh
}

func NewWeComIdentityClient(client *WeComClient) *WeComIdentityClient {
	return &WeComIdentityClient{client: client, tickets: make(map[weComJSTicketKey]weComToken), refreshes: make(map[weComJSTicketKey]*weComRefresh)}
}

// JSConfig follows official document/path/90506 and /90514. pageURL must be the
// exact browser URL; parsing it must not normalize escaping, query order, or case.
// A public HTTP handler calling this method must separately authorize its caller
// and restrict the signed host to its configured trusted domain.
func (c *WeComIdentityClient) JSConfig(ctx context.Context, cred Cred, pageURL string) (WeComJSConfig, error) {
	cred, agentID, err := normalizedWeComCred(cred)
	if err != nil {
		return WeComJSConfig{}, err
	}
	signedURL, err := weComIdentityURL(pageURL, true)
	if err != nil {
		return WeComJSConfig{}, err
	}
	if err := c.client.VerifyApplication(ctx, cred); err != nil {
		return WeComJSConfig{}, err
	}
	company, err := c.jsTicket(ctx, cred, false)
	if err != nil {
		return WeComJSConfig{}, err
	}
	agent, err := c.jsTicket(ctx, cred, true)
	if err != nil {
		return WeComJSConfig{}, err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return WeComJSConfig{}, errors.New("无法生成 JS-SDK 签名随机串")
	}
	out := WeComJSConfig{CorpID: cred.CorpID, AgentID: agentID, NonceStr: hex.EncodeToString(nonce[:]), Timestamp: c.client.now().Unix(), SignedURL: signedURL}
	out.Signature = weComJSSignature(company, out.NonceStr, out.Timestamp, signedURL)
	out.AgentSignature = weComJSSignature(agent, out.NonceStr, out.Timestamp, signedURL)
	return out, nil
}

func weComJSSignature(ticket, nonce string, timestamp int64, pageURL string) string {
	content := "jsapi_ticket=" + ticket + "&noncestr=" + nonce + "&timestamp=" + strconv.FormatInt(timestamp, 10) + "&url=" + pageURL
	sum := sha1.Sum([]byte(content))
	return hex.EncodeToString(sum[:])
}

func (c *WeComIdentityClient) jsTicket(ctx context.Context, cred Cred, agent bool) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	key := weComJSTicketKey{credential: weComCredentialKey(cred), agent: agent}
	c.mu.Lock()
	if cached, ok := c.tickets[key]; ok && c.client.now().Before(cached.expires) {
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

	ticket, err := c.fetchJSTicket(ctx, cred, agent)
	c.mu.Lock()
	if err == nil {
		// Bound memory even when a platform repeatedly rotates credentials.
		if len(c.tickets) >= 1024 {
			var oldest weComJSTicketKey
			var oldestExpiry time.Time
			for k, v := range c.tickets {
				if oldestExpiry.IsZero() || v.expires.Before(oldestExpiry) {
					oldest, oldestExpiry = k, v.expires
				}
			}
			delete(c.tickets, oldest)
		}
		c.tickets[key] = ticket
	}
	pending.token, pending.err = ticket.value, err
	delete(c.refreshes, key)
	close(pending.done)
	c.mu.Unlock()
	return ticket.value, err
}

func (c *WeComIdentityClient) fetchJSTicket(ctx context.Context, cred Cred, agent bool) (weComToken, error) {
	started := c.client.now()
	path := "/cgi-bin/get_jsapi_ticket"
	var query url.Values
	if agent {
		path, query = "/cgi-bin/ticket/get", url.Values{"type": {"agent_config"}}
	}
	b, err := c.client.authenticated(ctx, cred, http.MethodGet, path, query, nil)
	if err != nil {
		return weComToken{}, err
	}
	var out struct {
		Ticket    string `json:"ticket"`
		ExpiresIn int64  `json:"expires_in"`
	}
	if json.Unmarshal(b, &out) != nil || strings.TrimSpace(out.Ticket) == "" || len(out.Ticket) > 512 || out.ExpiresIn <= 0 || out.ExpiresIn > 30*24*60*60 {
		return weComToken{}, errors.New("企业微信 JS-SDK 票据响应缺少有效 ticket 或 expires_in")
	}
	ttl := time.Duration(out.ExpiresIn) * time.Second
	return weComToken{value: out.Ticket, expires: started.Add(ttl - min(2*time.Minute, ttl/10))}, nil
}

// BuildWebLoginURL uses the current official Web login endpoint (/98152).
// Callers must bind state to the initiating browser session and verify it once
// before exchanging the returned code. This helper alone does not create a login
// session. In-app ordinary WeChat pages continue using snsapi_base OAuth instead.
func BuildWebLoginURL(cred Cred, redirectURL, state, language string) (string, error) {
	cred, agentID, err := normalizedWeComCred(cred)
	if err != nil {
		return "", err
	}
	if _, err := weComIdentityURL(redirectURL, false); err != nil {
		return "", err
	}
	if len(state) < 16 || len(state) > 128 || strings.ContainsAny(state, "\r\n\x00") {
		return "", errors.New("登录 state 必须为 16–128 字节随机串，并在回调时与发起会话校验")
	}
	if language == "" {
		language = "zh"
	}
	if language != "zh" && language != "en" {
		return "", errors.New("登录语言仅支持 zh 或 en")
	}
	query := url.Values{"login_type": {"CorpApp"}, "appid": {cred.CorpID}, "agentid": {strconv.FormatInt(agentID, 10)}, "redirect_uri": {redirectURL}, "state": {state}, "lang": {language}}
	return "https://login.work.weixin.qq.com/wwlogin/sso/login?" + query.Encode(), nil
}

func weComIdentityURL(raw string, stripFragment bool) (string, error) {
	if len(raw) == 0 || len(raw) > 8192 || strings.TrimSpace(raw) != raw || strings.ContainsAny(raw, "\r\n\x00") {
		return "", errors.New("网页地址必须为有效的 HTTPS 完整地址")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Opaque != "" {
		return "", errors.New("网页地址必须为有效的 HTTPS 完整地址，且不能包含用户名密码")
	}
	if !stripFragment && strings.Contains(raw, "#") {
		return "", errors.New("登录回调地址不能包含 # 片段")
	}
	return strings.SplitN(raw, "#", 2)[0], nil
}

// Approval operations below address the separate enterprise Approval app. They
// use this self-built application's Secret after an administrator grants access
// under 审批 → 可调用接口的应用. They do not create or submit the flow-engine
// templates found on the self-built application's own 审批接口 settings page.
// Official docs: /91854, /91982, /97437, /97438, /91853, /91983, /91816.
func (c *WeComClient) ApprovalTemplate(ctx context.Context, cred Cred, templateID string) (json.RawMessage, error) {
	if !weComApprovalID(templateID) {
		return nil, errors.New("审批模板 ID 不能为空或超过 256 字节")
	}
	body, _ := json.Marshal(struct {
		TemplateID string `json:"template_id"`
	}{templateID})
	return c.approvalCall(ctx, cred, "/cgi-bin/oa/gettemplatedetail", body)
}

func (c *WeComClient) CreateApprovalTemplate(ctx context.Context, cred Cred, input json.RawMessage) (string, error) {
	if _, err := weComApprovalTemplatePayload(input, false); err != nil {
		return "", err
	}
	b, err := c.approvalCall(ctx, cred, "/cgi-bin/oa/approval/create_template", input)
	if err != nil {
		return "", err
	}
	var out struct {
		TemplateID string `json:"template_id"`
	}
	if json.Unmarshal(b, &out) != nil || !weComApprovalID(out.TemplateID) {
		return "", errors.New("企业微信创建审批模板响应缺少有效 template_id；结果未知，请核实后再重试")
	}
	return out.TemplateID, nil
}

func (c *WeComClient) UpdateApprovalTemplate(ctx context.Context, cred Cred, input json.RawMessage) error {
	if _, err := weComApprovalTemplatePayload(input, true); err != nil {
		return err
	}
	_, err := c.approvalCall(ctx, cred, "/cgi-bin/oa/approval/update_template", input)
	return err
}

func weComApprovalTemplatePayload(input json.RawMessage, update bool) (map[string]json.RawMessage, error) {
	obj, err := weComApprovalObject(input)
	if err != nil {
		return nil, err
	}
	var templateID string
	if update && (json.Unmarshal(obj["template_id"], &templateID) != nil || !weComApprovalID(templateID)) {
		return nil, errors.New("更新审批模板需要有效 template_id")
	}
	var names []struct {
		Text string `json:"text"`
		Lang string `json:"lang"`
	}
	if json.Unmarshal(obj["template_name"], &names) != nil || len(names) == 0 {
		return nil, errors.New("审批模板需要 template_name 多语言名称数组")
	}
	for _, name := range names {
		if strings.TrimSpace(name.Text) == "" || len([]rune(name.Text)) > 40 || name.Lang != "zh_CN" {
			return nil, errors.New("审批模板名称须为 1–40 个字符，lang 必须为 zh_CN")
		}
	}
	var content struct {
		Controls []json.RawMessage `json:"controls"`
	}
	if json.Unmarshal(obj["template_content"], &content) != nil || len(content.Controls) == 0 {
		return nil, errors.New("审批模板需要非空 template_content.controls 控件数组")
	}
	return obj, nil
}

func (c *WeComClient) ApplyApproval(ctx context.Context, cred Cred, input json.RawMessage) (string, error) {
	obj, err := weComApprovalObject(input)
	if err != nil {
		return "", err
	}
	var fields struct {
		CreatorUserID       string `json:"creator_userid"`
		TemplateID          string `json:"template_id"`
		UseTemplateApprover *int   `json:"use_template_approver"`
	}
	if json.Unmarshal(input, &fields) != nil || !weComApprovalID(fields.TemplateID) {
		return "", errors.New("审批申请需要有效 template_id")
	}
	if _, err := weComRecipients([]string{fields.CreatorUserID}); err != nil {
		return "", errors.New("审批申请需要有效的 creator_userid 成员账号")
	}
	if fields.UseTemplateApprover == nil || (*fields.UseTemplateApprover != 0 && *fields.UseTemplateApprover != 1) {
		return "", errors.New("审批申请 use_template_approver 必须明确为 0（自定义流程）或 1（模板流程）")
	}
	if *fields.UseTemplateApprover == 0 {
		var process struct {
			Nodes []json.RawMessage `json:"node_list"`
		}
		processOK := json.Unmarshal(obj["process"], &process) == nil && len(process.Nodes) > 0
		if !processOK {
			return "", errors.New("自定义审批流程需要 process.node_list 流程节点列表")
		}
	}
	var data struct {
		Contents []json.RawMessage `json:"contents"`
	}
	if json.Unmarshal(obj["apply_data"], &data) != nil || data.Contents == nil {
		return "", errors.New("审批申请需要 apply_data.contents 控件值数组")
	}
	var summaries []struct {
		Info []struct {
			Text string `json:"text"`
			Lang string `json:"lang"`
		} `json:"summary_info"`
	}
	if json.Unmarshal(obj["summary_list"], &summaries) != nil || len(summaries) < 1 || len(summaries) > 3 {
		return "", errors.New("审批申请需要 summary_list，包含 1–3 行摘要")
	}
	for _, summary := range summaries {
		if len(summary.Info) == 0 {
			return "", errors.New("审批摘要行需要非空 summary_info")
		}
		for _, info := range summary.Info {
			if strings.TrimSpace(info.Text) == "" || len([]rune(info.Text)) > 20 || (info.Lang != "zh_CN" && info.Lang != "en") {
				return "", errors.New("审批摘要文本须为 1–20 个字符，lang 为 zh_CN 或 en")
			}
		}
	}
	b, err := c.approvalCall(ctx, cred, "/cgi-bin/oa/applyevent", input)
	if err != nil {
		return "", err
	}
	var out struct {
		SPNo string `json:"sp_no"`
	}
	if json.Unmarshal(b, &out) != nil || !weComApprovalID(out.SPNo) {
		return "", errors.New("企业微信提交审批响应缺少有效 sp_no；结果未知，请核实后再重试")
	}
	return out.SPNo, nil
}

func (c *WeComClient) ApprovalDetail(ctx context.Context, cred Cred, spNo string) (json.RawMessage, error) {
	if !weComApprovalID(spNo) {
		return nil, errors.New("审批单号不能为空或超过 256 字节")
	}
	body, _ := json.Marshal(struct {
		SPNo string `json:"sp_no"`
	}{spNo})
	return c.approvalCall(ctx, cred, "/cgi-bin/oa/getapprovaldetail", body)
}

type WeComApprovalPage struct {
	SPNoList   []string `json:"sp_no_list"`
	NextCursor string   `json:"new_next_cursor"`
}

func (c *WeComClient) ApprovalList(ctx context.Context, cred Cred, input json.RawMessage) (WeComApprovalPage, error) {
	obj, err := weComApprovalObject(input)
	if err != nil {
		return WeComApprovalPage{}, err
	}
	var fields struct {
		StartTime string `json:"starttime"`
		EndTime   string `json:"endtime"`
		Size      *int   `json:"size"`
		NewCursor string `json:"new_cursor"`
		Filters   []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"filters"`
	}
	if json.Unmarshal(input, &fields) != nil {
		return WeComApprovalPage{}, errors.New("审批列表参数格式错误：starttime、endtime、new_cursor 须为字符串，size 须为整数")
	}
	start, startErr := strconv.ParseInt(fields.StartTime, 10, 64)
	end, endErr := strconv.ParseInt(fields.EndTime, 10, 64)
	if startErr != nil || endErr != nil || start < 0 || end <= start || end-start > 31*24*60*60 {
		return WeComApprovalPage{}, errors.New("审批列表的时间戳字符串 endtime 必须大于 starttime，跨度不得超过 31 天")
	}
	if fields.Size != nil && (*fields.Size < 1 || *fields.Size > 100) {
		return WeComApprovalPage{}, errors.New("审批列表 size 必须为 1–100")
	}
	if _, legacy := obj["cursor"]; legacy {
		return WeComApprovalPage{}, errors.New("审批列表请使用 new_cursor，不支持已废弃的 cursor")
	}
	for _, filter := range fields.Filters {
		if filter.Value == "" {
			return WeComApprovalPage{}, errors.New("审批列表筛选条件 value 不能为空")
		}
		switch filter.Key {
		case "template_id", "creator", "department", "sp_status", "record_type":
		default:
			return WeComApprovalPage{}, errors.New("审批列表筛选条件 key 不支持")
		}
	}
	if fields.Size == nil {
		obj["size"] = json.RawMessage("100")
	}
	if _, ok := obj["new_cursor"]; !ok {
		obj["new_cursor"] = json.RawMessage(`""`)
	}
	body, _ := json.Marshal(obj)
	b, err := c.approvalCall(ctx, cred, "/cgi-bin/oa/getapprovalinfo", body)
	if err != nil {
		return WeComApprovalPage{}, err
	}
	var out WeComApprovalPage
	if json.Unmarshal(b, &out) != nil || out.SPNoList == nil {
		return WeComApprovalPage{}, errors.New("企业微信审批列表响应缺少有效 sp_no_list")
	}
	return out, nil
}

// OpenApprovalData is the self-built application's flow engine, not the
// enterprise Approval app. Creation is a client-side JSAPI (thirdPartyOpenPage),
// so this server does not claim to offer a server-side submit endpoint. /90269.
func (c *WeComClient) OpenApprovalData(ctx context.Context, cred Cred, thirdNo string) (json.RawMessage, error) {
	if !weComApprovalID(thirdNo) {
		return nil, errors.New("自建应用审批编号 thirdNo 不能为空或超过 256 字节")
	}
	body, _ := json.Marshal(struct {
		ThirdNo string `json:"thirdNo"`
	}{thirdNo})
	return c.approvalCall(ctx, cred, "/cgi-bin/corp/getopenapprovaldata", body)
}

func (c *WeComClient) approvalCall(ctx context.Context, cred Cred, path string, body []byte) (json.RawMessage, error) {
	b, err := c.authenticated(ctx, cred, http.MethodPost, path, nil, body)
	if err != nil {
		var upstream *weComAPIError
		if errors.As(err, &upstream) && (upstream.code == 301055 || upstream.code == 301088 || upstream.code == 48002 || upstream.code == 48001) {
			return nil, errors.New("当前应用没有审批接口权限，请在企业微信管理后台【审批 → 可调用接口的应用】授权当前自建应用")
		}
		return nil, err
	}
	return json.RawMessage(b), nil
}

func weComApprovalObject(input json.RawMessage) (map[string]json.RawMessage, error) {
	if len(input) == 0 || len(input) > 1024*1024 {
		return nil, errors.New("审批请求必须为不超过 1 MiB 的 JSON 对象")
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(input, &obj) != nil || obj == nil {
		return nil, errors.New("审批请求必须为有效的 JSON 对象")
	}
	return obj, nil
}

func weComApprovalID(value string) bool {
	return strings.TrimSpace(value) != "" && strings.TrimSpace(value) == value && len(value) <= 256 && !strings.ContainsAny(value, "\r\n\x00")
}
