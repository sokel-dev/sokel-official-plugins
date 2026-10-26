// wecom sends application notifications and hosts an authenticated interaction portal through Sokel.
package main

//go:generate go run -mod=mod github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen generate -lang go .

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

type wecomAPI interface {
	AccessToken(context.Context, Cred) (string, error)
	VerifyApplication(context.Context, Cred) error
	SendText(context.Context, Cred, []string, string) (Delivery, error)
	SendCard(context.Context, Cred, []string, string, string, string, string) (Delivery, error)
	UserID(context.Context, Cred, string) (string, error)
}

type application struct {
	store   *Store
	api     wecomAPI
	sendMu  sync.Mutex
	eventMu sync.Mutex
}

func main() {
	dir := strings.TrimSpace(os.Getenv("SOKEL_DATA_DIR"))
	if dir == "" {
		log.Fatal("请设置 SOKEL_DATA_DIR 并挂载持久卷；交互消息插件仅支持单副本")
	}
	store, err := OpenStore(dir)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	client := NewWeComClient()
	a := &application{store: store, api: client}
	callbackStore, err := OpenCallbackStore(filepath.Join(dir, "features"))
	if err != nil {
		log.Fatal(err)
	}
	defer callbackStore.Close()
	features := &featureApplication{client: client, identity: NewWeComIdentityClient(client), callbackStore: callbackStore, callbacks: NewCallbackService(callbackStore)}
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "wecom"})
	RegisterCredential(p)
	RegisterDoc(p)
	DeclareEvents(p)
	OnSendText(p, a.sendText)
	OnSendCard(p, a.sendCard)
	OnSendInteractiveMessage(p, a.sendInteractiveMessage)
	OnGetInteractiveMessage(p, a.getInteractiveMessage)
	OnHealthCheck(p, a.healthCheck)
	features.register(p)
	sokel.RegisterWebhook(p, func(ctx sokel.WebhookCtx, req *sokel.WebhookRequest) sokel.WebhookResponse {
		return features.routeWebhook(a, ctx, req)
	})
	sokel.RegisterSource(p, sokel.Source{ID: "interaction_outbox", Label: "交互消息动作事件"}, a.runOutbox)
	sokel.RegisterSource(p, sokel.Source{ID: "callback_outbox", Label: "消息和应用事件"}, features.runCallbackOutbox)
	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}

func credential(ctx plugin.Ctx) (Cred, string, error) {
	c := sokel.CredentialAs[Cred](ctx)
	c.CorpID, c.AgentID, c.Secret = strings.TrimSpace(c.CorpID), strings.TrimSpace(c.AgentID), strings.TrimSpace(c.Secret)
	c.CallbackToken, c.CallbackAesKey = strings.TrimSpace(c.CallbackToken), strings.TrimSpace(c.CallbackAesKey)
	id, err := strconv.ParseInt(c.AgentID, 10, 64)
	if c.CorpID == "" || c.Secret == "" || err != nil || id <= 0 {
		return c, "", fmt.Errorf("请配置企业 ID、正整数 AgentId 和应用 Secret")
	}
	c.AgentID = strconv.FormatInt(id, 10)
	// One namespace per application; rotating a secret keeps existing messages usable.
	sum := sha256.Sum256([]byte(c.CorpID + "\x00" + c.AgentID))
	return c, hex.EncodeToString(sum[:]), nil
}

func recipients(users []string) ([]string, error) {
	if len(users) == 0 || len(users) > 1000 {
		return nil, fmt.Errorf("请提供 1 至 1000 个成员 UserID")
	}
	seen := make(map[string]bool)
	out := make([]string, 0, len(users))
	for _, user := range users {
		u := strings.TrimSpace(user)
		if u == "" || len(u) > 64 || strings.ContainsAny(u, "|\r\n\t") || strings.HasPrefix(u, "@") {
			return nil, fmt.Errorf("成员 UserID 无效；不支持 @all 广播或分隔符")
		}
		k := strings.ToLower(u)
		if !seen[k] {
			seen[k] = true
			out = append(out, u)
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out, nil
}

func accepted(d Delivery) bool {
	return d.MsgID != "" && d.InvalidUser == "" && d.UnlicensedUser == "" && d.InvalidParty == "" && d.InvalidTag == ""
}

func (a *application) sendText(ctx plugin.Ctx, in *SendTextIn) (*SendTextOut, error) {
	c, _, err := credential(ctx)
	if err != nil {
		return nil, err
	}
	users, err := recipients(in.Users)
	if err != nil {
		return nil, err
	}
	d, err := a.api.SendText(ctx, c, users, in.Text)
	if err != nil {
		return nil, err
	}
	return &SendTextOut{MsgID: d.MsgID, InvalidUser: d.InvalidUser, UnlicensedUser: d.UnlicensedUser, AllAccepted: accepted(d)}, nil
}

func (a *application) sendCard(ctx plugin.Ctx, in *SendCardIn) (*SendCardOut, error) {
	c, _, err := credential(ctx)
	if err != nil {
		return nil, err
	}
	users, err := recipients(in.Users)
	if err != nil {
		return nil, err
	}
	button := in.Button
	if button == "" {
		button = "查看详情"
	}
	d, err := a.api.SendCard(ctx, c, users, in.Title, html.EscapeString(in.Description), in.URL, button)
	if err != nil {
		return nil, err
	}
	return &SendCardOut{MsgID: d.MsgID, InvalidUser: d.InvalidUser, UnlicensedUser: d.UnlicensedUser, AllAccepted: accepted(d)}, nil
}

func portalURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", fmt.Errorf("请填写本凭证的完整 HTTPS Webhook URL，不要附带查询参数或片段")
	}
	if !strings.Contains(u.Path, "/hooks/") || strings.HasSuffix(u.Path, "/hooks/") {
		return "", fmt.Errorf("H5 地址应为平台提供的 /hooks/… Webhook URL")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u.String(), nil
}

func (a *application) healthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	c, _, err := credential(ctx)
	if err == nil && (c.CallbackToken != "" || c.CallbackAesKey != "") {
		if _, keyErr := NewCallbackCrypt(c.CallbackToken, c.CallbackAesKey, c.CorpID); keyErr != nil {
			err = fmt.Errorf("接收消息 Token 或 EncodingAESKey 格式无效，请核对 API 接收配置")
		}
	}
	if err == nil {
		err = a.api.VerifyApplication(ctx, c)
	}
	if err == nil {
		err = a.store.Check()
	}
	if err != nil {
		return &HealthCheckOut{OK: false, Message: err.Error()}, nil
	}
	return &HealthCheckOut{OK: true, Message: "应用凭证与本地存储检查通过；请另行核验成员可见范围、微信插件和网页授权可信域名"}, nil
}

func (a *application) flushEvents(ctx plugin.SourceCtx, ns string) error {
	if err := a.store.Check(); err != nil {
		return err
	}
	c, verifiedNS, err := credential(ctx)
	if err != nil {
		return err
	}
	if verifiedNS != ns {
		return fmt.Errorf("事件凭证与消息不匹配")
	}
	if err := a.api.VerifyApplication(ctx, c); err != nil {
		return err
	}
	a.eventMu.Lock()
	defer a.eventMu.Unlock()
	for i, e := range a.store.PendingEvents(ns) {
		if i >= 16 {
			break
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		payload := &InteractionActionEvent{EventID: e.ID, InteractionID: e.AlertID, MsgID: e.MsgID, RequestID: e.ExternalID, ActionID: e.Action, ActionText: e.ActionText, ActionValue: e.Value, Context: e.Context, Revision: e.Revision, ActorUserID: e.ActorUserID, OccurredAt: e.OccurredAt}
		if err := TriggerInteractionAction(ctx, e.ID, payload); err != nil {
			return err
		}
		if err := a.store.MarkDispatched(ns, e.ID); err != nil {
			return err
		}
	}
	return nil
}

func (a *application) runOutbox(ctx plugin.SourceCtx) error {
	_, ns, err := credential(ctx)
	if err != nil {
		return err
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		if err := a.flushEvents(ctx, ns); err != nil {
			ctx.ReportStatus("degraded", "处理事件待重试")
		} else if len(a.store.PendingEvents(ns)) > 0 {
			ctx.ReportStatus("degraded", "处理事件正在分批提交")
		} else {
			ctx.ReportStatus("ok", "处理事件已提交")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
