package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Synthetic examples based on official API shapes. No live account captures.
type handlerTransport struct{ h http.HandlerFunc }

func (t handlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	t.h(w, r)
	res := w.Result()
	res.Request = r
	return res, nil
}
func fakeAPI(h http.HandlerFunc) *api {
	a := newAPI("test-secret")
	a.client = &http.Client{Transport: handlerTransport{h}}
	return a
}
func reply(w http.ResponseWriter, s string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, s)
}
func body(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var m map[string]any
	if e := json.NewDecoder(r.Body).Decode(&m); e != nil {
		t.Fatal(e)
	}
	return m
}

type fileCtx struct {
	context.Context
	bytes []byte
	fail  bool
}

func (c *fileCtx) Credential() map[string]string {
	return map[string]string{"access_token": "test-secret"}
}
func (*fileCtx) Fetch(*plugin.File) ([]byte, error) { return nil, fmt.Errorf("unexpected fetch") }
func (c *fileCtx) Upload(n, m string, b []byte) (*plugin.File, error) {
	if c.fail {
		return nil, fmt.Errorf("test-secret must be hidden")
	}
	c.bytes = append([]byte{}, b...)
	return &plugin.File{ID: "stored", Name: n, Mime: m, Size: int64(len(b))}, nil
}
func (c *fileCtx) UploadReader(n, m string, r io.Reader) (*plugin.File, error) {
	b, e := io.ReadAll(r)
	if e != nil {
		return nil, e
	}
	return c.Upload(n, m, b)
}
func identity(t *testing.T, w http.ResponseWriter, r *http.Request) bool {
	t.Helper()
	if r.URL.Host != "graph.microsoft.com" || r.Header.Get("Authorization") != "Bearer test-secret" {
		t.Fatal("origin or auth")
	}
	if r.URL.Path == "/v1.0/me" {
		if r.Method != "GET" || r.URL.Query().Get("$select") != "id" {
			t.Fatal("identity must be read-only")
		}
		reply(w, `{"id":"viewer"}`)
		return true
	}
	return false
}
func TestReadOnlyHealth(t *testing.T) {
	calls := 0
	a := fakeAPI(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if !identity(t, w, r) {
			t.Fatal("unexpected identity request")
		}
	})
	o, e := a.health_check(context.Background(), &HealthCheckIn{})
	if e != nil || o == nil || !o.OK || calls != 1 {
		t.Fatalf("health %v %v calls=%d", o, e, calls)
	}
}

var _ = strings.Contains

func TestTeamsProtocol(t *testing.T) {
	posts := 0
	a := fakeAPI(func(w http.ResponseWriter, r *http.Request) {
		if identity(t, w, r) {
			return
		}
		switch {
		case r.Method == "POST":
			posts++
			m := body(t, r)
			if m["body"].(map[string]any)["content"] != "Hello 中文" || m["body"].(map[string]any)["contentType"] != "text" {
				t.Fatal(m)
			}
			w.WriteHeader(201)
			reply(w, `{"id":"new","body":{"contentType":"text","content":"Hello 中文"}}`)
		case strings.HasSuffix(r.URL.Path, "/joinedTeams"):
			if r.URL.RawQuery != "" {
				t.Fatal("joinedTeams has no query support")
			}
			reply(w, `{"value":[{"id":"team","displayName":"Research","description":"Notes"}]}`)
		case strings.HasSuffix(r.URL.Path, "/channels"):
			if r.URL.Query().Get("$top") != "" {
				t.Fatal("channels top unsupported")
			}
			reply(w, `{"value":[{"id":"19:channel@thread.tacv2","displayName":"General","membershipType":"standard"}]}`)
		case strings.HasSuffix(r.URL.Path, "/chats"):
			reply(w, `{"value":[{"id":"19:chat@thread.v2","chatType":"group","topic":"Research"}]}`)
		default:
			if r.URL.Query().Get("$top") != "2" {
				t.Fatal(r.URL.RawQuery)
			}
			reply(w, `{"value":[{"id":"m","body":{"contentType":"html","content":"<p>hi</p>"},"from":{"user":{"id":"u","displayName":"User"}}}]}`)
		}
	})
	c := context.Background()
	if o, e := a.list_teams(c, &ListTeamsIn{}); e != nil || o == nil || len(o.Teams) != 1 {
		t.Fatalf("teams %v %v", o, e)
	}
	if o, e := a.list_channels(c, &ListChannelsIn{TeamID: "team"}); e != nil || o == nil || len(o.Channels) != 1 {
		t.Fatalf("channels %v %v", o, e)
	}
	if o, e := a.list_chats(c, &ListChatsIn{Limit: 2}); e != nil || o == nil || len(o.Chats) != 1 {
		t.Fatalf("chats %v %v", o, e)
	}
	if o, e := a.list_channel_messages(c, &ListChannelMessagesIn{TeamID: "team", ChannelID: "19:channel@thread.tacv2", Limit: 2}); e != nil || o == nil || len(o.Messages) != 1 || o.Messages[0].AuthorID != "u" {
		t.Fatalf("messages %v %v", o, e)
	}
	if o, e := a.list_replies(c, &ListRepliesIn{TeamID: "team", ChannelID: "19:channel@thread.tacv2", MessageID: "m", Limit: 2}); e != nil || o == nil || len(o.Messages) != 1 {
		t.Fatalf("replies %v %v", o, e)
	}
	if o, e := a.list_chat_messages(c, &ListChatMessagesIn{ChatID: "19:chat@thread.v2", Limit: 2}); e != nil || o == nil || len(o.Messages) != 1 {
		t.Fatalf("chat messages %v %v", o, e)
	}
	if o, e := a.send_channel_message(c, &SendChannelMessageIn{TeamID: "team", ChannelID: "19:channel@thread.tacv2", Content: "Hello 中文", ReplyToID: "m"}); e != nil || o == nil || o.Message.ID != "new" {
		t.Fatalf("send channel %v %v", o, e)
	}
	if o, e := a.send_chat_message(c, &SendChatMessageIn{ChatID: "19:chat@thread.v2", Content: "Hello 中文"}); e != nil || o == nil || o.Message.ID != "new" || posts != 2 {
		t.Fatalf("send chat %v %v %d", o, e, posts)
	}
}
