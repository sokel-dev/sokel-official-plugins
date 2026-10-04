package main

// 事件源：larkws 长连接 → 三类平台事件。
//
// 这是引官方 SDK 的头号理由（见 client.go 顶注）：**事件不走公网 webhook**。
// 插件主动向飞书建一条 WebSocket，事件从连接上推下来——不要公网 IP、不要
// 加解密验签，与「插件出站接 broker」的部署哲学完全同构。
//
// 前置（写在 docs/feishu.md，用户侧要配）：开放平台「事件与回调」把订阅方式
// 设为**长连接**，并勾选 im.message.receive_v1 / im.chat.member.bot.added_v1；
// 卡片回调在「卡片交互」处同样选长连接。
//
// per-credential：一条凭证（= 一个应用）一条长连接，多应用单实例由 SDK 的
// source supervisor 管（telegram 多 bot 同款机制）。
//
// 去重：飞书会重推事件（至少一次语义），event_id 交给平台按
// (pluginId, event, eventID) 去重——插件内不自建去重表。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

func runEvents(ctx plugin.SourceCtx) error {
	cred := sokel.SourceCredentialAs[Cred](ctx)
	appID, secret := strings.TrimSpace(cred.AppID), strings.TrimSpace(cred.AppSecret)
	if appID == "" || secret == "" {
		return fmt.Errorf("凭证缺 app_id/app_secret，事件源不启动")
	}

	// 长连接模式下平台不做签名校验（认证发生在建连时），两个 token 传空即可。
	handler := dispatcher.NewEventDispatcher("", "").
		OnP2MessageReceiveV1(func(_ context.Context, ev *larkim.P2MessageReceiveV1) error {
			return onMessage(ctx, ev)
		}).
		OnP2ChatMemberBotAddedV1(func(_ context.Context, ev *larkim.P2ChatMemberBotAddedV1) error {
			return onBotAdded(ctx, ev)
		}).
		OnP2CardActionTrigger(func(_ context.Context, ev *callback.CardActionTriggerEvent) (*callback.CardActionTriggerResponse, error) {
			return onCardAction(ctx, ev)
		})

	cli := larkws.NewClient(appID, secret,
		larkws.WithEventHandler(handler),
		larkws.WithDomain(baseURL(cred.Domain)),
		larkws.WithAutoReconnect(true),
		larkws.WithOnReady(func() { ctx.ReportStatus("ok", "长连接已建立") }),
		larkws.WithOnReconnecting(func() { ctx.ReportStatus("degraded", "长连接断开，重连中") }),
		larkws.WithOnReconnected(func() { ctx.ReportStatus("ok", "长连接已恢复") }),
	)
	// Start 阻塞直到 ctx 取消（凭证被删/禁用时 supervisor 取消它）。
	err := cli.Start(ctx)
	if err != nil && ctx.Err() == nil {
		return fmt.Errorf("飞书长连接失败: %w（多半是 app_id/app_secret 有误，或应用没把订阅方式设为「长连接」）", err)
	}
	return nil
}

// onMessage im.message.receive_v1 → 「收到消息」。
func onMessage(ctx plugin.SourceCtx, ev *larkim.P2MessageReceiveV1) error {
	if ev.Event == nil || ev.Event.Message == nil {
		return nil
	}
	m := ev.Event.Message
	e := &MessageEvent{
		ChatID:      str(m.ChatId),
		ChatType:    str(m.ChatType),
		MessageID:   str(m.MessageId),
		MessageType: str(m.MessageType),
		Raw:         ev.Event,
	}
	if s := ev.Event.Sender; s != nil && s.SenderId != nil {
		e.SenderOpenID = str(s.SenderId.OpenId)
	}
	// text 消息的正文在 content 的 JSON 串里；@人以 @_user_N 占位出现在文本中，
	// 对应关系在 mentions 里。给下游的 text 把占位符去掉——工作流关心的是指令本身。
	if str(m.MessageType) == "text" {
		var c struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal([]byte(str(m.Content)), &c)
		text := c.Text
		for _, mt := range m.Mentions {
			if mt != nil {
				text = strings.ReplaceAll(text, str(mt.Key), "")
			}
		}
		e.Text = strings.TrimSpace(text)
	}
	e.MentionedBot = len(m.Mentions) > 0
	return TriggerMessage(ctx, ev.EventV2Base.Header.EventID, e)
}

// onBotAdded im.chat.member.bot.added_v1 → 「bot 被拉进群」。
func onBotAdded(ctx plugin.SourceCtx, ev *larkim.P2ChatMemberBotAddedV1) error {
	if ev.Event == nil {
		return nil
	}
	e := &BotAddedEvent{
		ChatID:   str(ev.Event.ChatId),
		ChatName: str(ev.Event.Name),
		Raw:      ev.Event,
	}
	if op := ev.Event.OperatorId; op != nil {
		e.InviterOpenID = str(op.OpenId)
	}
	return TriggerBotAdded(ctx, ev.EventV2Base.Header.EventID, e)
}

// onCardAction card.action.trigger → 「卡片按钮点击」。
// 返回一个轻 toast 让飞书端立刻有反馈；卡片本身要不要更新由工作流决定
// （用 call 直调 cardkit 接口），插件不越权替用户改卡片。
func onCardAction(ctx plugin.SourceCtx, ev *callback.CardActionTriggerEvent) (*callback.CardActionTriggerResponse, error) {
	if ev.Event == nil {
		return nil, nil
	}
	e := &CardActionEvent{Raw: ev.Event}
	if op := ev.Event.Operator; op != nil {
		e.OperatorOpenID = op.OpenID
	}
	if c := ev.Event.Context; c != nil {
		e.ChatID = c.OpenChatID
		e.MessageID = c.OpenMessageID
	}
	if a := ev.Event.Action; a != nil {
		e.ActionValue = a.Value
		if len(a.FormValue) > 0 {
			e.FormValues = a.FormValue
		}
	}
	eventID := ""
	if ev.EventV2Base != nil && ev.EventV2Base.Header != nil {
		eventID = ev.EventV2Base.Header.EventID
	}
	if err := TriggerCardAction(ctx, eventID, e); err != nil {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "error", Content: "工作流触发失败"},
		}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已收到"},
	}, nil
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
