package main

import (
	"context"
	"github.com/sokel-dev/sokel-official-plugins/brevo/schema"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixture(t *testing.T, fn func(*http.Request) (int, string)) *client {
	t.Helper()
	c := newClient(&Cred{APIKey: "fixture-secret"})
	c.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		code, body := fn(r)
		return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": []string{"application/json"}, "Retry-After": []string{"17"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	return c
}
func file(t *testing.T, n string) string {
	t.Helper()
	b, e := os.ReadFile("testdata/" + n + ".official.json")
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func ptr[T any](v T) *T { return &v }
func TestCreateOfficialReceipt(t *testing.T) {
	c := fixture(t, func(r *http.Request) (int, string) {
		b, _ := io.ReadAll(r.Body)
		if r.Method != "POST" || r.URL.Host != "api.brevo.com" || r.URL.Path != "/v3/contacts" || r.Header.Get("api-key") != "fixture-secret" || !strings.Contains(string(b), `"emailBlacklisted":false`) || strings.Contains(string(b), "updateEnabled") {
			t.Fatal("invalid wire", string(b))
		}
		return 201, file(t, "create")
	})
	o, e := c.create_contact(context.Background(), &CreateContactIn{Email: "test@example.com", EmailStatus: "allowed"})
	if e != nil || o == nil || o.ContactID != "21" {
		t.Fatal(o, e)
	}
}
func TestExplicitEmailSendOfficialReceipt(t *testing.T) {
	c := fixture(t, func(r *http.Request) (int, string) {
		b, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/v3/smtp/email" || r.Method != "POST" || !strings.Contains(string(b), `"textContent":"Hello"`) || strings.Contains(string(b), "htmlContent") {
			t.Fatal(string(b))
		}
		return 201, file(t, "send")
	})
	o, e := c.send_email(context.Background(), &SendEmailIn{SenderEmail: "sender@example.com", To: []schema.Recipient{{Email: "test@example.com"}}, Subject: "Test", ContentType: "text", Body: "Hello"})
	if e != nil || o == nil || !o.Accepted || o.MessageID != "<201798300811.5787683@relay.domain.com>" {
		t.Fatal(o, e)
	}
}
func TestUpdatePreservesFalse(t *testing.T) {
	c := fixture(t, func(r *http.Request) (int, string) {
		b, _ := io.ReadAll(r.Body)
		if string(b) != `{"emailBlacklisted":false}` || r.URL.Query().Get("identifierType") != "contact_id" {
			t.Fatal(string(b), r.URL)
		}
		return 204, ""
	})
	o, e := c.update_contact(context.Background(), &UpdateContactIn{ContactID: "21", Options: schema.UpdateContactOptions{EmailBlacklisted: ptr(false)}})
	if e != nil || o == nil || !o.Accepted {
		t.Fatal(o, e)
	}
}
