package main

import (
	"encoding/json"
	"github.com/sokel-dev/sokel-official-plugins/gitea/schema"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestReadsAndValidation(t *testing.T) {
	repo := `{"id":2,"name":"仓库","full_name":"alice/仓库","private":false}`
	issue := `{"id":3,"number":4,"title":"Work","state":"open","body":"text"}`
	pr := `{"id":5,"number":6,"title":"Change","state":"open","merged":false}`
	c := testClient(func(r *http.Request) (*http.Response, error) {
		body := repo
		switch {
		case r.URL.Path == "/prefix/api/v1/user":
			body = `{"id":1,"login":"alice"}`
		case strings.HasSuffix(r.URL.Path, "/issues/4"):
			body = issue
		case strings.HasSuffix(r.URL.Path, "/issues"):
			if r.URL.Query().Get("type") != "issues" {
				t.Fatal("issue filter missing")
			}
			body = "[" + issue + "]"
		case strings.HasSuffix(r.URL.Path, "/pulls"):
			body = "[" + pr + "]"
		}
		res := response("application/json", body)
		res.Header.Set("X-Total-Count", "1")
		return res, nil
	})
	if o, e := c.healthCheck(ctxFor(""), &HealthCheckIn{}); e != nil || !o.OK {
		t.Fatal(o, e)
	}
	if o, e := c.getRepository(ctxFor(""), &GetRepositoryIn{Owner: "alice", Repository: "仓库"}); e != nil || o.Repository.ID != "2" {
		t.Fatal(o, e)
	}
	if o, e := c.getIssue(ctxFor(""), &GetIssueIn{Owner: "alice", Repository: "repo", Number: "4"}); e != nil || o.Issue.ID != "3" {
		t.Fatal(o, e)
	}
	if o, e := c.listIssues(ctxFor(""), &ListIssuesIn{Owner: "alice", Repository: "repo"}); e != nil || len(o.Issues) != 1 {
		t.Fatal(o, e)
	}
	if o, e := c.listPullRequests(ctxFor(""), &ListPullRequestsIn{Owner: "alice", Repository: "repo"}); e != nil || len(o.PullRequests) != 1 {
		t.Fatal(o, e)
	}
	bad := testClient(func(*http.Request) (*http.Response, error) { t.Fatal("invalid input reached server"); return nil, nil })
	for _, name := range []string{"..", "a/b", "a?token=x", "bad name"} {
		if _, e := bad.getRepository(ctxFor(""), &GetRepositoryIn{Owner: "alice", Repository: name}); e == nil {
			t.Fatal(name)
		}
	}
	zero := 0
	if _, e := bad.listRepositories(ctxFor(""), &ListRepositoriesIn{Pagination: schema.PageOptions{Limit: &zero}}); e == nil {
		t.Fatal("zero limit accepted")
	}
	if _, e := bad.listIssues(ctxFor(""), &ListIssuesIn{Owner: "a", Repository: "b", State: "bad"}); e == nil {
		t.Fatal("bad state")
	}
	if _, e := bad.createIssue(ctxFor(""), &CreateIssueIn{Owner: "a", Repository: "b"}); e == nil {
		t.Fatal("empty title")
	}
	if _, e := bad.addComment(ctxFor(""), &AddCommentIn{Owner: "a", Repository: "b", Number: "1"}); e == nil {
		t.Fatal("empty comment")
	}
	if _, e := bad.createPullRequest(ctxFor(""), &CreatePullRequestIn{Owner: "a", Repository: "b", Title: "PR"}); e == nil {
		t.Fatal("empty branches")
	}
}
func TestWritesAndGeneratedDispatch(t *testing.T) {
	t.Setenv("SOKEL_GITEA_URL", "http://service.internal:3000/prefix")
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	calls := 0
	http.DefaultTransport = roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		b, _ := io.ReadAll(r.Body)
		if r.GetBody != nil || r.Header.Get("Authorization") != "token secret" {
			t.Fatal("credential/replay")
		}
		var body string
		switch {
		case strings.HasSuffix(r.URL.Path, "/comments"):
			if !strings.Contains(string(b), `"body":"Comment"`) {
				t.Fatal(string(b))
			}
			body = `{"id":9007199254740993,"body":"Comment"}`
		case strings.HasSuffix(r.URL.Path, "/pulls"):
			if !strings.Contains(string(b), `"head":"alice:feature/topic"`) {
				t.Fatal(string(b))
			}
			body = `{"id":1,"number":2,"title":"PR","state":"open"}`
		default:
			for _, part := range []string{`"closed":false`, `"labels":[9007199254740993]`, `"assignees":["bob"]`, `"body":"Description"`} {
				if !strings.Contains(string(b), part) {
					t.Fatalf("wire missing %s: %s", part, b)
				}
			}
			body = `{"id":1,"number":1,"title":"Issue","state":"open"}`
		}
		s := response("application/json", body)
		s.StatusCode = 201
		return s, nil
	})
	for _, tc := range []struct{ op, in string }{{"create_issue", `{"owner":"alice","repository":"repo","title":"Issue","options":{"closed":false,"body":"Description","label_ids":["9007199254740993"],"assignees":["bob"]}}`}, {"add_comment", `{"owner":"alice","repository":"repo","number":"1","body":"Comment"}`}, {"create_pull_request", `{"owner":"alice","repository":"repo","title":"PR","head":"alice:feature/topic","base":"main"}`}} {
		vars, e := dispatch(t, tc.op, tc.in, ctxFor(""))
		if e != nil {
			t.Fatal(e)
		}
		if len(vars) == 0 {
			t.Fatal("empty generated result")
		}
		b, _ := json.Marshal(vars)
		if strings.Contains(string(b), "secret") {
			t.Fatal("credential leaked")
		}
		if tc.op == "add_comment" && !strings.Contains(string(b), "9007199254740993") {
			t.Fatalf("generated output lost precise ID: %s", b)
		}
	}
	if calls != 3 {
		t.Fatal(calls)
	}
}
func TestMalformedShapesAndHealthFailures(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `[{"id":0}]`, `[{"id":1,"name":"repo"}]`} {
		c := testClient(func(*http.Request) (*http.Response, error) {
			s := response("application/json", raw)
			s.Header.Set("X-Total-Count", "1")
			return s, nil
		})
		if _, e := c.listRepositories(ctxFor(""), &ListRepositoriesIn{}); e == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	c := testClient(func(*http.Request) (*http.Response, error) {
		return response("application/json", `{"message":"secret"}`), nil
	})
	if o, e := c.healthCheck(ctxFor(""), &HealthCheckIn{}); e != nil || o.OK {
		t.Fatal(o, e)
	}
	c = testClient(func(*http.Request) (*http.Response, error) {
		s := response("application/json", `{"secret":"token"}`)
		s.StatusCode = 401
		return s, nil
	})
	if o, e := c.healthCheck(ctxFor(""), &HealthCheckIn{}); e != nil || o.OK || strings.Contains(o.Message, "secret") {
		t.Fatal(o, e)
	}
	for _, headers := range []http.Header{{}, {"X-Total-Count": {"-1"}}, {"X-Total-Count": {"100"}}, {"Link": {"broken"}}, {"Link": {`<https://evil/secret>; rel="next"`}}} {
		c := testClient(func(*http.Request) (*http.Response, error) {
			s := response("application/json", `[{"id":1,"name":"repo","full_name":"a/repo"}]`)
			for k, v := range headers {
				s.Header[k] = v
			}
			return s, nil
		})
		if _, e := c.listRepositories(ctxFor(""), &ListRepositoriesIn{}); e == nil {
			t.Fatal("accepted malformed paging")
		}
	}
}
