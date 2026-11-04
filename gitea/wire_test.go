package main

import (
	"github.com/sokel-dev/sokel-official-plugins/gitea/schema"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestREDListRepositoriesPagination(t *testing.T) {
	c := testClient(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/prefix/api/v1/user/repos" || r.URL.Query().Get("page") != "1" || r.Header.Get("Authorization") != "token secret" {
			t.Fatalf("incorrect request")
		}
		s := response("application/json", `[{"id":9007199254740993,"name":"repo","full_name":"alice/repo"}]`)
		s.Header.Set("Link", `<http://service.internal:3000/prefix/api/v1/user/repos?limit=50&page=2>; rel="next"`)
		s.Header.Set("X-Total-Count", "2")
		return s, nil
	})
	out, e := c.listRepositories(ctxFor(""), &ListRepositoriesIn{})
	if e != nil {
		t.Fatal(e)
	}
	if out.NextPage != 2 || out.Repositories[0].ID != "9007199254740993" {
		t.Fatalf("pagination or ID lost: %+v", out)
	}
}
func TestREDCreateIssueFalseOnWire(t *testing.T) {
	f := false
	c := testClient(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		if r.GetBody != nil || !strings.Contains(string(b), `"closed":false`) {
			t.Fatalf("false omitted or replay enabled: %s", b)
		}
		s := response("application/json", `{"id":1,"number":1,"title":"Test","state":"open"}`)
		s.StatusCode = 201
		return s, nil
	})
	out, e := c.createIssue(ctxFor(""), &CreateIssueIn{Owner: "alice", Repository: "repo", Title: "Test", Options: schema.IssueOptions{Closed: &f}})
	if e != nil {
		t.Fatal(e)
	}
	if out.Issue.Number != "1" {
		t.Fatal(out)
	}
}
