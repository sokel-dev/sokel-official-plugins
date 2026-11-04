package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-official-plugins/mailchimp/schema"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAllReadOperationsAndGeneratedDispatch(t *testing.T) {
	cases := []struct{ op, input, path, response string }{
		{"health_check", `{}`, "/3.0/ping", file(t, "ping")},
		{"list_audiences", `{"page":{"offset":0,"limit":1}}`, "/3.0/lists", `{"lists":[{"id":"list1","name":"Audience","double_optin":false}],"total_items":2,"_links":[{"href":"https://evil.example"}]}`},
		{"get_audience", `{"list_id":"list1"}`, "/3.0/lists/list1", `{"id":"list1","name":"Audience","double_optin":false}`},
		{"list_members", `{"list_id":"list1","options":{"vip_only":false},"page":{"offset":0,"limit":1},"since_last_changed":"2026-01-01T00:00:00+08:00","status":"subscribed"}`, "/3.0/lists/list1/members", file(t, "members")},
		{"get_member", `{"list_id":"list1","email_address":"Ada.Lovelace@example.com"}`, "/3.0/lists/list1/members/2b9150605ac374d671a306b5fcee60a0", `{"id":"2b9150605ac374d671a306b5fcee60a0","email_address":"ada.lovelace@example.com","status":"subscribed","merge_fields":{"BIG":9007199254740993}}`},
		{"list_member_tags", `{"list_id":"list1","email_address":"ada.lovelace@example.com"}`, "/3.0/lists/list1/members/2b9150605ac374d671a306b5fcee60a0/tags", file(t, "tags")},
		{"list_campaigns", `{"list_id":"list1","status":"sent"}`, "/3.0/campaigns", `{"campaigns":[{"id":"c1","type":"regular","status":"sent","settings":{"title":"Title","subject_line":"Subject"},"recipients":{"list_id":"list1"}}],"total_items":1}`},
	}
	for _, tc := range cases {
		t.Run(tc.op, func(t *testing.T) {
			calls := 0
			c := fixture(t, func(r *http.Request) (int, string) {
				calls++
				if r.URL.Path != tc.path || r.Method != "GET" {
					t.Fatal(r.URL)
				}
				if tc.op == "list_members" && (r.URL.Query().Get("vip_only") != "false" || r.URL.Query().Get("since_last_changed") != "2026-01-01T00:00:00+08:00") {
					t.Fatal(r.URL)
				}
				return 200, tc.response
			})
			out, e := invoke(t, c, tc.op, tc.input)
			if e != nil || out == nil || calls != 1 {
				t.Fatal(out, e, calls)
			}
			if tc.op == "get_member" {
				b, _ := json.Marshal(out)
				if !strings.Contains(string(b), "9007199254740993") {
					t.Fatal(string(b))
				}
			}
			if tc.op == "list_audiences" && (out["has_more"] != true || out["next_offset"] != 1) {
				t.Fatal(out)
			}
		})
	}
}
func TestUpsertGeneratedOmissionAndZero(t *testing.T) {
	for _, options := range []string{``, `,"options":{"vip":false}`} {
		c := fixture(t, func(r *http.Request) (int, string) {
			b, _ := io.ReadAll(r.Body)
			if strings.Contains(string(b), `"status":`) || !strings.Contains(string(b), `"N":0`) || strings.Contains(string(b), `"vip"`) != strings.Contains(options, "vip") {
				t.Fatal(string(b))
			}
			return 200, `{"id":"2b9150605ac374d671a306b5fcee60a0","email_address":"ada.lovelace@example.com","status":"unsubscribed"}`
		})
		_, e := invoke(t, c, "upsert_member", `{"list_id":"list1","email_address":"ada.lovelace@example.com","status_if_new":"pending","merge_fields":{"N":0}`+options+`}`)
		if e != nil {
			t.Fatal(e)
		}
	}
}
func TestInvalidInputNoRequests(t *testing.T) {
	c := fixture(t, func(*http.Request) (int, string) { t.Fatal("invalid input sent"); return 200, "{}" })
	for _, in := range []*UpsertMemberIn{{ListID: "list1", EmailAddress: "user@example.com"}, {ListID: "../x", EmailAddress: "user@example.com", StatusIfNew: "pending"}, {ListID: "list1", EmailAddress: "Name <user@example.com>", StatusIfNew: "pending"}} {
		if _, e := c.upsert_member(context.Background(), in); e == nil {
			t.Fatal(in)
		}
	}
	if _, e := c.list_members(context.Background(), &ListMembersIn{ListID: "list1", Status: "invalid"}); e == nil {
		t.Fatal("status")
	}
	if _, e := c.list_members(context.Background(), &ListMembersIn{ListID: "list1", SinceLastChanged: "yesterday"}); e == nil {
		t.Fatal("time")
	}
	if _, e := c.list_campaigns(context.Background(), &ListCampaignsIn{Status: "invalid"}); e == nil {
		t.Fatal("status")
	}
	if _, e := c.list_member_tags(context.Background(), &ListMemberTagsIn{ListID: "list1", EmailAddress: "a@example.com", Page: schema.PageOptions{Limit: ptr(int64(0))}}); e == nil {
		t.Fatal("limit")
	}
	for _, key := range []string{"x", "x-evil.example", "x-us6.evil", "x-us0", "x-us6/path"} {
		c := newClient(&Cred{APIKey: key})
		if c.initErr == nil {
			t.Fatal(key)
		}
	}
}
func TestMalformedReceiptsAndEmptyPages(t *testing.T) {
	for _, raw := range []string{`{}`, `{"members":[],"total_items":1}`, `{"members":[{}],"total_items":1}`, `{"members":null,"total_items":0}`} {
		c := fixture(t, func(*http.Request) (int, string) { return 200, raw })
		if _, e := c.list_members(context.Background(), &ListMembersIn{ListID: "list1"}); e == nil {
			t.Fatal(raw)
		}
	}
	c := fixture(t, func(*http.Request) (int, string) { return 200, `{"members":[],"total_items":0}` })
	o, e := c.list_members(context.Background(), &ListMembersIn{ListID: "list1"})
	if e != nil || o.Members == nil || o.HasMore {
		t.Fatal(o, e)
	}
	for _, raw := range []string{`{}`, `{"health_status":"not healthy"}`} {
		c := fixture(t, func(*http.Request) (int, string) { return 200, raw })
		o, e := c.health_check(context.Background(), &HealthCheckIn{})
		if e != nil || o.OK {
			t.Fatal(o, e)
		}
	}
	c = fixture(t, func(*http.Request) (int, string) { return 401, `{"detail":"fixture-secret-us6"}` })
	o2, e := c.health_check(context.Background(), &HealthCheckIn{})
	if e != nil || o2.OK || strings.Contains(o2.Message, "fixture-secret") {
		t.Fatal(o2, e)
	}
}

func TestUnsafeMergeNumberRejectedBeforeWrite(t *testing.T) {
	c := fixture(t, func(*http.Request) (int, string) { t.Fatal("unsafe merge number sent"); return 200, "{}" })
	if _, e := invoke(t, c, "upsert_member", `{"list_id":"list1","email_address":"ada.lovelace@example.com","status_if_new":"transactional","merge_fields":{"BIG":9007199254740993}}`); e == nil {
		t.Fatal("unsafe number accepted")
	}
}
