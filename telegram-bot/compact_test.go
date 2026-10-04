package main

import (
	"reflect"
	"testing"
)

// compact strips zero values: Telegram is picky about empty parse_mode / 0 reply_to_message_id /
// false switches — passing the zero value instead causes an error or changes behavior, so it must
// be dropped. Non-zero values are all kept.
func TestCompact(t *testing.T) {
	got := compact(map[string]any{
		"chat_id":                  int64(123), // non-zero, kept
		"text":                     "hi",       // non-empty, kept
		"parse_mode":               "",         // empty string, dropped
		"reply_to_message_id":      int64(0),   // 0, dropped
		"disable_web_page_preview": false,      // false, dropped
		"show_alert":               true,       // true, kept
		"reply_markup":             nil,        // nil, dropped
	})
	want := map[string]any{"chat_id": int64(123), "text": "hi", "show_alert": true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("compact = %v, want %v", got, want)
	}
}

// A typed nil must also be dropped: SendMessageIn.ReplyMarkup is map[string]any, and when the
// input isn't given it's a **typed nil** (interface{type: map, value: nil}), which doesn't match
// `case nil`. It used to slip through and get marshaled as "reply_markup": null → Telegram 400
// "object expected as reply markup" (run_a433bdca, 2026-08-21: wiped out all fallback
// notifications). Slices have the same issue.
func TestCompactStripsTypedNil(t *testing.T) {
	var m map[string]any
	var sl []any
	got := compact(map[string]any{"chat_id": int64(1), "text": "x", "reply_markup": m, "entities": sl})
	want := map[string]any{"chat_id": int64(1), "text": "x"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("compact = %v, want %v", got, want)
	}
}

// chat_id may be a string (@channelusername) — it must not be mistaken for a zero value and dropped.
func TestCompactKeepsUsernameChatID(t *testing.T) {
	got := compact(map[string]any{"chat_id": "@mychannel", "text": "x"})
	if got["chat_id"] != "@mychannel" {
		t.Errorf("username chat_id 应保留: %v", got)
	}
}
