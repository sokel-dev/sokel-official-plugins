package main

import (
	"encoding/json"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"testing"
)

// mapUpdate：一条 update → (事件类型, payload)。按 update 里非空的那个字段判类型，平铺关键字段。
// update → 事件的映射：现在走 typed 的 TriggerXxx，所以用一个假 SourceCtx 录下
// 「推了哪个事件、payload 是什么」。比原先断言 mapUpdate 的返回值更贴近真实路径——
// 事件名与字段名都由生成物定死，这条测的是「哪条 update 对应哪个事件」这层判断。
func TestTriggerUpdate(t *testing.T) {
	parse := func(s string) tgUpdate {
		var u tgUpdate
		if err := json.Unmarshal([]byte(s), &u); err != nil {
			t.Fatalf("parse: %v", err)
		}
		return u
	}
	fire := func(raw string) (*recordingSource, string) {
		rec := &recordingSource{}
		ev, err := triggerUpdate(rec, parse(raw))
		if err != nil {
			t.Fatalf("trigger: %v", err)
		}
		return rec, ev
	}

	rec, ev := fire(`{"update_id":1,"message":{"message_id":9,"text":"hi","from":{"id":42,"username":"bob"},"chat":{"id":100}}}`)
	if ev != "message" || rec.event != "message" || rec.eventID != "1" {
		t.Fatalf("应推 message: ev=%q rec=%+v", ev, rec)
	}
	m, ok := rec.payload.(*MessageEvent)
	if !ok || m.ChatID != 100 || m.UserID != 42 || m.Username != "bob" || m.Text != "hi" || m.MessageID != 9 {
		t.Errorf("message 平铺不对: %+v", rec.payload)
	}

	rec, ev = fire(`{"update_id":2,"callback_query":{"id":"cbq1","data":"vote_a","from":{"id":7},"message":{"message_id":50,"chat":{"id":200}}}}`)
	if ev != "callback_query" {
		t.Fatalf("应推 callback_query: %q", ev)
	}
	c, ok := rec.payload.(*CallbackQueryEvent)
	if !ok || c.CallbackID != "cbq1" || c.CallbackData != "vote_a" || c.UserID != 7 || c.ChatID != 200 || c.MessageID != 50 {
		t.Errorf("callback 平铺不对: %+v", rec.payload)
	}

	if _, ev := fire(`{"update_id":3,"edited_message":{"message_id":9,"text":"fixed","from":{"id":1},"chat":{"id":100}}}`); ev != "edited_message" {
		t.Errorf("应推 edited_message: %q", ev)
	}
	if _, ev := fire(`{"update_id":4,"my_chat_member":{"chat":{"id":300},"from":{"id":9}}}`); ev != "my_chat_member" {
		t.Errorf("应推 my_chat_member: %q", ev)
	}
	// 未订阅/未识别的 update（如 poll）：不推事件，也不报错
	rec, ev = fire(`{"update_id":5,"poll":{"id":"p"}}`)
	if ev != "" || rec.event != "" {
		t.Errorf("未识别类型不该推事件: ev=%q rec=%+v", ev, rec)
	}
}

// recordingSource：只录不发的 plugin.SourceCtx。
type recordingSource struct {
	plugin.SourceCtx
	event, eventID string
	payload        any
}

func (r *recordingSource) Trigger(event, eventID string, payload any) error {
	r.event, r.eventID, r.payload = event, eventID, payload
	return nil
}
