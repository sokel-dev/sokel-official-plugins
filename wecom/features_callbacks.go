package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

func callbackCredentialKey(c Cred) string {
	raw, _ := json.Marshal([2]string{c.CorpID, c.AgentID})
	key := sha256.Sum256(raw)
	return hex.EncodeToString(key[:])
}

func (a *featureApplication) routeWebhook(alerts *application, ctx sokel.WebhookCtx, req *sokel.WebhookRequest) sokel.WebhookResponse {
	// Sokel forwards the wildcard without a leading slash. Accept both forms so
	// direct SDK embedders and the real credential-scoped gateway behave alike.
	if strings.Trim(req.Path, "/") == "callback" {
		return a.callbackWebhook(ctx, req)
	}
	return alerts.webhook(ctx, req)
}

func (a *featureApplication) callbackWebhook(ctx sokel.WebhookCtx, req *sokel.WebhookRequest) sokel.WebhookResponse {
	c, _, err := credential(ctx)
	if err != nil {
		return portalError(503, "请先配置应用凭证与 API 接收密钥")
	}
	q, err := url.ParseQuery(req.Query)
	if err != nil {
		return portalError(400, "回调参数无效")
	}
	status, contentType, body, err := a.callbacks.Handle(ctx, c, callbackCredentialKey(c), req.Method, q, req.Body)
	if err != nil {
		// Authenticated message content, keys and storage paths never enter errors.
		return portalError(status, "企业微信回调校验或持久化失败，请检查 API 接收配置")
	}
	return sokel.WebhookResponse{Status: status, Headers: securityHeaders(contentType), Body: body}
}

func (a *featureApplication) callbackCredential(ctx plugin.Ctx) (Cred, error) {
	c, _, err := credential(ctx)
	if err != nil {
		return c, err
	}
	if _, err := callbackNamespace(c); err != nil {
		return c, fmt.Errorf("请先在凭证中配置接收消息 Token 与 EncodingAESKey")
	}
	if err := a.client.VerifyApplication(ctx, c); err != nil {
		return c, err
	}
	return c, nil
}

func (a *featureApplication) autoReplySet(ctx plugin.Ctx, in *AutoReplySetIn) (*AutoReplySetOut, error) {
	c, err := a.callbackCredential(ctx)
	if err != nil {
		return nil, err
	}
	rule := AutoReplyRule{ID: in.RuleID, Enabled: in.Enabled, MatchType: in.MatchType, Keyword: in.Keyword, Reply: in.Reply, Priority: in.Priority}
	if err := a.callbackStore.SetRule(c, rule); err != nil {
		return nil, err
	}
	return &AutoReplySetOut{OK: true}, nil
}

func (a *featureApplication) autoReplyList(ctx plugin.Ctx, _ *AutoReplyListIn) (*AutoReplyListOut, error) {
	c, err := a.callbackCredential(ctx)
	if err != nil {
		return nil, err
	}
	rules, err := a.callbackStore.ListRules(c)
	if err != nil {
		return nil, err
	}
	out := &AutoReplyListOut{Rules: []ReplyRuleView{}}
	for _, rule := range rules {
		out.Rules = append(out.Rules, ReplyRuleView{RuleID: rule.ID, Enabled: rule.Enabled, MatchType: rule.MatchType, Keyword: rule.Keyword, Reply: rule.Reply, Priority: rule.Priority})
	}
	return out, nil
}

func (a *featureApplication) autoReplyDelete(ctx plugin.Ctx, in *AutoReplyDeleteIn) (*AutoReplyDeleteOut, error) {
	c, err := a.callbackCredential(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.callbackStore.DeleteRule(c, in.RuleID); err != nil {
		return nil, err
	}
	return &AutoReplyDeleteOut{OK: true}, nil
}

func callbackMessageView(p CallbackPending) ReceivedMessageView {
	m := p.Message
	return ReceivedMessageView{EventID: p.ID, MsgID: m.MsgID, FromUser: m.FromUserName, ToUser: m.ToUserName, AgentID: m.AgentID, MessageType: m.MsgType, Content: m.Content, EventType: m.Event, EventKey: m.EventKey, ChangeType: m.ChangeType, CreatedAt: int(m.CreateTime), ReceivedAt: p.ReceivedAt, MediaID: m.MediaID, PicURL: m.PicURL, TaskID: m.TaskID, CardType: m.CardType, ResponseCode: m.ResponseCode, RawXML: m.RawXML}
}

func (a *featureApplication) listReceived(ctx plugin.Ctx, in *ListReceivedIn) (*ListReceivedOut, error) {
	c, err := a.callbackCredential(ctx)
	if err != nil {
		return nil, err
	}
	if in.Limit < 0 || in.Limit > 100 {
		return nil, fmt.Errorf("记录数应为 1 至 100")
	}
	limit := in.Limit
	if limit == 0 {
		limit = 20
	}
	items, err := a.callbackStore.ListMessages(c, limit)
	if err != nil {
		return nil, err
	}
	out := &ListReceivedOut{Messages: []ReceivedMessageView{}}
	for _, p := range items {
		out.Messages = append(out.Messages, callbackMessageView(p))
	}
	return out, nil
}

// A shared lock prevents overlapping copies of the same application credential
// from simultaneously draining one persisted outbox. Trigger deduplicates by ID.
var callbackPublishMu sync.Mutex

func (a *featureApplication) flushCallbackEvents(ctx plugin.SourceCtx, c Cred) error {
	if err := a.client.VerifyApplication(ctx, c); err != nil {
		return err
	}
	callbackPublishMu.Lock()
	defer callbackPublishMu.Unlock()
	items, err := a.callbackStore.Pending(c, callbackCredentialKey(c), 16)
	if err != nil {
		return err
	}
	for _, p := range items {
		v := callbackMessageView(p)
		if p.Message.MsgType == "event" {
			err = TriggerApplicationEvent(ctx, p.ID, &ApplicationEventEvent{EventID: v.EventID, MsgID: v.MsgID, FromUser: v.FromUser, ToUser: v.ToUser, AgentID: v.AgentID, MessageType: v.MessageType, Content: v.Content, EventType: v.EventType, EventKey: v.EventKey, ChangeType: v.ChangeType, CreatedAt: v.CreatedAt, ReceivedAt: v.ReceivedAt, MediaID: v.MediaID, PicURL: v.PicURL, TaskID: v.TaskID, CardType: v.CardType, ResponseCode: v.ResponseCode, RawXML: v.RawXML})
		} else {
			err = TriggerMessageReceived(ctx, p.ID, &MessageReceivedEvent{EventID: v.EventID, MsgID: v.MsgID, FromUser: v.FromUser, ToUser: v.ToUser, AgentID: v.AgentID, MessageType: v.MessageType, Content: v.Content, EventType: v.EventType, EventKey: v.EventKey, ChangeType: v.ChangeType, CreatedAt: v.CreatedAt, ReceivedAt: v.ReceivedAt, MediaID: v.MediaID, PicURL: v.PicURL, TaskID: v.TaskID, CardType: v.CardType, ResponseCode: v.ResponseCode, RawXML: v.RawXML})
		}
		if err != nil {
			return err
		}
		if err := a.callbackStore.MarkDispatched(p.ID); err != nil {
			return err
		}
	}
	return nil
}

func (a *featureApplication) runCallbackOutbox(ctx plugin.SourceCtx) error {
	c, _, err := credential(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(c.CallbackToken) == "" && strings.TrimSpace(c.CallbackAesKey) == "" {
		// Sending notifications remains usable without API-receive configuration.
		<-ctx.Done()
		return nil
	}
	if _, err := callbackNamespace(c); err != nil {
		return fmt.Errorf("API 接收 Token 或 EncodingAESKey 配置无效")
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		if err := a.flushCallbackEvents(ctx, c); err != nil {
			ctx.ReportStatus("degraded", "消息和应用事件待重试")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
