package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"io"
	"net/http"
	"strings"
	"testing"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func bind[T any](t *testing.T, s string) *T {
	t.Helper()
	var x T
	if e := contract.BindInput(json.RawMessage(s), &x); e != nil {
		t.Fatal(e)
	}
	return &x
}
func wire(t *testing.T, c *client, method, path, query, body, reply string) {
	t.Helper()
	c.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if r.Method != method || r.URL.Path != path || r.URL.RawQuery != query {
			t.Fatalf("unexpected target %s %s", r.Method, r.URL)
		}
		b, e := io.ReadAll(r.Body)
		if e != nil {
			t.Fatal(e)
		}
		if string(b) != body {
			t.Fatalf("body %s want %s", b, body)
		}
		if strings.Contains(r.URL.String(), "secret-test") {
			t.Fatal("credential in URL")
		}
		if r.URL.Host != "api.intercom.io" || r.Header.Get("Authorization") != "Bearer secret-test" || r.Header.Get("Intercom-Version") != "2.15" {
			t.Fatal("authentication/version")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(reply)), Request: r}, nil
	})
}

func TestDocumentedWireRequests(t *testing.T) {
	t.Run("health_check", func(t *testing.T) {
		c := newClient(Cred{AccessToken: "secret-test"})
		wire(t, c, `GET`, `/me`, ``, ``, `{"type":"admin","id":"1295","name":"Alex","email":"alex@example.com"}`)
		o, e := c.health_check(context.Background(), bind[HealthCheckIn](t, `{}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
		if !o.OK {
			t.Fatal(o)
		}
	})
	t.Run("list_contacts", func(t *testing.T) {
		c := newClient(Cred{AccessToken: "secret-test"})
		wire(t, c, `GET`, `/contacts`, `per_page=50`, ``, `{"type":"list","data":[{"type":"contact","id":"5ba682d23d7cf92bef87bfd4","role":"user","email":"alex@example.com","name":null,"phone":null,"external_id":null,"unsubscribed_from_emails":false}],"total_count":1,"pages":{"type":"pages","page":1,"per_page":50,"total_pages":1}}`)
		o, e := c.list_contacts(context.Background(), bind[ListContactsIn](t, `{}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
	t.Run("get_contact", func(t *testing.T) {
		c := newClient(Cred{AccessToken: "secret-test"})
		wire(t, c, `GET`, `/contacts/5ba682d23d7cf92bef87bfd4`, ``, ``, `{"type":"contact","id":"5ba682d23d7cf92bef87bfd4","role":"user","email":"alex@example.com","name":null,"phone":null,"external_id":null,"unsubscribed_from_emails":false}`)
		o, e := c.get_contact(context.Background(), bind[GetContactIn](t, `{"contact_id":"5ba682d23d7cf92bef87bfd4"}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
	t.Run("create_contact", func(t *testing.T) {
		c := newClient(Cred{AccessToken: "secret-test"})
		wire(t, c, `POST`, `/contacts`, ``, `{"email":"alex@example.com","role":"user","unsubscribed_from_emails":false}`, `{"type":"contact","id":"5ba682d23d7cf92bef87bfd4","role":"user","email":"alex@example.com","name":null,"phone":null,"external_id":null,"unsubscribed_from_emails":false}`)
		o, e := c.create_contact(context.Background(), bind[CreateContactIn](t, `{"email":"alex@example.com","fields":{"unsubscribed_from_emails":false}}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
	t.Run("update_contact", func(t *testing.T) {
		c := newClient(Cred{AccessToken: "secret-test"})
		wire(t, c, `PUT`, `/contacts/5ba682d23d7cf92bef87bfd4`, ``, `{"unsubscribed_from_emails":false}`, `{"type":"contact","id":"5ba682d23d7cf92bef87bfd4","role":"user","email":"alex@example.com","name":null,"phone":null,"external_id":null,"unsubscribed_from_emails":false}`)
		o, e := c.update_contact(context.Background(), bind[UpdateContactIn](t, `{"contact_id":"5ba682d23d7cf92bef87bfd4","fields":{"unsubscribed_from_emails":false}}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
	t.Run("search_contacts", func(t *testing.T) {
		c := newClient(Cred{AccessToken: "secret-test"})
		wire(t, c, `POST`, `/contacts/search`, ``, `{"pagination":{"per_page":50},"query":{"field":"email","operator":"=","value":"alex@example.com"}}`, `{"type":"list","data":[{"type":"contact","id":"5ba682d23d7cf92bef87bfd4","role":"user","email":"alex@example.com","name":null,"phone":null,"external_id":null,"unsubscribed_from_emails":false}],"total_count":1,"pages":{"type":"pages","page":1,"per_page":50,"total_pages":1}}`)
		o, e := c.search_contacts(context.Background(), bind[SearchContactsIn](t, `{"email":"alex@example.com"}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
	t.Run("list_conversations", func(t *testing.T) {
		c := newClient(Cred{AccessToken: "secret-test"})
		wire(t, c, `GET`, `/conversations`, `per_page=50`, ``, `{"type":"conversation.list","conversations":[{"type":"conversation","id":"503","title":null,"state":"open","open":true,"read":false,"created_at":1663597223,"updated_at":1663597260}],"total_count":1,"pages":{"type":"pages","page":1,"per_page":50,"total_pages":1}}`)
		o, e := c.list_conversations(context.Background(), bind[ListConversationsIn](t, `{}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
	t.Run("get_conversation", func(t *testing.T) {
		c := newClient(Cred{AccessToken: "secret-test"})
		wire(t, c, `GET`, `/conversations/503`, `display_as=plaintext`, ``, `{"type":"conversation","id":"503","title":null,"state":"open","open":true,"read":false,"created_at":1663597223,"updated_at":1663597260,"source":{"body":"Hello"},"conversation_parts":{"type":"conversation_part.list","conversation_parts":[],"total_count":0}}`)
		o, e := c.get_conversation(context.Background(), bind[GetConversationIn](t, `{"conversation_id":"503"}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
	t.Run("reply_conversation", func(t *testing.T) {
		c := newClient(Cred{AccessToken: "secret-test"})
		wire(t, c, `POST`, `/conversations/503/reply`, ``, `{"admin_id":"1295","body":"Hello","message_type":"comment","type":"admin"}`, `{"type":"conversation","id":"503","title":null,"state":"open","open":true,"read":false,"created_at":1663597223,"updated_at":1663597260}`)
		o, e := c.reply_conversation(context.Background(), bind[ReplyConversationIn](t, `{"conversation_id":"503","admin_id":"1295","body":"Hello"}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
}
