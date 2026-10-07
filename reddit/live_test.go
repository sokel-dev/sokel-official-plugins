package main

// Live check: REDDIT_LIVE=1 go test -run TestLive -v. Two real feed fetches through the pacer (about a minute
// apart) to confirm the client, the User-Agent and the proxy path against reddit.com. Not part of the normal run.

import (
	"context"
	"os"
	"testing"
)

func TestLive(t *testing.T) {
	if os.Getenv("REDDIT_LIVE") == "" {
		t.Skip("set REDDIT_LIVE=1 to hit reddit.com")
	}
	ctx := &fakeCtx{Context: context.Background(), cred: map[string]string{"proxy": os.Getenv("REDDIT_PROXY"), "watch_user": "importcjj"}}
	h, _ := opHealthCheck(ctx, &HealthCheckIn{})
	t.Logf("health: %+v", h)
	if !h.OK {
		t.Fatal(h.Message)
	}
	s, err := opSearch(ctx, &RedditSearchIn{Query: "workflow", Subreddits: "golang+selfhosted", Time: "day"})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("search: %d posts, first %q", s.Count, func() string {
		if s.Count > 0 {
			return s.Items[0].Title
		}
		return ""
	}())
}
