package main

import (
	"encoding/json"
	"github.com/sokel-dev/sokel-official-plugins/gitea/schema"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
)

// Captured from an isolated Gitea 28.3.0 instance with synthetic accounts and data.
func TestCapturedGiteaResponses(t *testing.T) {
	b, e := os.ReadFile("testdata/local-gitea-28.3.0.json")
	if e != nil {
		t.Fatal(e)
	}
	var rows []struct {
		Path    string            `json:"path"`
		Status  int               `json:"status"`
		Headers map[string]string `json:"response_headers"`
		Body    json.RawMessage   `json:"body"`
	}
	if e = json.Unmarshal(b, &rows); e != nil {
		t.Fatal(e)
	}
	for _, row := range rows {
		t.Run(row.Path, func(t *testing.T) {
			body := row.Body
			var bodyText string
			if json.Unmarshal(body, &bodyText) == nil {
				body = []byte(bodyText)
			}
			u, e := url.Parse(row.Path)
			if e != nil {
				t.Fatal(e)
			}
			c := newClient("http://127.0.0.1:13007", Cred{Token: "synthetic"})
			c.http = &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
				s := response(row.Headers["Content-Type"], string(body))
				s.StatusCode = row.Status
				for k, v := range row.Headers {
					s.Header.Set(k, v)
				}
				return s, nil
			})}
			ctx := ctxFor("")
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			switch {
			case u.Path == "/api/v1/user":
				o, e := c.healthCheck(ctx, &HealthCheckIn{})
				if e != nil || !o.OK {
					t.Fatal(o, e)
				}
			case u.Path == "/api/v1/user/repos":
				one := 1
				if _, e := c.listRepositories(ctx, &ListRepositoriesIn{Pagination: schema.PageOptions{Limit: &one}}); e != nil {
					t.Fatal(e)
				}
			case len(parts) == 5:
				if _, e := c.getRepository(ctx, &GetRepositoryIn{Owner: parts[3], Repository: parts[4]}); e != nil {
					t.Fatal(e)
				}
			case len(parts) == 6 && parts[5] == "issues":
				var issues []issueWire
				if e := decodeJSON(body, &issues); e != nil {
					t.Fatal(e)
				}
				for _, v := range issues {
					if _, e := v.output(); e != nil {
						t.Fatal(e)
					}
				}
				q := u.Query()
				page, limit := 1, 50
				if q.Get("page") != "" {
					page, _ = strconv.Atoi(q.Get("page"))
				}
				if q.Get("limit") != "" {
					limit, _ = strconv.Atoi(q.Get("limit"))
				}
				headers := http.Header{}
				for k, v := range row.Headers {
					headers.Set(k, v)
				}
				if _, e := c.pagination(headers, u.Path, q, page, limit, len(issues)); e != nil {
					t.Fatal(e)
				}
			case len(parts) == 6 && parts[5] == "pulls":
				if _, e := c.listPullRequests(ctx, &ListPullRequestsIn{Owner: parts[3], Repository: parts[4], State: u.Query().Get("state")}); e != nil {
					t.Fatal(e)
				}
			default:
				var comments []commentWire
				if e := decodeJSON(body, &comments); e != nil {
					t.Fatal(e)
				}
				for _, v := range comments {
					if _, e := idString(v.ID); e != nil {
						t.Fatal(e)
					}
				}
			}
		})
	}
}
