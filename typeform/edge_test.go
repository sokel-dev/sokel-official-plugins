package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-official-plugins/typeform/schema"
	"net/url"
	"strings"
	"testing"
)

func TestPaginationAndAnswerTypes(t *testing.T) {
	for _, raw := range []string{`{"type":"boolean","boolean":false}`, `{"type":"number","number":0}`, `{"type":"text","text":""}`, `{"type":"choice","choice":{"label":"One","ref":"one"}}`, `{"type":"choices","choices":{"labels":["One","Two"]}}`, `{"type":"payment","payment":{"amount":"10","success":true}}`, `{"type":"file_url","file_url":"https://files.example/x"}`} {
		a := strings.TrimSuffix(raw, "}") + `,"field":{"id":"field1","ref":"ref1","type":"yes_no"}}`
		b := `{"token":"cursor1","response_id":"different-id","answers":[` + a + `],"calculated":{"score":0}}`
		out, e := response([]byte(b))
		if e != nil || len(out.Answers) != 1 || out.Answers[0].Value == nil {
			t.Fatal(out, e)
		}
		if out.Token == *out.ResponseID {
			t.Fatal("token confused with response ID")
		}
	}
	for _, b := range []string{`{}`, `{"token":"https://evil.example"}`, `{"token":"ok","answers":[{"type":"boolean","field":{"id":"field1","type":"yes_no"}}]}`, `{"token":"ok","answers":[{"type":"boolean","boolean":false,"field":{}}]}`} {
		if _, e := response([]byte(b)); e == nil {
			t.Fatal("incomplete response accepted")
		}
	}
	r, e := response([]byte(`{"token":"valid","answers":null}`))
	if e != nil || r.Answers == nil {
		t.Fatal(r, e)
	}
	c := fresh()
	ctx := context.Background()
	fixture(t, c, "GET", "/forms/form123/responses", url.Values{"page_size": {"100"}, "response_type": {"completed"}, "since": {"2026-11-01T01:00:00"}, "until": {"2026-11-02T01:00:00"}, "query": {"hello"}}, "", 200, `{"items":[],"page_count":0,"total_items":0}`)
	o, e := c.list_responses(ctx, &ListResponsesIn{FormID: "form123", Options: schema.ResponseOptions{Since: ptr("2026-11-01T09:00:00+08:00"), Until: ptr("2026-11-02T01:00:00Z"), Query: ptr("hello")}})
	if e != nil || o.MayHaveMore || o.NextBefore != "" {
		t.Fatal(o, e)
	}
	for _, b := range []string{`{}`, `{"items":null,"page_count":0,"total_items":0}`, `{"items":[],"page_count":-1,"total_items":0}`, `{"items":[{"token":"ok"}],"page_count":1,"total_items":0}`, `{"items":[],"page_count":2,"total_items":2}`, `{"items":[{"token":"same"}],"page_count":2,"total_items":2}`} {
		c = fresh()
		withReply(c, 200, b)
		if _, e := c.list_responses(ctx, &ListResponsesIn{FormID: "form123", Options: schema.ResponseOptions{Before: ptr("same")}}); e == nil {
			t.Fatal("bad response pagination accepted", b)
		}
	}
	c = fresh()
	withReply(c, 200, `{"items":[{"token":"a"},{"token":"a"}],"page_count":1,"total_items":2}`)
	if _, e := c.list_responses(ctx, &ListResponsesIn{FormID: "form123"}); e == nil {
		t.Fatal("duplicate token")
	}
	c = fresh()
	withReply(c, 200, `{"items":[{"token":"ok","answers":[]}],"page_count":1,"total_items":1}`)
	o, e = c.list_responses(ctx, &ListResponsesIn{FormID: "form123"})
	if e != nil || o.MayHaveMore || o.NextBefore != "" {
		t.Fatal(o, e)
	}
}
func TestTypeformInputValidation(t *testing.T) {
	for _, s := range []string{"", "https://x", "../", "id?x", strings.Repeat("x", 129)} {
		if checkID(s) == nil {
			t.Fatal("unsafe ID")
		}
	}
	for _, p := range []schema.PageOptions{{Page: ptr(0)}, {Page: ptr(1000001)}, {PageSize: ptr(0)}, {PageSize: ptr(201)}} {
		if _, _, _, e := pageValues(p); e == nil {
			t.Fatal("bad page")
		}
	}
	for _, s := range []string{"bad", "2026-11-01T00:00:00.1Z"} {
		if _, e := submittedTime(s); e == nil {
			t.Fatal("bad timestamp")
		}
	}
	q := url.Values{}
	if addSearch(q, "\x00") == nil || addSearch(q, strings.Repeat("x", 1025)) == nil || addSearch(q, string([]byte{255})) == nil {
		t.Fatal("bad search")
	}
	for _, options := range []schema.ResponseOptions{{PageSize: ptr(0)}, {PageSize: ptr(1001)}, {Before: ptr("")}, {Before: ptr("url/?")}, {Since: ptr("bad")}, {Until: ptr("bad")}, {Since: ptr("2026-11-02T00:00:00Z"), Until: ptr("2026-11-01T00:00:00Z")}, {Query: ptr("\x00")}} {
		c := fresh()
		if _, e := c.list_responses(context.Background(), &ListResponsesIn{FormID: "form123", Options: options}); e == nil {
			t.Fatal("bad options")
		}
	}
	c := fresh()
	c.cred.AccessToken = "bad\r"
	if _, e := c.authorization(); e == nil {
		t.Fatal("bad credential")
	}
	if _, e := workspace([]byte(`{"id":"x"}`)); e == nil {
		t.Fatal("missing name")
	}
	if _, e := workspace([]byte(`bad`)); e == nil {
		t.Fatal("bad metadata")
	}
}
func TestTypeformObjectErrorsAndEmptyForms(t *testing.T) {
	ctx := context.Background()
	for _, body := range []string{`{}`, `{"code":"ERROR","description":"token-test"}`, `null`, `{"id":"different","title":"Other"}`} {
		c := fresh()
		withReply(c, 200, body)
		if _, e := c.get_form(ctx, &GetFormIn{FormID: "form123"}); e == nil {
			t.Fatal("bad form")
		}
		if _, e := c.get_workspace(ctx, &GetWorkspaceIn{WorkspaceID: "abc123"}); e == nil {
			t.Fatal("bad workspace")
		}
	}
	c := fresh()
	withReply(c, 200, `{"id":"form123","title":"Draft"}`)
	o, e := c.get_form(ctx, &GetFormIn{FormID: "form123"})
	if e != nil || o.Form.Questions == nil || len(o.Form.Questions) != 0 {
		t.Fatal(o, e)
	}
	withReply(c, 200, `{"id":"form123","title":"Draft","fields":[{"title":"bad"}]}`)
	if _, e = c.get_form(ctx, &GetFormIn{FormID: "form123"}); e == nil {
		t.Fatal("bad question")
	}
	for _, form := range []string{`{"title":"bad"}`, `{"id":"form1"}`} {
		withReply(c, 200, `{"items":[`+form+`],"page_count":1,"total_items":1}`)
		if _, e := c.list_forms(ctx, &ListFormsIn{}); e == nil {
			t.Fatal("bad form summary")
		}
	}
	fixture(t, c, "GET", "/workspaces", url.Values{"page": {"1"}, "page_size": {"100"}, "search": {"Sales"}}, "", 200, `{"items":[`+workspaceJSON+`],"page_count":2,"total_items":101}`)
	w, e := c.list_workspaces(ctx, &ListWorkspacesIn{Search: "Sales"})
	if e != nil || w.NextPage != 2 {
		t.Fatal(w, e)
	}
	c = fresh()
	withReply(c, 200, `{"items":[],"page_count":0,"total_items":0}`)
	if _, e := c.list_forms(ctx, &ListFormsIn{WorkspaceID: "https://x"}); e == nil {
		t.Fatal("unsafe workspace path")
	}
	var payload any
	if e := decodeJSON([]byte(`{"zero":0}`), &payload); e != nil {
		t.Fatal(e)
	}
	if _, ok := payload.(map[string]any)["zero"].(json.Number); !ok {
		t.Fatal("numeric precision lost")
	}
}
