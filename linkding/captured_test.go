package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestCapturedServiceResponses(t *testing.T) {
	b, e := os.ReadFile("testdata/local-service-captures.json")
	if e != nil {
		t.Fatal(e)
	}
	var rows []capturedExchange
	if e = json.Unmarshal(b, &rows); e != nil {
		t.Fatal(e)
	}
	if len(rows) < 15 {
		t.Fatal("missing captures")
	}
	for _, r := range rows {
		t.Run(r.Method+r.Path, func(t *testing.T) {
			u, _ := url.Parse(r.Path)
			q := u.Query()
			op := ""
			in := map[string]any{}
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			if r.Method == "GET" && (u.Path == "/api/tags/" || u.Path == "/api/bookmarks/" || u.Path == "/api/bookmarks/archived/") {
				options := map[string]any{}
				for _, k := range []string{"limit", "offset"} {
					if v := q.Get(k); v != "" {
						n, _ := strconv.Atoi(v)
						options[k] = n
					}
				}
				if q.Get("q") != "" {
					options["search"] = q.Get("q")
				}
				if strings.Contains(u.Path, "archived") {
					options["archived"] = true
				}
				in["options"] = options
				op = "list_bookmarks"
				if u.Path == "/api/tags/" {
					op = "list_tags"
				}
			} else if r.Method == "POST" && u.Path == "/api/bookmarks/" {
				var body map[string]any
				json.Unmarshal([]byte(r.Request), &body)
				in["url"] = body["url"]
				delete(body, "url")
				in["fields"] = body
				op = "save_bookmark"
			} else if len(parts) >= 3 {
				in["bookmark_id"] = parts[2]
				switch r.Method {
				case "GET":
					op = "get_bookmark"
				case "PATCH":
					var body map[string]any
					json.Unmarshal([]byte(r.Request), &body)
					in["changes"] = body
					op = "update_bookmark"
				case "DELETE":
					op = "delete_bookmark"
				case "POST":
					if len(parts) == 4 && parts[3] == "archive" {
						op = "archive_bookmark"
					} else if len(parts) == 4 && parts[3] == "unarchive" {
						op = "unarchive_bookmark"
					}
				}
			}
			if op == "" {
				t.Fatalf("unknown capture %s", r.Path)
			}
			body := strings.ReplaceAll(string(r.Response), "http://127.0.0.1:19091", "http://fixture.invalid")
			s := newFixtureServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Method != r.Method || req.URL.Path != u.Path {
					t.Error("capture route mismatch")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(r.Status)
				if r.Status != 204 {
					io.WriteString(w, body)
				}
			}))
			defer s.Close()
			t.Setenv("SOKEL_LINKDING_URL", s.URL)
			if _, e := dispatch(t, op, in, map[string]string{"api_token": "fixture"}); e != nil {
				t.Fatal(e)
			}
		})
	}
}
