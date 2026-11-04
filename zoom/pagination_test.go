package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestRejectInvalidContinuationTokens(t *testing.T) {
	for _, resource := range []string{"users", "meetings"} {
		for _, tc := range []struct{ name, token string }{
			{"repeated", "prior-token"},
			{"oversized", strings.Repeat("x", 4097)},
			{"carriage-return", "next\rtoken"},
			{"line-feed", "next\ntoken"},
		} {
			t.Run(resource+"/"+tc.name, func(t *testing.T) {
				raw, _ := json.Marshal(map[string]any{resource: []any{}, "next_page_token": tc.token})
				path := "/v2/users"
				if resource == "meetings" {
					path = "/v2/users/me/meetings"
				}
				a, n := fakeAPI(t, "GET", path, 200, string(raw), nil)
				var err error
				if resource == "users" {
					_, err = a.listUsers(context.Background(), &ListUsersIn{NextPageToken: "prior-token"})
				} else {
					_, err = a.listMeetings(context.Background(), &ListMeetingsIn{NextPageToken: "prior-token"})
				}
				if err == nil || *n != 1 {
					t.Fatalf("invalid continuation accepted; error=%v requests=%d", err, *n)
				}
			})
		}
	}
}

func TestEmptyFinalAndDistinctTokens(t *testing.T) {
	for _, resource := range []string{"users", "meetings"} {
		for _, tc := range []struct{ prior, next string }{{"", ""}, {"prior-token", ""}, {"prior-token", strings.Repeat("x", 4096)}} {
			raw, _ := json.Marshal(map[string]any{resource: []any{}, "next_page_token": tc.next})
			path := "/v2/users"
			if resource == "meetings" {
				path = "/v2/users/me/meetings"
			}
			a, _ := fakeAPI(t, "GET", path, 200, string(raw), nil)
			var more bool
			var err error
			if resource == "users" {
				o, e := a.listUsers(context.Background(), &ListUsersIn{NextPageToken: tc.prior})
				err = e
				if o != nil {
					more = o.HasMore
				}
			} else {
				o, e := a.listMeetings(context.Background(), &ListMeetingsIn{NextPageToken: tc.prior})
				err = e
				if o != nil {
					more = o.HasMore
				}
			}
			if err != nil || more != (tc.next != "") {
				t.Fatalf("valid token rejected: resource=%s error=%v", resource, err)
			}
		}
	}
}
