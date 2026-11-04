package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Synthetic examples based on official API shapes. No live account captures.
type handlerTransport struct{ h http.HandlerFunc }

func (t handlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	t.h(w, r)
	res := w.Result()
	res.Request = r
	return res, nil
}
func fakeAPI(h http.HandlerFunc) *api {
	a := newAPI("test-secret")
	a.client = &http.Client{Transport: handlerTransport{h}}
	return a
}
func reply(w http.ResponseWriter, s string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, s)
}
func body(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var m map[string]any
	if e := json.NewDecoder(r.Body).Decode(&m); e != nil {
		t.Fatal(e)
	}
	return m
}

type fileCtx struct {
	context.Context
	bytes []byte
	fail  bool
}

func (c *fileCtx) Credential() map[string]string {
	return map[string]string{"access_token": "test-secret"}
}
func (*fileCtx) Fetch(*plugin.File) ([]byte, error) { return nil, fmt.Errorf("unexpected fetch") }
func (c *fileCtx) Upload(n, m string, b []byte) (*plugin.File, error) {
	if c.fail {
		return nil, fmt.Errorf("test-secret must be hidden")
	}
	c.bytes = append([]byte{}, b...)
	return &plugin.File{ID: "stored", Name: n, Mime: m, Size: int64(len(b))}, nil
}
func (c *fileCtx) UploadReader(n, m string, r io.Reader) (*plugin.File, error) {
	b, e := io.ReadAll(r)
	if e != nil {
		return nil, e
	}
	return c.Upload(n, m, b)
}
func identity(t *testing.T, w http.ResponseWriter, r *http.Request) bool {
	t.Helper()
	if r.URL.Host != "graph.microsoft.com" || r.Header.Get("Authorization") != "Bearer test-secret" {
		t.Fatal("origin or auth")
	}
	if r.URL.Path == "/v1.0/me" {
		if r.Method != "GET" || r.URL.Query().Get("$select") != "id" {
			t.Fatal("identity must be read-only")
		}
		reply(w, `{"id":"viewer"}`)
		return true
	}
	return false
}
func TestReadOnlyHealth(t *testing.T) {
	calls := 0
	a := fakeAPI(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if !identity(t, w, r) {
			t.Fatal("unexpected identity request")
		}
	})
	o, e := a.health_check(context.Background(), &HealthCheckIn{})
	if e != nil || o == nil || !o.OK || calls != 1 {
		t.Fatalf("health %v %v calls=%d", o, e, calls)
	}
}

var _ = strings.Contains

func TestOutlookProtocol(t *testing.T) {
	posts := 0
	a := fakeAPI(func(w http.ResponseWriter, r *http.Request) {
		if identity(t, w, r) {
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/v1.0/users/viewer/") {
			t.Fatal("must bind resource to delegated identity", r.URL.Path)
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/sendMail"):
			posts++
			m := body(t, r)["message"].(map[string]any)
			if m["subject"] != "Report 中文" || m["body"].(map[string]any)["contentType"] != "HTML" || len(m["toRecipients"].([]any)) != 1 {
				t.Fatal(m)
			}
			w.WriteHeader(202)
		case r.Method == "POST":
			posts++
			m := body(t, r)
			if m["start"].(map[string]any)["dateTime"] != "2026-11-03T02:00:00" || m["start"].(map[string]any)["timeZone"] != "UTC" || m["transactionId"] != "txn-1" {
				t.Fatal(m)
			}
			w.WriteHeader(201)
			reply(w, `{"id":"event","subject":"Review","start":{"dateTime":"2026-11-03T02:00:00","timeZone":"UTC"},"end":{"dateTime":"2026-11-03T03:00:00","timeZone":"UTC"}}`)
		case strings.HasSuffix(r.URL.Path, "/$value"):
			w.Header().Set("Content-Type", "application/pdf")
			w.Write([]byte{0, 1, 2, 255})
		case strings.HasSuffix(r.URL.Path, "/attachments/a"):
			reply(w, `{"@odata.type":"#microsoft.graph.fileAttachment","id":"a","name":"报告.pdf","contentType":"application/pdf","size":4}`)
		case strings.HasSuffix(r.URL.Path, "/attachments"):
			reply(w, `{"value":[{"@odata.type":"#microsoft.graph.fileAttachment","id":"a","name":"报告.pdf","contentType":"application/pdf","size":4}]}`)
		case strings.HasSuffix(r.URL.Path, "/calendarView"):
			if r.URL.Query().Get("startDateTime") != "2026-11-03T00:00:00Z" {
				t.Fatal(r.URL.RawQuery)
			}
			reply(w, `{"value":[{"id":"event","subject":"Review"}]}`)
		case strings.HasSuffix(r.URL.Path, "/messages/m"):
			reply(w, `{"id":"m","subject":"Report","body":{"contentType":"html","content":"<p>内容</p>"},"from":{"emailAddress":{"address":"from@example.com"}}}`)
		default:
			if r.URL.Query().Get("$top") != "2" || r.URL.Query().Get("$filter") != "isRead eq false" {
				t.Fatal(r.URL.RawQuery)
			}
			reply(w, `{"value":[{"id":"m","subject":"Report"}],"@odata.nextLink":"https://graph.microsoft.com/v1.0/users/viewer/mailFolders/inbox/messages?$skiptoken=a%2Bb"}`)
		}
	})
	ctx := &fileCtx{Context: context.Background()}
	if o, e := a.list_messages(ctx, &ListMessagesIn{FolderID: "inbox", Limit: 2, Filter: "isRead eq false"}); e != nil || o == nil || len(o.Messages) != 1 || o.NextLink == "" {
		t.Fatalf("list %v %v", o, e)
	}
	if o, e := a.get_message(ctx, &GetMessageIn{MessageID: "m"}); e != nil || o == nil || o.Message.Body != "<p>内容</p>" {
		t.Fatalf("get %v %v", o, e)
	}
	if o, e := a.send_mail(ctx, &SendMailIn{To: []string{"to@example.com"}, Subject: "Report 中文", Body: "<b>text</b>", BodyType: "html"}); e != nil || o == nil || !o.Accepted {
		t.Fatalf("send %v %v", o, e)
	}
	if o, e := a.list_attachments(ctx, &ListAttachmentsIn{MessageID: "m"}); e != nil || o == nil || len(o.Attachments) != 1 {
		t.Fatalf("attachments %v %v", o, e)
	}
	if o, e := a.download_attachment(ctx, &DownloadAttachmentIn{MessageID: "m", AttachmentID: "a"}); e != nil || o == nil || o.File.Name != "报告.pdf" || len(ctx.bytes) != 4 || ctx.bytes[3] != 255 {
		t.Fatalf("file %v %v", o, e)
	}
	if o, e := a.calendar_view(ctx, &CalendarViewIn{Start: "2026-11-03T00:00:00Z", End: "2026-11-04T00:00:00Z"}); e != nil || o == nil || len(o.Events) != 1 {
		t.Fatalf("calendar %v %v", o, e)
	}
	if o, e := a.create_event(ctx, &CreateEventIn{Subject: "Review", Start: "2026-11-03T10:00:00+08:00", End: "2026-11-03T11:00:00+08:00", Attendees: []string{"user@example.com"}, TransactionID: "txn-1"}); e != nil || o == nil || o.Event.ID != "event" || posts != 2 {
		t.Fatalf("event %v %v %d", o, e, posts)
	}
}
