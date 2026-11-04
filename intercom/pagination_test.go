package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/sokel-dev/sokel-official-plugins/intercom/schema"
	"net/http"
	"testing"
)

const conversationFixture = `{"type":"conversation","id":"9007199254740993","title":null,"state":"closed","open":false,"read":false,"created_at":0,"updated_at":0}`

func TestPaginationMetadata(t *testing.T) {
	for _, tc := range []struct {
		pages, rows string
		total       int
		ok          bool
	}{{`{"page":1,"total_pages":1}`, `[]`, 0, true}, {`null`, `[]`, 0, true}, {`{"page":1,"total_pages":2,"next":{"starting_after":"next+/=\n"}}`, `[{"id":"a"}]`, 2, true}, {`{"page":1,"total_pages":2}`, `[{"id":"a"}]`, 2, false}, {`{"page":1,"total_pages":2,"next":{"starting_after":"prior"}}`, `[{"id":"a"}]`, 2, false}, {`{"page":1,"total_pages":2,"next":{"starting_after":"next"}}`, `[]`, 2, false}, {`null`, `[{"id":"a"}]`, 1, false}, {`{}`, `[]`, 0, false}} {
		b := []byte(fmt.Sprintf(`{"type":"list","data":%s,"total_count":%d,"pages":%s}`, tc.rows, tc.total, tc.pages))
		_, _, e := parsePage(b, "data", "prior", 50)
		if (e == nil) != tc.ok {
			t.Fatalf("%s: %v", b, e)
		}
	}
	for _, p := range []schema.Page{{Limit: ptr(0)}, {Limit: ptr(151)}, {Cursor: ptr("")}, {Cursor: ptr("\x00")}} {
		if _, _, e := pageOptions(p); e == nil {
			t.Fatal(p)
		}
	}
}
func TestConversationPartsLimitAndFalse(t *testing.T) {
	for _, n := range []int{0, 499, 500} {
		var env map[string]any
		_ = json.Unmarshal([]byte(conversationFixture), &env)
		parts := []map[string]any{}
		for i := 0; i < n; i++ {
			parts = append(parts, map[string]any{"id": fmt.Sprint(i + 1), "part_type": "comment", "body": nil, "created_at": 0})
		}
		env["source"] = map[string]any{"body": nil}
		env["conversation_parts"] = map[string]any{"conversation_parts": parts, "total_count": n}
		b, _ := json.Marshal(env)
		c := fresh()
		withReply(c, 200, string(b))
		o, e := c.get_conversation(context.Background(), &GetConversationIn{ConversationID: "9007199254740993"})
		if e != nil || o.PartsMayBeTruncated != (n == 500) || o.Conversation.Open || o.Conversation.Read || o.Conversation.ID != "9007199254740993" {
			t.Fatal(o, e)
		}
	}
	for _, tail := range []string{`"source":null,"conversation_parts":{"conversation_parts":[],"total_count":0}`, `"source":{},"conversation_parts":{"conversation_parts":[],"total_count":-1}`, `"source":{},"conversation_parts":{"conversation_parts":[{"id":"1","part_type":"comment","created_at":-1}],"total_count":1}`} {
		c := fresh()
		withReply(c, 200, conversationFixture[:len(conversationFixture)-1]+`,`+tail+`}`)
		if _, e := c.get_conversation(context.Background(), &GetConversationIn{ConversationID: "9007199254740993"}); e == nil {
			t.Fatal(tail)
		}
	}
}
func TestRegionAndOptionalUpdates(t *testing.T) {
	for region, host := range map[string]string{"": "api.intercom.io", "us": "api.intercom.io", "eu": "api.eu.intercom.io", "au": "api.au.intercom.io"} {
		c := newClient(Cred{AccessToken: "secret-test", Region: region})
		c.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
			if r.URL.Host != host || r.Header.Get("Authorization") != "Bearer secret-test" || r.Header.Get("Intercom-Version") != "2.15" {
				t.Fatal(r.URL, r.Header)
			}
			return httpResponse(200, `{}`), nil
		})
		if _, e := c.request(context.Background(), "GET", "/me", nil, nil); e != nil {
			t.Fatal(e)
		}
	}
	for _, region := range []string{"US", "https://evil.test", "us.evil", "../eu"} {
		if _, e := newClient(Cred{Region: region}).origin(); e == nil {
			t.Fatal(region)
		}
	}
	c := fresh()
	wire(t, c, "PUT", "/contacts/abc", "", `{"name":"","unsubscribed_from_emails":false}`, `{"type":"contact","id":"abc","role":"user","name":"","unsubscribed_from_emails":false}`)
	o, e := c.update_contact(context.Background(), bind[UpdateContactIn](t, `{"contact_id":"abc","fields":{"name":"","unsubscribed_from_emails":false}}`))
	if e != nil || o.Contact.Unsubscribed || o.Contact.Name == nil || *o.Contact.Name != "" {
		t.Fatal(o, e)
	}
	if _, e := fresh().update_contact(context.Background(), &UpdateContactIn{ContactID: "abc"}); e == nil {
		t.Fatal("empty update")
	}
	for _, body := range []string{`{"type":"contact","id":"abc","role":"user"}`, `{"type":"contact","id":"abc","role":"user","unsubscribed_from_emails":null}`} {
		if _, e := contact([]byte(body)); e == nil {
			t.Fatal(body)
		}
	}
	for _, in := range []*ReplyConversationIn{{ConversationID: "1", AdminID: "2", Body: " "}, {ConversationID: "1", AdminID: "2", Body: "ok", MessageType: "user"}, {ConversationID: "../2", AdminID: "2", Body: "ok"}} {
		if _, e := fresh().reply_conversation(context.Background(), in); e == nil {
			t.Fatal(in)
		}
	}
}
