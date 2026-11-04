package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-official-plugins/miniflux/schema"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestCapturedServiceResponses(t *testing.T) {
	b, e := os.ReadFile("testdata/local-service-captures.json")
	if e != nil {
		t.Fatal(e)
	}
	var captures []struct {
		Method   string          `json:"method"`
		Path     string          `json:"path"`
		Status   int             `json:"status"`
		Response json.RawMessage `json:"response"`
	}
	if e = json.Unmarshal(b, &captures); e != nil {
		t.Fatal(e)
	}
	count := 0
	for _, capture := range captures {
		if capture.Method != "GET" {
			continue
		}
		t.Run(capture.Path, func(t *testing.T) {
			c := newClient(Cred{APIKey: "fixture"})
			c.base = "http://local.test"
			c.http = &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
				return fixture(capture.Status, string(capture.Response)), nil
			})}
			var out any
			var e error
			switch {
			case strings.HasPrefix(capture.Path, "/v1/entries?"):
				out, e = c.listEntries(context.Background(), &ListEntriesIn{Options: schema.EntryOptions{}})
			case capture.Path == "/v1/me":
				out, e = c.getCurrentUser(context.Background(), &GetCurrentUserIn{})
			case capture.Path == "/v1/categories":
				out, e = c.listCategories(context.Background(), &ListCategoriesIn{})
			case capture.Path == "/v1/feeds":
				out, e = c.listFeeds(context.Background(), &ListFeedsIn{})
			case strings.HasPrefix(capture.Path, "/v1/entries/"):
				out, e = c.getEntry(context.Background(), &GetEntryIn{EntryID: strings.TrimPrefix(capture.Path, "/v1/entries/")})
			default:
				return
			}
			count++
			if e != nil {
				t.Fatal(e)
			}
			b, _ := json.Marshal(out)
			for _, secret := range []string{`"password"`, `"cookie"`, `"api_key"`, `"openid_connect_id"`} {
				if strings.Contains(string(b), secret) {
					t.Fatalf("private field exposed: %s", secret)
				}
			}
		})
	}
	if count < 6 {
		t.Fatalf("insufficient captured responses: %d", count)
	}
}
