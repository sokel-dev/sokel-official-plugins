package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-official-plugins/pipedrive/schema"
	"net/http"
	"strings"
	"testing"
)

func TestCursorAndExactValues(t *testing.T) {
	for _, tc := range []struct {
		next, rows string
		ok         bool
	}{{`null`, `[{"id":9007199254740993,"name":"Alex"}]`, true}, {`"next+/="`, `[{"id":1,"name":"Alex"}]`, true}, {`"prior"`, `[{"id":1,"name":"Alex"}]`, false}, {`""`, `[]`, false}, {`5`, `[]`, false}, {`"next"`, `[]`, false}} {
		c := fresh()
		withReply(c, 200, `{"success":true,"data":`+tc.rows+`,"additional_data":{"next_cursor":`+tc.next+`}}`)
		o, e := c.list_persons(context.Background(), &ListPersonsIn{Page: schema.Page{Cursor: ptr("prior")}})
		if (e == nil) != tc.ok {
			t.Fatalf("%+v: %v", tc, e)
		}
		if tc.ok && tc.next == `null` && (o.Persons[0].ID != "9007199254740993" || o.HasMore) {
			t.Fatal(o)
		}
	}
	for _, s := range []string{`9007199254740993`, `"9007199254740993"`} {
		var id exactID
		if json.Unmarshal([]byte(s), &id) != nil || id != "9007199254740993" {
			t.Fatal(id)
		}
	}
	for _, s := range []string{`null`, `1.2`, `-1`, `0`, `1e3`, `"x"`, `"01"`} {
		var id exactID
		if json.Unmarshal([]byte(s), &id) == nil {
			t.Fatal(s)
		}
	}
	for _, s := range []string{`9007199254740993.01`, `"9007199254740993.01"`, `0`, `1.2e+3`} {
		var v exactMoney
		if json.Unmarshal([]byte(s), &v) != nil {
			t.Fatal(s)
		}
	}
	for _, s := range []string{`null`, `true`, `"NaN"`, `"01"`} {
		var v exactMoney
		if json.Unmarshal([]byte(s), &v) == nil {
			t.Fatal(s)
		}
	}
	v, e := deal([]byte(`{"id":9007199254740993,"title":"Deal","status":"open","value":9007199254740993.01,"currency":"USD","person_id":9007199254740995,"org_id":null}`))
	if e != nil || v.ID != "9007199254740993" || v.Value != "9007199254740993.01" || *v.PersonID != "9007199254740995" || v.OrgID != nil {
		t.Fatal(v, e)
	}
}
func TestBoundedCompanyAndValidation(t *testing.T) {
	for _, s := range []string{"https://acme.pipedrive.com", "acme.evil.test", "x@evil", "-x", "x-", "UPPER", "", "a/b", strings.Repeat("a", 64)} {
		c := newClient(Cred{CompanyDomain: s, APIToken: "secret-test"})
		if _, e := c.origin(); e == nil {
			t.Fatal(s)
		}
	}
	for _, p := range []schema.Page{{Limit: ptr(0)}, {Limit: ptr(501)}, {Cursor: ptr("")}, {Cursor: ptr("\x00")}} {
		if _, _, e := fresh().page(context.Background(), "/api/v2/persons", p); e == nil {
			t.Fatal(p)
		}
	}
	for _, f := range []schema.DealFields{{Value: ptr("-1")}, {Value: ptr("1e3")}, {Currency: ptr("usd")}, {PersonID: ptr("1/2")}, {OrgID: ptr("0")}, {Status: ptr("deleted")}} {
		if _, e := dealBody("title", f); e == nil {
			t.Fatal(f)
		}
	}
	if _, e := dealBody("", schema.DealFields{}); e == nil {
		t.Fatal("empty update")
	}
	for _, a := range [][]schema.Address{{{Value: "not email"}}, {{Value: "a@b.test", Primary: ptr(true)}, {Value: "b@b.test", Primary: ptr(true)}}, {{Value: "a@b.test", Label: ptr(strings.Repeat("x", 129))}}} {
		if addresses(a, true) == nil {
			t.Fatal(a)
		}
	}
	c := fresh()
	c.http.Transport = transport(func(*http.Request) (*http.Response, error) { t.Fatal("invalid input sent"); return nil, nil })
	if _, e := c.create_person(context.Background(), &CreatePersonIn{Name: " "}); e == nil {
		t.Fatal("blank name")
	}
	if _, e := c.create_organization(context.Background(), &CreateOrganizationIn{Name: " "}); e == nil {
		t.Fatal("blank org")
	}
	if _, e := c.create_deal(context.Background(), &CreateDealIn{Title: " "}); e == nil {
		t.Fatal("blank deal")
	}
}
func TestOptionalFieldsSurviveBinding(t *testing.T) {
	c := fresh()
	wire(t, c, "POST", "/api/v2/persons", "", `{"emails":[{"value":"alex@example.com","primary":false}],"name":"Alex"}`, `{"success":true,"data":{"id":1,"name":"Alex","emails":[{"value":"alex@example.com","primary":false}]}}`)
	o, e := c.create_person(context.Background(), bind[CreatePersonIn](t, `{"name":"Alex","fields":{"emails":[{"value":"alex@example.com","primary":false}]}}`))
	if e != nil || o.Person.Emails[0].Primary == nil || *o.Person.Emails[0].Primary {
		t.Fatal(o, e)
	}
	b, e := dealBody("", bind[UpdateDealIn](t, `{"deal_id":"1","fields":{"value":"0"}}`).Fields)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(b)
	if string(raw) != `{"value":0}` {
		t.Fatal(string(raw))
	}
}
