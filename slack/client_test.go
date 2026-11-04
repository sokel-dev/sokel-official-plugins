package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHistoryCursorAndAuth(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != "GET" || r.URL.Path != "/conversations.history" || r.URL.Query().Get("cursor") != "opaque+cursor/=" || r.URL.Query().Get("limit") != "15" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("wrong wire request: %s %s", r.Method, r.URL.Path)
		}
		io.WriteString(w, `{"ok":true,"messages":[{"type":"message","user":"U123ABC456","text":"I find you punny and would like to smell your nose letter","ts":"1512085950.000216"}],"has_more":true,"response_metadata":{"next_cursor":"next"}}`)
	}))
	defer s.Close()
	c := &client{base: s.URL, token: "secret", http: s.Client()}
	out, err := c.history(context.Background(), &SlackHistoryIn{Channel: "C123", Cursor: "opaque+cursor/="})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Messages) != 1 || out.NextCursor != "next" || !out.HasMore {
		t.Fatalf("bad output %+v", out)
	}
}
func TestSendBytes(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		var b map[string]any
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			t.Fatal(err)
		}
		if r.Method != "POST" || r.URL.Path != "/chat.postMessage" || b["channel"] != "C123" || b["text"] != "你好 & <world>" || b["thread_ts"] != "1512085950.000216" {
			t.Errorf("wrong request %+v", b)
		}
		io.WriteString(w, `{"ok":true,"channel":"C123","ts":"1512085951.000001"}`)
	}))
	defer s.Close()
	c := &client{base: s.URL, token: "secret", http: s.Client()}
	out, err := c.send(context.Background(), &SlackSendIn{Channel: "C123", Text: "你好 & <world>", ThreadTs: "1512085950.000216"})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || out.Ts != "1512085951.000001" {
		t.Fatal("missing send receipt")
	}
}
