package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestBusinessReceiptsAndErrorRedaction(t *testing.T) {
	t.Run("list_persons", func(t *testing.T) {
		for _, reply := range []string{`{}`, `null`, `[]`, `{"success":false,"error":"secret-test"}`, `{"success":true,"data":null}`, `{"success":true,"data":{}}`} {
			c := fresh()
			withReply(c, 200, reply)
			_, e := c.list_persons(context.Background(), bind[ListPersonsIn](t, `{}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("reply=%s err=%v", reply, e)
			}
		}
		c := fresh()
		withReply(c, 200, `{"success":true,"data":[{"id":9007199254740993,"name":"Alex","org_id":null,"emails":[],"phones":[]},{"id":9007199254740993,"name":"Alex","org_id":null,"emails":[],"phones":[]}],"additional_data":{"next_cursor":null}}`)
		if _, e := c.list_persons(context.Background(), bind[ListPersonsIn](t, `{}`)); e == nil {
			t.Fatal("duplicate IDs accepted")
		}
	})
	t.Run("list_organizations", func(t *testing.T) {
		for _, reply := range []string{`{}`, `null`, `[]`, `{"success":false,"error":"secret-test"}`, `{"success":true,"data":null}`, `{"success":true,"data":{}}`} {
			c := fresh()
			withReply(c, 200, reply)
			_, e := c.list_organizations(context.Background(), bind[ListOrganizationsIn](t, `{}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("reply=%s err=%v", reply, e)
			}
		}
		c := fresh()
		withReply(c, 200, `{"success":true,"data":[{"id":9007199254740993,"name":"Acme"},{"id":9007199254740993,"name":"Acme"}],"additional_data":{"next_cursor":null}}`)
		if _, e := c.list_organizations(context.Background(), bind[ListOrganizationsIn](t, `{}`)); e == nil {
			t.Fatal("duplicate IDs accepted")
		}
	})
	t.Run("list_deals", func(t *testing.T) {
		for _, reply := range []string{`{}`, `null`, `[]`, `{"success":false,"error":"secret-test"}`, `{"success":true,"data":null}`, `{"success":true,"data":{}}`} {
			c := fresh()
			withReply(c, 200, reply)
			_, e := c.list_deals(context.Background(), bind[ListDealsIn](t, `{}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("reply=%s err=%v", reply, e)
			}
		}
		c := fresh()
		withReply(c, 200, `{"success":true,"data":[{"id":9007199254740993,"title":"Sale","status":"open","value":0,"currency":"USD","person_id":null,"org_id":null},{"id":9007199254740993,"title":"Sale","status":"open","value":0,"currency":"USD","person_id":null,"org_id":null}],"additional_data":{"next_cursor":null}}`)
		if _, e := c.list_deals(context.Background(), bind[ListDealsIn](t, `{}`)); e == nil {
			t.Fatal("duplicate IDs accepted")
		}
	})
	t.Run("get_person", func(t *testing.T) {
		for _, reply := range []string{`{}`, `null`, `[]`, `{"success":false,"error":"secret-test"}`, `{"success":true,"data":null}`, `{"success":true,"data":{}}`} {
			c := fresh()
			withReply(c, 200, reply)
			_, e := c.get_person(context.Background(), bind[GetPersonIn](t, `{"person_id":"9007199254740993"}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("reply=%s err=%v", reply, e)
			}
		}
	})
	t.Run("create_person", func(t *testing.T) {
		for _, reply := range []string{`{}`, `null`, `[]`, `{"success":false,"error":"secret-test"}`, `{"success":true,"data":null}`, `{"success":true,"data":{}}`} {
			c := fresh()
			withReply(c, 200, reply)
			_, e := c.create_person(context.Background(), bind[CreatePersonIn](t, `{"name":"Alex","fields":{"emails":[{"value":"alex@example.com","primary":false}]}}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("reply=%s err=%v", reply, e)
			}
		}
	})
	t.Run("create_organization", func(t *testing.T) {
		for _, reply := range []string{`{}`, `null`, `[]`, `{"success":false,"error":"secret-test"}`, `{"success":true,"data":null}`, `{"success":true,"data":{}}`} {
			c := fresh()
			withReply(c, 200, reply)
			_, e := c.create_organization(context.Background(), bind[CreateOrganizationIn](t, `{"name":"Acme"}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("reply=%s err=%v", reply, e)
			}
		}
	})
	t.Run("create_deal", func(t *testing.T) {
		for _, reply := range []string{`{}`, `null`, `[]`, `{"success":false,"error":"secret-test"}`, `{"success":true,"data":null}`, `{"success":true,"data":{}}`} {
			c := fresh()
			withReply(c, 200, reply)
			_, e := c.create_deal(context.Background(), bind[CreateDealIn](t, `{"title":"Sale","fields":{"value":"0","currency":"USD"}}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("reply=%s err=%v", reply, e)
			}
		}
	})
	t.Run("update_deal", func(t *testing.T) {
		for _, reply := range []string{`{}`, `null`, `[]`, `{"success":false,"error":"secret-test"}`, `{"success":true,"data":null}`, `{"success":true,"data":{}}`} {
			c := fresh()
			withReply(c, 200, reply)
			_, e := c.update_deal(context.Background(), bind[UpdateDealIn](t, `{"deal_id":"9007199254740993","fields":{"value":"0"}}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("reply=%s err=%v", reply, e)
			}
		}
	})
}

var _ = json.Valid
