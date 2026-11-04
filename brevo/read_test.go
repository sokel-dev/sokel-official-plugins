package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-official-plugins/brevo/schema"
	"io"
	"net/http"
	"strings"
	"testing"
)

const contactFixture = `{"id":9007199254740993,"email":"contact@example.com","emailBlacklisted":false,"smsBlacklisted":true,"listIds":[9007199254740995],"attributes":{"BIG":9007199254740997}}`
const listFixture = `{"id":53,"name":"List","folderId":1,"uniqueSubscribers":0,"totalSubscribers":0,"totalBlacklisted":0}`

func TestReadOperationsAndGeneratedDispatch(t *testing.T) {
	for _, tc := range []struct{ op, input, path, response string }{
		{"health_check", `{}`, "/v3/account", `{"email":"michael.davis@example.com","marketingAutomation":{"key":"upstream-secret"}}`},
		{"list_contacts", `{"page":{"offset":0,"limit":2},"modified_since":"2026-01-01T00:00:00+08:00"}`, "/v3/contacts", `{"contacts":[` + contactFixture + `],"count":3}`},
		{"get_contact", `{"contact_id":"9007199254740993"}`, "/v3/contacts/9007199254740993", contactFixture},
		{"list_lists", `{}`, "/v3/contacts/lists", `{"lists":[` + listFixture + `],"count":1}`},
		{"get_list", `{"list_id":"53"}`, "/v3/contacts/lists/53", listFixture},
	} {
		t.Run(tc.op, func(t *testing.T) {
			c := fixture(t, func(r *http.Request) (int, string) {
				if r.URL.Path != tc.path || r.Method != "GET" {
					t.Fatal(r.URL)
				}
				if tc.op == "list_contacts" && (r.URL.Query().Get("offset") != "0" || r.URL.Query().Get("modifiedSince") != "2026-01-01T00:00:00+08:00") {
					t.Fatal(r.URL)
				}
				return 200, tc.response
			})
			o, e := invoke(t, c, tc.op, tc.input)
			if e != nil || o == nil {
				t.Fatal(o, e)
			}
			b, _ := json.Marshal(o)
			if strings.Contains(string(b), "upstream-secret") {
				t.Fatal("account secrets exposed")
			}
			if tc.op == "get_contact" && !strings.Contains(string(b), "9007199254740997") {
				t.Fatal(string(b))
			}
			if tc.op == "list_contacts" && (o["has_more"] != true || o["next_offset"] != 1) {
				t.Fatal(o)
			}
		})
	}
}
func TestGeneratedWritesFalseZeroAndReceipt(t *testing.T) {
	for _, tc := range []struct {
		op, input string
		status    int
		response  string
	}{
		{"create_contact", `{"email":"new@example.com","email_status":"allowed","attributes":{"N":0,"FLAG":false},"list_ids":["9007199254740993"]}`, 201, file(t, "create")},
		{"update_contact", `{"contact_id":"21","attributes":{"N":0},"options":{"email_blacklisted":false}}`, 204, ""},
		{"send_email", `{"sender_email":"sender@example.com","to":[{"email":"contact@example.com","name":"Reader"}],"subject":"Hello","content_type":"html","body":"<p>Hello</p>"}`, 201, file(t, "send")},
	} {
		t.Run(tc.op, func(t *testing.T) {
			c := fixture(t, func(r *http.Request) (int, string) {
				b, _ := io.ReadAll(r.Body)
				s := string(b)
				if tc.op != "send_email" && !strings.Contains(s, `"N":0`) {
					t.Fatal(s)
				}
				if tc.op == "create_contact" && (!strings.Contains(s, `"emailBlacklisted":false`) || !strings.Contains(s, "9007199254740993") || !strings.Contains(s, `"FLAG":false`)) {
					t.Fatal(s)
				}
				if tc.op == "update_contact" && !strings.Contains(s, `"emailBlacklisted":false`) {
					t.Fatal(s)
				}
				if tc.op == "send_email" && (!strings.Contains(s, "htmlContent") || strings.Contains(s, "textContent")) {
					t.Fatal(s)
				}
				return tc.status, tc.response
			})
			if _, e := invoke(t, c, tc.op, tc.input); e != nil {
				t.Fatal(e)
			}
		})
	}
	c := fixture(t, func(r *http.Request) (int, string) {
		b, _ := io.ReadAll(r.Body)
		if strings.Contains(string(b), "emailBlacklisted") {
			t.Fatal(string(b))
		}
		return 204, ""
	})
	if _, e := invoke(t, c, "update_contact", `{"contact_id":"21","attributes":{"N":0}}`); e != nil {
		t.Fatal(e)
	}
}
func TestInvalidInputsNoRequests(t *testing.T) {
	c := fixture(t, func(*http.Request) (int, string) { t.Fatal("invalid request sent"); return 200, "{}" })
	if _, e := c.create_contact(context.Background(), &CreateContactIn{Email: "a@example.com"}); e == nil {
		t.Fatal("missing consent")
	}
	for _, a := range []map[string]any{{"EMAIL": "new@example.com"}, {"bad": "x"}, {"N": nil}, {"N": map[string]any{"x": 1}}, {"N": []any{1}}, {"N": float64(9007199254740992)}} {
		if _, e := c.update_contact(context.Background(), &UpdateContactIn{ContactID: "21", Attributes: a}); e == nil {
			t.Fatal(a)
		}
	}
	if _, e := c.update_contact(context.Background(), &UpdateContactIn{ContactID: "21"}); e == nil {
		t.Fatal("empty update")
	}
	for _, id := range []string{"0", "-1", "../account", "1.2", "9223372036854775808"} {
		if _, e := c.get_contact(context.Background(), &GetContactIn{ContactID: id}); e == nil {
			t.Fatal(id)
		}
	}
	for _, in := range []*SendEmailIn{{}, {SenderEmail: "a@example.com", To: []schema.Recipient{{Email: "a@example.com"}}, Subject: "X\nBcc:bad", Body: "x", ContentType: "text"}, {SenderEmail: "a@example.com", To: []schema.Recipient{{Email: "a@example.com"}, {Email: "A@example.com"}}, Subject: "X", Body: "x", ContentType: "text"}} {
		if _, e := c.send_email(context.Background(), in); e == nil {
			t.Fatal(in)
		}
	}
	if _, e := c.list_lists(context.Background(), &ListListsIn{Page: schema.PageOptions{Limit: ptr(int64(51))}}); e == nil {
		t.Fatal("list max")
	}
}
func TestMalformedSuccessAndEmptyLists(t *testing.T) {
	for _, raw := range []string{`{}`, `{"id":0}`, `{"id":1.5}`} {
		c := fixture(t, func(*http.Request) (int, string) { return 201, raw })
		if _, e := c.create_contact(context.Background(), &CreateContactIn{Email: "a@example.com", EmailStatus: "blocked"}); e == nil {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{`{}`, `{"messageId":""}`} {
		c := fixture(t, func(*http.Request) (int, string) { return 201, raw })
		if _, e := c.send_email(context.Background(), &SendEmailIn{SenderEmail: "a@example.com", To: []schema.Recipient{{Email: "b@example.com"}}, Subject: "X", Body: "x", ContentType: "text"}); e == nil {
			t.Fatal(raw)
		}
	}
	c := fixture(t, func(*http.Request) (int, string) { return 200, `{"lists":[],"count":0}` })
	o, e := c.list_lists(context.Background(), &ListListsIn{})
	if e != nil || o.Lists == nil || o.HasMore {
		t.Fatal(o, e)
	}
	c = fixture(t, func(*http.Request) (int, string) { return 200, `{}` })
	h, e := c.health_check(context.Background(), &HealthCheckIn{})
	if e != nil || h.OK {
		t.Fatal(h, e)
	}
	c = fixture(t, func(*http.Request) (int, string) { return 401, `{"message":"fixture-secret"}` })
	h, e = c.health_check(context.Background(), &HealthCheckIn{})
	if e != nil || h.OK || strings.Contains(h.Message, "fixture-secret") {
		t.Fatal(h, e)
	}
}
