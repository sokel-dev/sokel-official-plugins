package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestBusinessReceiptsAndErrorRedaction(t *testing.T) {
	t.Run("list_contacts", func(t *testing.T) {
		for _, reply := range []string{`{}`, `null`, `[]`, `{"type":"error.list","errors":[{"message":"secret-test"}]}`, `{"type":"contact","id":"bad"}`} {
			c := fresh()
			withReply(c, 200, reply)
			_, e := c.list_contacts(context.Background(), bind[ListContactsIn](t, `{}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("reply=%s err=%v", reply, e)
			}
		}
		c := fresh()
		withReply(c, 200, `{"type":"list","data":[{"type":"contact","id":"5ba682d23d7cf92bef87bfd4","role":"user","email":"alex@example.com","name":null,"phone":null,"external_id":null,"unsubscribed_from_emails":false},{"type":"contact","id":"5ba682d23d7cf92bef87bfd4","role":"user","email":"alex@example.com","name":null,"phone":null,"external_id":null,"unsubscribed_from_emails":false}],"total_count":2,"pages":{"type":"pages","page":1,"per_page":50,"total_pages":1}}`)
		if _, e := c.list_contacts(context.Background(), bind[ListContactsIn](t, `{}`)); e == nil {
			t.Fatal("duplicate IDs accepted")
		}
	})
	t.Run("get_contact", func(t *testing.T) {
		for _, reply := range []string{`{}`, `null`, `[]`, `{"type":"error.list","errors":[{"message":"secret-test"}]}`, `{"type":"contact","id":"bad"}`} {
			c := fresh()
			withReply(c, 200, reply)
			_, e := c.get_contact(context.Background(), bind[GetContactIn](t, `{"contact_id":"5ba682d23d7cf92bef87bfd4"}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("reply=%s err=%v", reply, e)
			}
		}
	})
	t.Run("create_contact", func(t *testing.T) {
		for _, reply := range []string{`{}`, `null`, `[]`, `{"type":"error.list","errors":[{"message":"secret-test"}]}`, `{"type":"contact","id":"bad"}`} {
			c := fresh()
			withReply(c, 200, reply)
			_, e := c.create_contact(context.Background(), bind[CreateContactIn](t, `{"email":"alex@example.com","fields":{"unsubscribed_from_emails":false}}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("reply=%s err=%v", reply, e)
			}
		}
	})
	t.Run("update_contact", func(t *testing.T) {
		for _, reply := range []string{`{}`, `null`, `[]`, `{"type":"error.list","errors":[{"message":"secret-test"}]}`, `{"type":"contact","id":"bad"}`} {
			c := fresh()
			withReply(c, 200, reply)
			_, e := c.update_contact(context.Background(), bind[UpdateContactIn](t, `{"contact_id":"5ba682d23d7cf92bef87bfd4","fields":{"unsubscribed_from_emails":false}}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("reply=%s err=%v", reply, e)
			}
		}
	})
	t.Run("search_contacts", func(t *testing.T) {
		for _, reply := range []string{`{}`, `null`, `[]`, `{"type":"error.list","errors":[{"message":"secret-test"}]}`, `{"type":"contact","id":"bad"}`} {
			c := fresh()
			withReply(c, 200, reply)
			_, e := c.search_contacts(context.Background(), bind[SearchContactsIn](t, `{"email":"alex@example.com"}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("reply=%s err=%v", reply, e)
			}
		}
		c := fresh()
		withReply(c, 200, `{"type":"list","data":[{"type":"contact","id":"5ba682d23d7cf92bef87bfd4","role":"user","email":"alex@example.com","name":null,"phone":null,"external_id":null,"unsubscribed_from_emails":false},{"type":"contact","id":"5ba682d23d7cf92bef87bfd4","role":"user","email":"alex@example.com","name":null,"phone":null,"external_id":null,"unsubscribed_from_emails":false}],"total_count":2,"pages":{"type":"pages","page":1,"per_page":50,"total_pages":1}}`)
		if _, e := c.search_contacts(context.Background(), bind[SearchContactsIn](t, `{"email":"alex@example.com"}`)); e == nil {
			t.Fatal("duplicate IDs accepted")
		}
	})
	t.Run("list_conversations", func(t *testing.T) {
		for _, reply := range []string{`{}`, `null`, `[]`, `{"type":"error.list","errors":[{"message":"secret-test"}]}`, `{"type":"contact","id":"bad"}`} {
			c := fresh()
			withReply(c, 200, reply)
			_, e := c.list_conversations(context.Background(), bind[ListConversationsIn](t, `{}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("reply=%s err=%v", reply, e)
			}
		}
		c := fresh()
		withReply(c, 200, `{"type":"conversation.list","conversations":[{"type":"conversation","id":"503","title":null,"state":"open","open":true,"read":false,"created_at":1663597223,"updated_at":1663597260},{"type":"conversation","id":"503","title":null,"state":"open","open":true,"read":false,"created_at":1663597223,"updated_at":1663597260}],"total_count":2,"pages":{"type":"pages","page":1,"per_page":50,"total_pages":1}}`)
		if _, e := c.list_conversations(context.Background(), bind[ListConversationsIn](t, `{}`)); e == nil {
			t.Fatal("duplicate IDs accepted")
		}
	})
	t.Run("get_conversation", func(t *testing.T) {
		for _, reply := range []string{`{}`, `null`, `[]`, `{"type":"error.list","errors":[{"message":"secret-test"}]}`, `{"type":"contact","id":"bad"}`} {
			c := fresh()
			withReply(c, 200, reply)
			_, e := c.get_conversation(context.Background(), bind[GetConversationIn](t, `{"conversation_id":"503"}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("reply=%s err=%v", reply, e)
			}
		}
	})
	t.Run("reply_conversation", func(t *testing.T) {
		for _, reply := range []string{`{}`, `null`, `[]`, `{"type":"error.list","errors":[{"message":"secret-test"}]}`, `{"type":"contact","id":"bad"}`} {
			c := fresh()
			withReply(c, 200, reply)
			_, e := c.reply_conversation(context.Background(), bind[ReplyConversationIn](t, `{"conversation_id":"503","admin_id":"1295","body":"Hello"}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("reply=%s err=%v", reply, e)
			}
		}
	})
}

var _ = json.Valid
