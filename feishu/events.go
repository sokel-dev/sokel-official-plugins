package main

// Event source: larkws long-lived connection -> three kinds of platform events.
//
// This is the number-one reason the official SDK was pulled in (see the top comment in
// client.go): **events don't arrive over a public webhook**. The plugin actively opens a
// WebSocket to Feishu, and events are pushed down that connection — no public IP needed, no
// encrypt/decrypt/verify step, exactly isomorphic to the "plugin makes an outbound connection to
// the broker" deployment philosophy.
//
// Prerequisite (documented in docs/feishu.md for users to configure): in the Open Platform's
// "Events & Callbacks," set the subscription method to **long connection**, and check
// im.message.receive_v1 / im.chat.member.bot.added_v1; card callbacks under "Card Interaction"
// need the same long-connection setting.
//
// Per-credential: one credential (= one app) gets one long-lived connection; multiple apps in a
// single instance are managed by the SDK's source supervisor (the same mechanism as Telegram's
// multi-bot setup).
//
// Dedup: Feishu re-delivers events (at-least-once semantics); event_id is handed to the platform
// to dedup by (pluginId, event, eventID) — the plugin doesn't build its own dedup table.

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

	// In long-connection mode the platform doesn't do signature verification
	// (authentication happens at connection time), so both tokens can be passed empty.
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
	// Start blocks until ctx is canceled (the supervisor cancels it when the credential is
	// deleted/disabled).
	err := cli.Start(ctx)
	if err != nil && ctx.Err() == nil {
		return fmt.Errorf("飞书长连接失败: %w（多半是 app_id/app_secret 有误，或应用没把订阅方式设为「长连接」）", err)
	}
	return nil
}

// onMessage handles im.message.receive_v1 -> "message received".
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
	// A text message's body is in content's JSON string; an @-mention appears in the text
	// as an @_user_N placeholder, with the mapping in mentions. The placeholders are
	// stripped from the text handed downstream — the workflow cares about the actual
	// instruction.
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

// onBotAdded handles im.chat.member.bot.added_v1 -> "bot added to a chat".
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

// onCardAction handles card.action.trigger -> "card button clicked".
// Returns a lightweight toast so the Feishu client gets immediate feedback; whether the card
// itself needs updating is up to the workflow (via a direct `call` to the cardkit API) — the
// plugin doesn't overstep and modify the card on the user's behalf.
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
