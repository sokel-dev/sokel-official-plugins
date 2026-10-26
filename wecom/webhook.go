package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

//go:embed web/index.html
var portalHTML string

type portalRequest struct {
	InteractionID string `json:"interaction_id"`
	State         string `json:"state"`
	Code          string `json:"code"`
	Ticket        string `json:"ticket"`
	ActionID      string `json:"action_id"`
}

type interactionView struct {
	ID           string          `json:"id"`
	RequestID    string          `json:"request_id"`
	Title        string          `json:"title"`
	Content      string          `json:"content"`
	Buttons      []MessageButton `json:"buttons"`
	Context      map[string]any  `json:"context,omitempty"`
	LastActionID string          `json:"last_action_id"`
	ActorUserID  string          `json:"actor_user_id"`
	Revision     int             `json:"revision"`
	CreatedAt    string          `json:"created_at"`
	UpdatedAt    string          `json:"updated_at"`
	ExpiresAt    string          `json:"expires_at"`
}

type portalResponse struct {
	Ticket      string          `json:"ticket"`
	UserID      string          `json:"user_id"`
	Interaction interactionView `json:"interaction"`
	EventID     string          `json:"event_id,omitempty"`
	EventStatus string          `json:"event_status,omitempty"`
}

func viewOf(r Alert) interactionView {
	return interactionView{ID: r.ID, RequestID: r.ExternalID, Title: r.Title, Content: r.Description, Buttons: messageButtons(r.Buttons), Context: r.Context, LastActionID: r.LastActionID, ActorUserID: r.ActorUserID, Revision: r.Revision, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, ExpiresAt: r.ExpiresAt}
}

func securityHeaders(contentType string) map[string]string {
	return map[string]string{"Content-Type": contentType, "Cache-Control": "no-store, private", "Referrer-Policy": "no-referrer", "X-Content-Type-Options": "nosniff", "X-Frame-Options": "DENY", "Permissions-Policy": "camera=(), microphone=(), geolocation=()"}
}

func jsonResponse(status int, v any) sokel.WebhookResponse {
	body, err := json.Marshal(v)
	if err != nil {
		return sokel.WebhookResponse{Status: 500, Headers: securityHeaders("application/json; charset=utf-8"), Body: []byte(`{"error":"响应生成失败"}`)}
	}
	return sokel.WebhookResponse{Status: status, Headers: securityHeaders("application/json; charset=utf-8"), Body: body}
}

func portalError(status int, message string) sokel.WebhookResponse {
	return jsonResponse(status, struct {
		Error string `json:"error"`
	}{message})
}

func storeResponse(err error) sokel.WebhookResponse {
	var se *StoreError
	if errors.As(err, &se) {
		message := map[int]string{400: "请求参数无效，请重新打开消息", 403: "操作凭证无效或你不在消息收件人范围内，请重新授权", 404: "消息不存在或已超出保留期", 409: "操作票据已使用或暂时无法处理，请重新授权", 410: "消息或操作凭证已过期，请重新授权查看"}[se.Status]
		if message == "" {
			message = "暂时无法保存或读取交互记录，请联系管理员"
		}
		return portalError(se.Status, message)
	}
	// Filesystem paths and upstream URLs must never reach the public page.
	return portalError(500, "暂时无法保存或读取交互记录，请稍后重试")
}

type timedSource struct {
	plugin.SourceCtx
	timer context.Context
}

func (c timedSource) Deadline() (time.Time, bool) { return c.timer.Deadline() }
func (c timedSource) Done() <-chan struct{}       { return c.timer.Done() }
func (c timedSource) Err() error                  { return c.timer.Err() }
func (c timedSource) Value(key any) any           { return c.timer.Value(key) }

func (a *application) webhook(ctx sokel.WebhookCtx, req *sokel.WebhookRequest) sokel.WebhookResponse {
	timeout, cancel := context.WithTimeout(ctx, 11*time.Second)
	defer cancel()
	ctx = timedSource{SourceCtx: ctx, timer: timeout}
	if req.Path != "" && req.Path != "/" {
		return portalError(404, "页面不存在")
	}
	q, err := url.ParseQuery(req.Query)
	if err != nil {
		return portalError(400, "请求参数无效")
	}
	if req.Method == http.MethodGet && q.Get("op") == "" {
		nonce, err := randomID()
		if err != nil {
			return portalError(500, "页面暂时不可用")
		}
		headers := securityHeaders("text/html; charset=utf-8")
		headers["Content-Security-Policy"] = fmt.Sprintf("default-src 'none'; script-src 'nonce-%s'; style-src 'nonce-%s'; connect-src 'self'; img-src 'self' data:; base-uri 'none'; frame-ancestors 'none'; form-action 'none'", nonce, nonce)
		return sokel.WebhookResponse{Status: 200, Headers: headers, Body: []byte(strings.ReplaceAll(portalHTML, "{{NONCE}}", nonce))}
	}
	if req.Method != http.MethodPost {
		return portalError(405, "请通过消息处理页面提交操作")
	}
	typ, _, err := mime.ParseMediaType(req.Header("Content-Type"))
	if err != nil || typ != "application/json" {
		return portalError(415, "只接受 JSON 请求")
	}
	if len(req.Body) > 16384 {
		return portalError(413, "请求内容过大")
	}
	var in portalRequest
	dec := json.NewDecoder(bytes.NewReader(req.Body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return portalError(400, "请求内容无效")
	}
	if err := dec.Decode(new(json.RawMessage)); err != io.EOF {
		return portalError(400, "请求内容无效")
	}
	if !validNonce(in.InteractionID) {
		return portalError(400, "消息地址无效")
	}
	cred, ns, err := credential(ctx)
	if err != nil {
		return portalError(503, "消息应用尚未正确配置，请联系管理员")
	}
	if err := a.api.VerifyApplication(ctx, cred); err != nil {
		return portalError(503, "应用凭证校验失败，请联系管理员检查 Secret 和 AgentId")
	}
	record, err := a.store.GetAlert(ns, in.InteractionID)
	if err != nil {
		return storeResponse(err)
	}
	parsed, err := url.Parse(record.PortalURL)
	if err != nil || parsed.Scheme != "https" {
		return portalError(500, "消息页面地址配置无效")
	}
	origin := req.Header("Origin")
	if origin != "" && origin != parsed.Scheme+"://"+parsed.Host {
		return portalError(403, "请从原消息链接打开处理页面")
	}
	if site := req.Header("Sec-Fetch-Site"); site == "cross-site" {
		return portalError(403, "不接受跨站请求")
	}
	switch q.Get("op") {
	case "login":
		if !validNonce(in.State) {
			return portalError(400, "授权状态无效，请重新打开页面")
		}
		if err := a.store.StartLogin(ns, in.InteractionID, in.State); err != nil {
			return storeResponse(err)
		}
		params := url.Values{"appid": {cred.CorpID}, "agentid": {cred.AgentID}, "redirect_uri": {interactionURL(record)}, "response_type": {"code"}, "scope": {"snsapi_base"}, "state": {in.State}}
		authURL := "https://open.weixin.qq.com/connect/oauth2/authorize?" + params.Encode() + "#wechat_redirect"
		return jsonResponse(200, struct {
			AuthorizeURL string `json:"authorize_url"`
		}{authURL})
	case "exchange":
		if !validNonce(in.State) || in.Code == "" || len(in.Code) > 512 {
			return portalError(400, "授权参数无效，请重新授权")
		}
		if err := a.store.ConsumeLogin(ns, in.InteractionID, in.State); err != nil {
			return storeResponse(err)
		}
		uid, err := a.api.UserID(ctx, cred, in.Code)
		if err != nil {
			return portalError(403, "未能验证企业成员身份，请重新授权；仍失败请检查成员绑定和应用可信域名")
		}
		ticket, r, err := a.store.NewTicket(ns, in.InteractionID, uid)
		if err != nil {
			return storeResponse(err)
		}
		return jsonResponse(200, portalResponse{Ticket: ticket, UserID: uid, Interaction: viewOf(r)})
	case "action":
		if !validNonce(in.Ticket) {
			return portalError(403, "操作凭证无效，请重新授权")
		}
		result, err := a.store.ApplyAction(ns, in.InteractionID, in.Ticket, in.ActionID)
		if err != nil {
			return storeResponse(err)
		}
		status := "submitted"
		for _, e := range a.store.PendingEvents(ns) {
			if e.ID == result.EventID {
				status = "pending"
				break
			}
		}
		return jsonResponse(200, portalResponse{Ticket: result.Ticket, UserID: result.UserID, Interaction: viewOf(result.Alert), EventID: result.EventID, EventStatus: status})
	default:
		return portalError(404, "操作不存在")
	}
}

func validNonce(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
