package main

import (
	"reflect"
	"testing"
)

// compact 去零值：Telegram 对空 parse_mode / 0 reply_to_message_id / false 开关敏感，
// 传了空值反而报错或改变行为，必须剔除。非零值全保留。
func TestCompact(t *testing.T) {
	got := compact(map[string]any{
		"chat_id":                  int64(123), // 非零保留
		"text":                     "hi",       // 非空保留
		"parse_mode":               "",         // 空串剔除
		"reply_to_message_id":      int64(0),   // 0 剔除
		"disable_web_page_preview": false,      // false 剔除
		"show_alert":               true,       // true 保留
		"reply_markup":             nil,        // nil 剔除
	})
	want := map[string]any{"chat_id": int64(123), "text": "hi", "show_alert": true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("compact = %v, want %v", got, want)
	}
}

// typed nil 也必须剔除：SendMessageIn.ReplyMarkup 是 map[string]any，入参没给时
// 它是**带类型的 nil**（interface{type: map, value: nil}），不命中 case nil，
// 曾被保留并 marshal 成 "reply_markup": null → Telegram 400 "object expected as
// reply markup"（run_a433bdca，2026-08-21：兜底通知全灭）。切片同理。
func TestCompactStripsTypedNil(t *testing.T) {
	var m map[string]any
	var sl []any
	got := compact(map[string]any{"chat_id": int64(1), "text": "x", "reply_markup": m, "entities": sl})
	want := map[string]any{"chat_id": int64(1), "text": "x"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("compact = %v, want %v", got, want)
	}
}

// chat_id 允许字符串（@channelusername）——不能被当空值误删。
func TestCompactKeepsUsernameChatID(t *testing.T) {
	got := compact(map[string]any{"chat_id": "@mychannel", "text": "x"})
	if got["chat_id"] != "@mychannel" {
		t.Errorf("username chat_id 应保留: %v", got)
	}
}
