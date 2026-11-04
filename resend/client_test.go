package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/sokel-dev/sokel-official-plugins/resend/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
)

const emailID = "4ef9a417-02e9-4d39-ad75-9611e0fcc33c"
const domainID = "d91cd9bd-1176-453e-8fc1-35364d380206"

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func ptr[T any](v T) *T                                               { return &v }
func fixture(t *testing.T, name string) string {
	t.Helper()
	b, e := os.ReadFile("testdata/" + name + ".json")
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func fake(t *testing.T, body string, inspect func(*http.Request)) *client {
	t.Helper()
	c := newClient(Cred{APIKey: "re_fixture_secret"})
	c.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "api.resend.com" || r.Header.Get("Authorization") != "Bearer re_fixture_secret" {
			t.Fatal("Wrong origin or authorization")
		}
		if inspect != nil {
			inspect(r)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	return c
}
func TestSendRequestPreservesEmptyTextAndIdempotency(t *testing.T) {
	c := fake(t, fixture(t, "send"), func(r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/emails" || r.Header.Get("Idempotency-Key") != "event/123" {
			t.Fatalf("wrong send request: %s %s", r.Method, r.URL.Path)
		}
		var v map[string]any
		if e := json.NewDecoder(r.Body).Decode(&v); e != nil {
			t.Fatal(e)
		}
		if v["text"] != "" || v["html"] != "<p>Hi</p>" || v["scheduled_at"] != "2099-01-01T00:00:00Z" {
			t.Fatalf("missing exact optional values: %#v", v)
		}
		if _, ok := v["idempotency_key"]; ok {
			t.Fatal("idempotency must be header")
		}
	})
	var in SendEmailIn
	raw, _ := json.Marshal(map[string]any{"email": map[string]any{"from": "Acme <onboarding@resend.dev>", "to": []any{"delivered@resend.dev"}, "subject": "Hi", "html": "<p>Hi</p>", "text": "", "scheduled_at": "2099-01-01T00:00:00Z"}, "idempotency_key": "event/123"})
	if e := contract.BindInput(raw, &in); e != nil {
		t.Fatal(e)
	}
	out, e := c.sendEmail(context.Background(), &in)
	if e != nil || out.EmailID != "49a3999c-0ce1-4ea6-ab68-afcd6dc2e794" {
		t.Fatalf("%+v %v", out, e)
	}
}
func TestGetOperationsUseOfficialExamples(t *testing.T) {
	ctx := context.Background()
	e, cancelErr := fake(t, fixture(t, "cancel"), func(r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/emails/"+emailID+"/cancel" {
			t.Fatal("wrong cancellation")
		}
	}).cancelEmail(ctx, &CancelEmailIn{EmailID: emailID})
	// Official cancel example uses a different ID; a mismatched response must fail.
	if cancelErr == nil && e.EmailID != emailID {
		t.Fatal("accepted different email")
	}
	cancelFixture := strings.ReplaceAll(fixture(t, "cancel"), "49a3999c-0ce1-4ea6-ab68-afcd6dc2e794", emailID)
	var cancelBody map[string]any
	json.Unmarshal([]byte(cancelFixture), &cancelBody)
	cancelBody["id"] = emailID
	b, _ := json.Marshal(cancelBody)
	if _, err := fake(t, string(b), nil).cancelEmail(ctx, &CancelEmailIn{EmailID: emailID}); err != nil {
		t.Fatal(err)
	}
	sent, err := fake(t, fixture(t, "get"), nil).getEmail(ctx, &GetEmailIn{EmailID: emailID})
	if err != nil || sent.Email.Text != nil || sent.Email.HTML == nil || sent.Email.LastEvent == nil || *sent.Email.LastEvent != "delivered" {
		t.Fatalf("%+v %v", sent, err)
	}
	received, err := fake(t, fixture(t, "get-received"), func(r *http.Request) {
		if r.URL.Path != "/emails/receiving/"+emailID || r.URL.Query().Get("html_format") != "cid" {
			t.Fatal("wrong receiving request")
		}
	}).getReceivedEmail(ctx, &GetReceivedEmailIn{EmailID: emailID})
	if err != nil || len(received.Email.Attachments) != 2 || received.Email.Raw == nil || received.Email.Authentication.SPF != "pass" {
		t.Fatalf("%+v %v", received, err)
	}
	domain, err := fake(t, fixture(t, "domain"), nil).getDomain(ctx, &GetDomainIn{DomainID: domainID})
	if err != nil || len(domain.Records) != 4 || domain.Domain.ClickTracking == nil || *domain.Domain.ClickTracking {
		t.Fatalf("%+v %v", domain, err)
	}
}
func TestPagination(t *testing.T) {
	ctx := context.Background()
	body := strings.Replace(fixture(t, "list"), `"has_more": false`, `"has_more": true`, 1)
	c := fake(t, body, func(r *http.Request) {
		if r.URL.Query().Get("limit") != "2" || r.URL.Query().Get("after") != emailID {
			t.Fatal("wrong paging")
		}
	})
	out, e := c.listEmails(ctx, &ListEmailsIn{Page: schema.PageOptions{Limit: ptr(2), After: ptr(emailID)}})
	if e != nil || !out.HasMore || out.NextAfter != "3a9f8c2b-1e5d-4f8a-9c7b-2d6e5f8a9c7b" {
		t.Fatalf("%+v %v", out, e)
	}
	r, e := fake(t, fixture(t, "list-received"), func(r *http.Request) {
		if r.URL.Path != "/emails/receiving" {
			t.Fatal("wrong path")
		}
	}).listReceivedEmails(ctx, &ListReceivedEmailsIn{})
	if e != nil || !r.HasMore || r.NextAfter != "a39999a6-88e3-48b1-888b-beaabcde1b33" {
		t.Fatalf("%+v %v", r, e)
	}
	d, e := fake(t, fixture(t, "domains"), func(r *http.Request) {
		if r.URL.Query().Get("limit") != "20" {
			t.Fatal("domains must use explicit bound")
		}
	}).listDomains(ctx, &ListDomainsIn{})
	if e != nil || len(d.Domains) != 1 {
		t.Fatalf("%+v %v", d, e)
	}
	for _, body := range []string{`{}`, `{"data":[],"has_more":true}`, `{"data":[],"has_more":null}`, `{"data":[{"id":"invalid"}],"has_more":false}`} {
		if _, e := fake(t, body, nil).listEmails(ctx, &ListEmailsIn{}); e == nil {
			t.Fatalf("accepted malformed page %s", body)
		}
	}
}
func TestValidationBeforeNetwork(t *testing.T) {
	c := fake(t, "{}", func(*http.Request) { t.Fatal("invalid request reached transport") })
	ctx := context.Background()
	for _, id := range []string{"../domains", "https://other.invalid", "", emailID + "?x=1"} {
		if _, e := c.getEmail(ctx, &GetEmailIn{EmailID: id}); e == nil {
			t.Fatal("accepted invalid ID")
		}
	}
	for _, p := range []schema.PageOptions{{Limit: ptr(0)}, {Limit: ptr(101)}, {After: ptr("https://other.invalid")}, {After: ptr("")}} {
		if _, e := c.listEmails(ctx, &ListEmailsIn{Page: p}); e == nil {
			t.Fatal("accepted invalid page")
		}
	}
	valid := schema.SendOptions{From: "a@example.com", To: []string{"b@example.com"}, Subject: "Hi", Text: ptr("hello")}
	for _, change := range []func(*SendEmailIn){func(x *SendEmailIn) { x.Email.To = nil }, func(x *SendEmailIn) { x.Email.From = "a@example.com\r\nX: y" }, func(x *SendEmailIn) { x.Email.Text = nil }, func(x *SendEmailIn) { x.Email.Subject = "Hi\nBcc: a@example.com" }, func(x *SendEmailIn) { x.IdempotencyKey = "key\r\n" }, func(x *SendEmailIn) { x.Email.ScheduledAt = ptr("in 1 hour") }, func(x *SendEmailIn) { x.Email.Tags = []schema.Tag{{Name: "invalid tag", Value: "ok"}} }} {
		in := SendEmailIn{Email: valid}
		change(&in)
		if _, e := c.sendEmail(ctx, &in); e == nil {
			t.Fatal("accepted invalid email")
		}
	}
}
func TestTransportFailuresAreBoundedAndRedacted(t *testing.T) {
	ctx := context.Background()
	for _, status := range []int{301, 400, 401, 403, 404, 409, 422, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			c := newClient(Cred{APIKey: "re_fixture_secret"})
			c.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}, "Location": {"https://leak.invalid/"}, "Retry-After": {"7"}}, Body: io.NopCloser(strings.NewReader(`{"message":"re_fixture_secret private content"}`)), Request: r}, nil
			})
			_, e := c.getEmail(ctx, &GetEmailIn{EmailID: emailID})
			if e == nil || strings.Contains(e.Error(), "re_fixture_secret") || strings.Contains(e.Error(), "private content") || calls != 1 {
				t.Fatalf("unsafe error or retry: %v %d", e, calls)
			}
			if status == 429 && !strings.Contains(e.Error(), "7") {
				t.Fatal("missing Retry-After")
			}
		})
	}
	for _, body := range []string{"null", "{} {}", `{"id":"wrong"}`, strings.Repeat("x", (8<<20)+1)} {
		if _, e := fake(t, body, nil).getEmail(ctx, &GetEmailIn{EmailID: emailID}); e == nil {
			t.Fatal("accepted invalid response")
		}
	}
	c := fake(t, "{}", nil)
	c.http.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		return nil, &url.Error{Op: "GET", URL: "https://secret.invalid/re_fixture_secret", Err: errors.New("re_fixture_secret")}
	})
	if _, e := c.getEmail(ctx, &GetEmailIn{EmailID: emailID}); e == nil || strings.Contains(e.Error(), "re_fixture_secret") {
		t.Fatalf("transport leaked: %v", e)
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, e := c.getEmail(ctx, &GetEmailIn{EmailID: emailID}); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
