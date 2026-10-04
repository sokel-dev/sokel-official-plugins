package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

// callAPICtx：事件源侧调用。token 优先取平台注册时下发的凭证 bot_token（在平台凭证管理里配，default 工作空间），
// 缺省回退环境变量 TELEGRAM_BOT_TOKEN（多 bot/多工作空间部署时用 env 分部署）。
func callAPICtx(ctx plugin.SourceCtx, method string, params map[string]any) (any, error) {
	tok := ctx.Credential()["bot_token"]
	if tok == "" {
		tok = os.Getenv("TELEGRAM_BOT_TOKEN")
	}
	if tok == "" {
		return nil, fmt.Errorf("缺少 bot_token（在平台凭证管理里配，或设 TELEGRAM_BOT_TOKEN 环境变量）")
	}
	return callTelegram(ctx, tok, method, params)
}

// 接收侧：长轮询 getUpdates，把每条 update 按类型平铺成事件 payload → ctx.Trigger 推给平台。
// payload 字段名与 DeclareEvent 声明的 struct（sokel tag）一致，下游节点按契约引用。

// —— 事件 payload 契约（每种事件一套；Raw 兜底完整 update）——

// —— 原始 update 解析 ——

type tgUpdate struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		MessageID int64  `json:"message_id"`
		Text      string `json:"text"`
		From      tgUser `json:"from"`
		Chat      tgChat `json:"chat"`
	} `json:"message"`
	EditedMessage *struct {
		MessageID int64  `json:"message_id"`
		Text      string `json:"text"`
		From      tgUser `json:"from"`
		Chat      tgChat `json:"chat"`
	} `json:"edited_message"`
	CallbackQuery *struct {
		ID      string `json:"id"`
		Data    string `json:"data"`
		From    tgUser `json:"from"`
		Message *struct {
			MessageID int64  `json:"message_id"`
			Chat      tgChat `json:"chat"`
		} `json:"message"`
	} `json:"callback_query"`
	MyChatMember *struct {
		Chat tgChat `json:"chat"`
		From tgUser `json:"from"`
	} `json:"my_chat_member"`
}
type tgUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}
type tgChat struct {
	ID int64 `json:"id"`
}

// runUpdatesSource：长轮询循环。allowed_updates 与声明的事件对齐（省流量，Telegram 不推没订阅的类型）。
func runUpdatesSource(ctx plugin.SourceCtx) error {
	offset := int64(0)
	allowed := []string{"message", "edited_message", "callback_query", "my_chat_member"}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		ups, err := getUpdates(ctx, offset, allowed)
		if err != nil {
			log.Printf("[tg] getUpdates 失败: %v（3s 后重试）", err)
			time.Sleep(3 * time.Second)
			continue
		}
		for _, u := range ups {
			offset = u.UpdateID + 1
			// 事件名与 payload 字段都由生成的 TriggerXxx 定死——此前是「拼字符串 +
			// 无类型 payload」，写错要等运行期，症状还是最难查的「事件没触发」。
			if event, err := triggerUpdate(ctx, u); err != nil {
				log.Printf("[tg] 推事件 %s 失败: %v", event, err)
			}
		}
	}
}

// triggerUpdate：一条 update → 对应的 typed 触发。未识别的 update 直接跳过。
// 返回事件名仅供出错时打日志。
func triggerUpdate(ctx plugin.SourceCtx, u tgUpdate) (string, error) {
	id := strconv.FormatInt(u.UpdateID, 10)
	switch {
	case u.Message != nil:
		m := u.Message
		return "message", TriggerMessage(ctx, id, &MessageEvent{ChatID: int(m.Chat.ID), UserID: int(m.From.ID),
			Username: m.From.Username, Text: m.Text, MessageID: int(m.MessageID), Raw: u})
	case u.EditedMessage != nil:
		m := u.EditedMessage
		return "edited_message", TriggerEditedMessage(ctx, id, &EditedMessageEvent{ChatID: int(m.Chat.ID), UserID: int(m.From.ID),
			Username: m.From.Username, Text: m.Text, MessageID: int(m.MessageID), Raw: u})
	case u.CallbackQuery != nil:
		c := u.CallbackQuery
		e := &CallbackQueryEvent{UserID: int(c.From.ID), CallbackID: c.ID, CallbackData: c.Data, Raw: u}
		if c.Message != nil {
			e.ChatID = int(c.Message.Chat.ID)
			e.MessageID = int(c.Message.MessageID)
		}
		return "callback_query", TriggerCallbackQuery(ctx, id, e)
	case u.MyChatMember != nil:
		return "my_chat_member", TriggerMyChatMember(ctx, id, &MyChatMemberEvent{
			ChatID: int(u.MyChatMember.Chat.ID), UserID: int(u.MyChatMember.From.ID), Raw: u})
	}
	return "", nil
}

// getUpdates：一次长轮询（timeout=25s）。复用 callAPI 的凭证与 HTTP，但要 raw update 数组，故单独解析。
func getUpdates(ctx plugin.SourceCtx, offset int64, allowed []string) ([]tgUpdate, error) {
	res, err := callAPICtx(ctx, "getUpdates", map[string]any{
		"offset": offset, "timeout": 25, "allowed_updates": allowed,
	})
	if err != nil {
		return nil, err
	}
	// res = []any（update 对象数组）；回填进 tgUpdate。
	b, _ := json.Marshal(res)
	var ups []tgUpdate
	_ = json.Unmarshal(b, &ups)
	return ups, nil
}
