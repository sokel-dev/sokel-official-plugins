package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fakeAPI(t *testing.T, fn func(*http.Request) string) *api {
	t.Helper()
	a := newAPI("test-secret")
	a.client.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Fatal("missing authorization")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(fn(r))), Request: r}, nil
	})
	return a
}
func TestReadNestedTabsAndTables(t *testing.T) {
	a := fakeAPI(t, func(r *http.Request) string {
		if r.URL.String() != "https://docs.googleapis.com/v1/documents/doc?includeTabsContent=true" {
			t.Fatal(r.URL)
		}
		return `{"documentId":"doc","title":"Report","revisionId":"r1","tabs":[{"tabProperties":{"tabId":"t1","title":"Main"},"documentTab":{"body":{"content":[{"endIndex":4,"paragraph":{"elements":[{"textRun":{"content":"Hi\n"}}]}},{"endIndex":10,"table":{"tableRows":[{"tableCells":[{"content":[{"paragraph":{"elements":[{"textRun":{"content":"Cell\n"}}]}}]}]}]}}]}},"childTabs":[{"tabProperties":{"tabId":"t2","title":"Child"},"documentTab":{"body":{"content":[{"endIndex":3,"paragraph":{"elements":[{"textRun":{"content":"子\n"}}]}}]}}}]}]}`
	})
	out, e := a.get(context.Background(), &GetDocumentIn{DocumentID: "doc"})
	if e != nil {
		t.Fatal(e)
	}
	if len(out.Tabs) != 2 || out.Tabs[0].Text != "Hi\nCell\n" || out.Tabs[1].ParentID != "t1" || out.Tabs[0].EndIndex != 10 || out.RevisionID != "r1" {
		t.Fatalf("%+v", out)
	}
}
func TestWritesSelectTabAndRevision(t *testing.T) {
	var bodies []map[string]any
	a := fakeAPI(t, func(r *http.Request) string {
		if r.Method != "POST" || r.URL.Path != "/v1/documents/doc:batchUpdate" {
			t.Fatal(r.Method, r.URL)
		}
		var b map[string]any
		if e := json.NewDecoder(r.Body).Decode(&b); e != nil {
			t.Fatal(e)
		}
		bodies = append(bodies, b)
		return `{"documentId":"doc","writeControl":{"requiredRevisionId":"r2"},"replies":[{"replaceAllText":{"occurrencesChanged":2}}]}`
	})
	c := context.Background()
	out, e := a.insert(c, &InsertTextIn{DocumentID: "doc", TabID: "t1", Text: "🙂", RequiredRevisionID: "r1"})
	if e != nil || out.RevisionID != "r2" {
		t.Fatal(out, e)
	}
	_, e = a.insert(c, &InsertTextIn{DocumentID: "doc", TabID: "t1", Text: "text", Index: 3})
	if e != nil {
		t.Fatal(e)
	}
	rep, e := a.replace(c, &ReplaceTextIn{DocumentID: "doc", TabID: "t1", Find: "{{name}}", Replacement: "", RequiredRevisionID: "r1"})
	if e != nil || rep.OccurrencesChanged != 2 {
		t.Fatal(rep, e)
	}
	_, e = a.delete(c, &DeleteTextIn{DocumentID: "doc", TabID: "t1", StartIndex: 1, EndIndex: 3})
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(bodies)
	s := string(b)
	for _, want := range []string{`"endOfSegmentLocation":{"tabId":"t1"}`, `"location":{"index":3,"tabId":"t1"}`, `"requiredRevisionId":"r1"`, `"tabsCriteria":{"tabIds":["t1"]}`, `"matchCase":false`, `"replaceText":""`, `"range":{"endIndex":3,"startIndex":1,"tabId":"t1"}`} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %s in %s", want, s)
		}
	}
}
func TestInvalidWritesDoNotSend(t *testing.T) {
	a := fakeAPI(t, func(*http.Request) string { t.Fatal("unexpected request"); return "{}" })
	c := context.Background()
	checks := []func() error{func() error { _, e := a.insert(c, &InsertTextIn{DocumentID: "doc", Text: "x"}); return e }, func() error {
		_, e := a.insert(c, &InsertTextIn{DocumentID: "doc", TabID: "t", Text: "x", Index: -1})
		return e
	}, func() error { _, e := a.replace(c, &ReplaceTextIn{DocumentID: "doc", TabID: "t"}); return e }, func() error {
		_, e := a.delete(c, &DeleteTextIn{DocumentID: "doc", TabID: "t", StartIndex: 3, EndIndex: 3})
		return e
	}, func() error { _, e := a.create(c, &CreateDocumentIn{}); return e }}
	for _, fn := range checks {
		if fn() == nil {
			t.Fatal("invalid write accepted")
		}
	}
}

func TestIncompleteDocumentAndWriteReplies(t *testing.T) {
	c := context.Background()
	a := fakeAPI(t, func(*http.Request) string { return `{"documentId":"doc"}` })
	if _, e := a.get(c, &GetDocumentIn{DocumentID: "doc"}); e == nil {
		t.Fatal("missing tabs accepted")
	}
	for _, body := range []string{`{"documentId":"doc"}`, `{"documentId":"doc","replies":[]}`, `{"documentId":"doc","replies":[{},{}]}`} {
		a = fakeAPI(t, func(*http.Request) string { return body })
		if _, e := a.insert(c, &InsertTextIn{DocumentID: "doc", TabID: "t", Text: "x"}); e == nil {
			t.Fatal("incomplete batch reply accepted")
		}
	}
	a = fakeAPI(t, func(*http.Request) string { return `{"documentId":"doc","replies":[{}]}` })
	if _, e := a.replace(c, &ReplaceTextIn{DocumentID: "doc", TabID: "t", Find: "x"}); e == nil {
		t.Fatal("missing replacement result accepted as zero matches")
	}
	// Empty replies are valid for insert/delete, whose request still has one reply slot.
	if _, e := a.insert(c, &InsertTextIn{DocumentID: "doc", TabID: "t", Text: "x"}); e != nil {
		t.Fatal(e)
	}
}
