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
		if r.URL.Host != "acme.pipedrive.com" || r.Header.Get("x-api-token") != "secret-test" || r.Header.Get("Authorization") != "" {
			t.Fatal("authentication/origin")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(reply)), Request: r}, nil
	})
}

func TestDocumentedWireRequests(t *testing.T) {
	t.Run("list_persons", func(t *testing.T) {
		c := newClient(Cred{APIToken: "secret-test", CompanyDomain: "acme"})
		wire(t, c, `GET`, `/api/v2/persons`, `limit=100`, ``, `{"success":true,"data":[{"id":9007199254740993,"name":"Alex","org_id":null,"emails":[],"phones":[]}],"additional_data":{"next_cursor":null}}`)
		o, e := c.list_persons(context.Background(), bind[ListPersonsIn](t, `{}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
	t.Run("list_organizations", func(t *testing.T) {
		c := newClient(Cred{APIToken: "secret-test", CompanyDomain: "acme"})
		wire(t, c, `GET`, `/api/v2/organizations`, `limit=100`, ``, `{"success":true,"data":[{"id":9007199254740993,"name":"Acme"}],"additional_data":{"next_cursor":null}}`)
		o, e := c.list_organizations(context.Background(), bind[ListOrganizationsIn](t, `{}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
	t.Run("list_deals", func(t *testing.T) {
		c := newClient(Cred{APIToken: "secret-test", CompanyDomain: "acme"})
		wire(t, c, `GET`, `/api/v2/deals`, `limit=100`, ``, `{"success":true,"data":[{"id":9007199254740993,"title":"Sale","status":"open","value":0,"currency":"USD","person_id":null,"org_id":null}],"additional_data":{"next_cursor":null}}`)
		o, e := c.list_deals(context.Background(), bind[ListDealsIn](t, `{}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
	t.Run("health_check", func(t *testing.T) {
		c := newClient(Cred{APIToken: "secret-test", CompanyDomain: "acme"})
		wire(t, c, `GET`, `/api/v1/users/me`, ``, ``, `{"success":true,"data":{"id":9007199254740993,"name":"Alex","email":"alex@example.com"}}`)
		o, e := c.health_check(context.Background(), bind[HealthCheckIn](t, `{}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
		if !o.OK {
			t.Fatal(o)
		}
	})
	t.Run("get_person", func(t *testing.T) {
		c := newClient(Cred{APIToken: "secret-test", CompanyDomain: "acme"})
		wire(t, c, `GET`, `/api/v2/persons/9007199254740993`, ``, ``, `{"success":true,"data":{"id":9007199254740993,"name":"Alex","org_id":null,"emails":[],"phones":[]}}`)
		o, e := c.get_person(context.Background(), bind[GetPersonIn](t, `{"person_id":"9007199254740993"}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
	t.Run("create_person", func(t *testing.T) {
		c := newClient(Cred{APIToken: "secret-test", CompanyDomain: "acme"})
		wire(t, c, `POST`, `/api/v2/persons`, ``, `{"emails":[{"value":"alex@example.com","primary":false}],"name":"Alex"}`, `{"success":true,"data":{"id":9007199254740993,"name":"Alex","org_id":null,"emails":[],"phones":[]}}`)
		o, e := c.create_person(context.Background(), bind[CreatePersonIn](t, `{"name":"Alex","fields":{"emails":[{"value":"alex@example.com","primary":false}]}}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
	t.Run("create_organization", func(t *testing.T) {
		c := newClient(Cred{APIToken: "secret-test", CompanyDomain: "acme"})
		wire(t, c, `POST`, `/api/v2/organizations`, ``, `{"name":"Acme"}`, `{"success":true,"data":{"id":9007199254740993,"name":"Acme"}}`)
		o, e := c.create_organization(context.Background(), bind[CreateOrganizationIn](t, `{"name":"Acme"}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
	t.Run("create_deal", func(t *testing.T) {
		c := newClient(Cred{APIToken: "secret-test", CompanyDomain: "acme"})
		wire(t, c, `POST`, `/api/v2/deals`, ``, `{"currency":"USD","title":"Sale","value":0}`, `{"success":true,"data":{"id":9007199254740993,"title":"Sale","status":"open","value":0,"currency":"USD","person_id":null,"org_id":null}}`)
		o, e := c.create_deal(context.Background(), bind[CreateDealIn](t, `{"title":"Sale","fields":{"value":"0","currency":"USD"}}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
	t.Run("update_deal", func(t *testing.T) {
		c := newClient(Cred{APIToken: "secret-test", CompanyDomain: "acme"})
		wire(t, c, `PATCH`, `/api/v2/deals/9007199254740993`, ``, `{"value":0}`, `{"success":true,"data":{"id":9007199254740993,"title":"Sale","status":"open","value":0,"currency":"USD","person_id":null,"org_id":null}}`)
		o, e := c.update_deal(context.Background(), bind[UpdateDealIn](t, `{"deal_id":"9007199254740993","fields":{"value":"0"}}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
}
