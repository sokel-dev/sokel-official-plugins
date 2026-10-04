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

// callAPICtx is the event-source-side call. The token is taken first from the bot_token credential
// issued when the plugin registered with the platform (configured in the platform's credential
// manager, default workspace), falling back to the TELEGRAM_BOT_TOKEN environment variable
// (useful for splitting deployments by env when running multiple bots/workspaces).
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

// Receiving side: long-polls getUpdates, flattens each update by type into an event payload, and
// pushes it to the platform via ctx.Trigger. Payload field names match the struct (sokel tag)
// declared with DeclareEvent, so downstream nodes can reference them per the contract.

// —— Event payload contracts (one per event type; Raw carries the full update as a fallback) ——

// —— Raw update parsing ——

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

// runUpdatesSource is the long-polling loop. allowed_updates is kept in sync with the declared
// events (saves bandwidth — Telegram won't push types nobody subscribed to).
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
			// Both the event name and the payload fields are pinned by the generated TriggerXxx —
			// it used to be "concatenated strings + untyped payload", where a mistake only
			// surfaced at runtime, and the symptom was the hardest kind to debug: "event never fired".
			if event, err := triggerUpdate(ctx, u); err != nil {
				log.Printf("[tg] 推事件 %s 失败: %v", event, err)
			}
		}
	}
}

// triggerUpdate maps one update to its corresponding typed trigger. Unrecognized updates are
// skipped. The returned event name is only used for logging on error.
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

// getUpdates runs a single long-poll (timeout=25s). It reuses callAPI's credential and HTTP
// handling, but needs the raw update array, so it parses the response separately.
func getUpdates(ctx plugin.SourceCtx, offset int64, allowed []string) ([]tgUpdate, error) {
	res, err := callAPICtx(ctx, "getUpdates", map[string]any{
		"offset": offset, "timeout": 25, "allowed_updates": allowed,
	})
	if err != nil {
		return nil, err
	}
	// res = []any (an array of update objects); round-trip it back into tgUpdate.
	b, _ := json.Marshal(res)
	var ups []tgUpdate
	_ = json.Unmarshal(b, &ups)
	return ups, nil
}
