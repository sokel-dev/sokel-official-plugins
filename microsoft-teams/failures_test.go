package main

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestDelegatedPreflightRejectsAppOnlyWrites(t *testing.T) {
	calls := 0
	a := fakeAPI(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/v1.0/me" {
			t.Fatal("app-only token reached message write")
		}
		w.WriteHeader(400)
		reply(w, `{"error":{"message":"test-secret app-only"}}`)
	})
	if _, e := a.send_chat_message(context.Background(), &SendChatMessageIn{ChatID: "chat", Content: "Hello"}); e == nil || strings.Contains(e.Error(), "test-secret") || calls != 1 {
		t.Fatal(e, calls)
	}
}
func TestTeamsPostPathsAndValidation(t *testing.T) {
	paths := []string{}
	a := fakeAPI(func(w http.ResponseWriter, r *http.Request) {
		if identity(t, w, r) {
			return
		}
		paths = append(paths, r.URL.Path)
		w.WriteHeader(201)
		reply(w, `{"id":"new"}`)
	})
	c := context.Background()
	if _, e := a.send_channel_message(c, &SendChannelMessageIn{TeamID: "t", ChannelID: "19:c@thread.tacv2", ReplyToID: "m", Content: "reply"}); e != nil {
		t.Fatal(e)
	}
	if _, e := a.send_chat_message(c, &SendChatMessageIn{ChatID: "19:x@thread.v2", Content: "hi"}); e != nil {
		t.Fatal(e)
	}
	if len(paths) != 2 || paths[0] != "/v1.0/teams/t/channels/19:c@thread.tacv2/messages/m/replies" || paths[1] != "/v1.0/chats/19:x@thread.v2/messages" {
		t.Fatal(paths)
	}
	a = fakeAPI(func(w http.ResponseWriter, r *http.Request) { t.Fatal("invalid input reached network") })
	for _, in := range []SendChatMessageIn{{ChatID: "../other", Content: "hi"}, {ChatID: "x", Content: " "}, {ChatID: "x", Content: strings.Repeat("x", 65537)}, {ChatID: "x", Content: "hi", ContentType: "markdown"}} {
		if _, e := a.send_chat_message(c, &in); e == nil {
			t.Fatal("invalid message accepted")
		}
	}
	a = fakeAPI(func(w http.ResponseWriter, r *http.Request) {
		if identity(t, w, r) {
			return
		}
		w.WriteHeader(201)
		reply(w, "{}")
	})
	if _, e := a.send_chat_message(c, &SendChatMessageIn{ChatID: "x", Content: "hi"}); e == nil {
		t.Fatal("unconfirmed message accepted")
	}
}
