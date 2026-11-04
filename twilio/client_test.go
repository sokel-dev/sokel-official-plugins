package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-official-plugins/twilio/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

const accountSID = "ACaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const messageSID = "SMaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const numberSID = "PNaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

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
	c := newClient(Cred{AccountSid: accountSID, AuthToken: "fixture_secret"})
	c.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		u, p, ok := r.BasicAuth()
		if !ok || u != accountSID || p != "fixture_secret" || r.URL.Host != "api.twilio.com" || r.URL.Scheme != "https" {
			t.Fatal("bad auth/origin")
		}
		if inspect != nil {
			inspect(r)
		}
		status := 200
		if r.Method == "POST" {
			status = 201
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	return c
}
func TestSendFormAndBinding(t *testing.T) {
	c := fake(t, fixture(t, "send"), func(r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/2010-04-01/Accounts/"+accountSID+"/Messages.json" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Fatal("wrong send endpoint")
		}
		if e := r.ParseForm(); e != nil {
			t.Fatal(e)
		}
		if r.PostForm.Get("SmartEncoded") != "false" || r.PostForm.Get("ValidityPeriod") != "1" || len(r.PostForm["MediaUrl"]) != 2 || r.PostForm.Get("Body") != "Hello! 👍" || r.PostForm.Get("From") != "+15017122661" {
			t.Fatalf("incorrect form %v", r.PostForm)
		}
	})
	var in SendMessageIn
	raw := []byte(`{"message":{"to":"+15558675310","from":"+15017122661","body":"Hello! 👍","smart_encoded":false,"validity_period":1,"media_urls":["https://example.com/1.png","https://example.com/2.png"]}}`)
	if e := contract.BindInput(raw, &in); e != nil {
		t.Fatal(e)
	}
	out, e := c.sendMessage(context.Background(), &in)
	if e != nil || out.Message.SID == "" {
		t.Fatalf("%+v %v", out, e)
	}
}
func TestReadOperationsAndSecretExclusion(t *testing.T) {
	ctx := context.Background()
	a, e := fake(t, fixture(t, "account"), nil).getAccount(ctx, &GetAccountIn{})
	if e != nil || a.Account.SID != accountSID {
		t.Fatalf("%+v %v", a, e)
	}
	b, _ := json.Marshal(a)
	if strings.Contains(string(b), "auth_token") {
		t.Fatal("account output exposes auth token")
	}
	m, e := fake(t, fixture(t, "message"), nil).getMessage(ctx, &GetMessageIn{MessageSid: "SMb7c0a2ce80504485a6f653a7110836f5"})
	if e != nil || m.Message.SID != "SMb7c0a2ce80504485a6f653a7110836f5" || m.Message.Price == nil {
		t.Fatalf("%+v %v", m, e)
	}
	n, e := fake(t, fixture(t, "number"), nil).getIncomingNumber(ctx, &GetIncomingNumberIn{NumberSid: numberSID})
	if e != nil || n.Number.SID != numberSID {
		t.Fatalf("%+v %v", n, e)
	}
	ns, e := fake(t, fixture(t, "numbers"), nil).listIncomingNumbers(ctx, &ListIncomingNumbersIn{})
	if e != nil || len(ns.Numbers) != 1 || ns.Numbers[0].Capabilities.SMS || ns.HasMore {
		t.Fatalf("%+v %v", ns, e)
	}
	ms, e := fake(t, fixture(t, "media"), nil).listMessageMedia(ctx, &ListMessageMediaIn{MessageSid: messageSID})
	if e != nil || len(ms.Media) != 1 || ms.HasMore {
		t.Fatalf("%+v %v", ms, e)
	}
}
func TestPaginationAndExactFilters(t *testing.T) {
	ctx := context.Background()
	c := fake(t, fixture(t, "messages"), func(r *http.Request) {
		q := r.URL.Query()
		if q.Get("PageSize") != "2" || q.Get("DateSent>") != "2008-01-02" || q.Get("To") != "+123456789" {
			t.Fatalf("bad filters %v", q)
		}
	})
	out, e := c.listMessages(ctx, &ListMessagesIn{Page: schema.PageOptions{Size: ptr(2)}, Filters: schema.MessageFilters{To: ptr("+123456789"), From: ptr("+987654321"), SentAfter: ptr("2008-01-02")}})
	if e != nil || len(out.Messages) != 2 || !out.HasMore || !strings.HasPrefix(out.Messages[1].SID, "MM") {
		t.Fatalf("%+v %v", out, e)
	}
	var page map[string]any
	json.Unmarshal([]byte(fixture(t, "messages")), &page)
	page["next_page_uri"] = nil
	b, _ := json.Marshal(page)
	c = fake(t, string(b), func(r *http.Request) {
		q := r.URL.Query()
		if q.Get("Page") != "1" || q.Get("PageToken") != "PAMMc26223853f8c46b4ab7dfaa6abba0a26" || q.Get("DateSent>") != "2008-01-02" {
			t.Fatal("lost opaque cursor or filters")
		}
	})
	next, e := c.listMessages(ctx, &ListMessagesIn{Page: schema.PageOptions{NextPageURI: &out.NextPageURI}})
	if e != nil || next.HasMore || next.NextPageURI != "" {
		t.Fatalf("%+v %v", next, e)
	}
}
func TestHostilePaginationAndValidation(t *testing.T) {
	c := fake(t, "{}", func(*http.Request) { t.Fatal("invalid request reached network") })
	ctx := context.Background()
	path := "/2010-04-01/Accounts/" + accountSID + "/Messages.json"
	for _, uri := range []string{"https://evil.invalid" + path + "?PageToken=x", "//evil.invalid" + path, strings.Replace(path, accountSID, "ACbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", 1) + "?PageToken=x", path + "?unknown=1", path + "?PageSize=1001&PageToken=x", path + "?PageSize=1&PageSize=2&PageToken=x", path + "?PageToken=x#fragment"} {
		if _, e := c.listMessages(ctx, &ListMessagesIn{Page: schema.PageOptions{NextPageURI: &uri}}); e == nil {
			t.Fatalf("accepted %s", uri)
		}
	}
	for _, id := range []string{"../Accounts", messageSID + "?x=1", ""} {
		if _, e := c.getMessage(ctx, &GetMessageIn{MessageSid: id}); e == nil {
			t.Fatal("invalid SID")
		}
	}
	valid := schema.SendOptions{To: "+15558675310", From: ptr("+15017122661"), Body: ptr("Hello")}
	for _, change := range []func(*schema.SendOptions){func(x *schema.SendOptions) { x.To = "invalid" }, func(x *schema.SendOptions) { x.From = nil }, func(x *schema.SendOptions) { x.Body = nil }, func(x *schema.SendOptions) { x.ValidityPeriod = ptr(0) }, func(x *schema.SendOptions) { x.MediaURLs = []string{"file:///secret"} }, func(x *schema.SendOptions) { x.MessagingServiceSID = ptr("../keys") }, func(x *schema.SendOptions) { x.Body = ptr(strings.Repeat("你", 1601)) }} {
		v := valid
		change(&v)
		if _, e := c.sendMessage(ctx, &SendMessageIn{Message: v}); e == nil {
			t.Fatal("invalid send accepted")
		}
	}
}
func TestMalformedResponsesAndNoRetry(t *testing.T) {
	ctx := context.Background()
	for _, body := range []string{"{}", "null", `{"messages":null,"next_page_uri":null}`, `{"messages":[],"next_page_uri":"//evil.invalid/a"}`, `{"messages":[]}`, `{} {}`} {
		if _, e := fake(t, body, nil).listMessages(ctx, &ListMessagesIn{}); e == nil {
			t.Fatalf("accepted %s", body)
		}
	}
	for _, status := range []int{302, 400, 401, 403, 404, 429, 500} {
		calls := 0
		c := newClient(Cred{AccountSid: accountSID, AuthToken: "fixture_secret"})
		c.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: status, Header: http.Header{"Location": {"https://evil.invalid"}, "Retry-After": {"5"}}, Body: io.NopCloser(strings.NewReader(`{"message":"fixture_secret"}`)), Request: r}, nil
		})
		_, e := c.sendMessage(ctx, &SendMessageIn{Message: schema.SendOptions{To: "+15558675310", From: ptr("+15017122661"), Body: ptr("Hi")}})
		if e == nil || strings.Contains(e.Error(), "fixture_secret") || calls != 1 {
			t.Fatalf("error=%v calls=%d", e, calls)
		}
	}
}
