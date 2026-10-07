package main

// Fixture capture: PH_CAPTURE=1 PH_TOKEN=… go test -run TestCaptureFixtures. Writes the raw data payloads of the
// real API into testdata/ using the exact queries the plugin sends. Not part of the normal run.

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestCaptureFixtures(t *testing.T) {
	if os.Getenv("PH_CAPTURE") == "" {
		t.Skip("set PH_CAPTURE=1 PH_TOKEN=… to re-capture")
	}
	c := Cred{DeveloperToken: os.Getenv("PH_TOKEN"), Proxy: os.Getenv("PH_PROXY")}
	ctx := context.Background()
	save := func(name string, query string, vars map[string]any) json.RawMessage {
		var raw json.RawMessage
		if err := gql(ctx, c, query, vars, &raw); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var pretty map[string]any
		_ = json.Unmarshal(raw, &pretty)
		b, _ := json.MarshalIndent(pretty, "", "  ")
		if err := os.WriteFile("testdata/"+name, b, 0o644); err != nil {
			t.Fatal(err)
		}
		return raw
	}
	save("viewer.json", qViewer, nil)
	day := time.Now().In(pacific).AddDate(0, 0, -1)
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, pacific)
	lbVars := map[string]any{
		"after": start.Format(time.RFC3339), "before": start.Add(24 * time.Hour).Format(time.RFC3339), "order": "RANKING", "first": pageSize,
	}
	lb := save("leaderboard_p1.json", qLeaderboard, lbVars)
	var l struct {
		Posts struct {
			PageInfo phPageInfo
			Edges    []struct{ Node phPost }
		}
	}
	_ = json.Unmarshal(lb, &l)
	lbVars["cursor"] = l.Posts.PageInfo.EndCursor
	save("leaderboard_p2.json", qLeaderboard, lbVars)
	best := l.Posts.Edges[0].Node
	for _, e := range l.Posts.Edges {
		if e.Node.CommentsCount > best.CommentsCount {
			best = e.Node
		}
	}
	t.Logf("leaderboard %s: %d posts (next %v); most commented %s (%d comments)", start.Format("2006-01-02"), len(l.Posts.Edges), l.Posts.PageInfo.HasNextPage, best.Slug, best.CommentsCount)
	save("post.json", qPost, map[string]any{"slug": best.Slug})
	cm := save("comments_p1.json", qComments, map[string]any{"slug": best.Slug, "first": pageSize})
	var p struct{ Post phPost }
	_ = json.Unmarshal(cm, &p)
	save("comments_p2.json", qComments, map[string]any{"slug": best.Slug, "first": pageSize, "after": p.Post.Comments.PageInfo.EndCursor})
	all := flatComments(p.Post)
	replies := 0
	for _, x := range all {
		if x.ParentID != "" {
			replies++
		}
	}
	t.Logf("comments page 1: %d top-level, %d incl. replies (%d replies), total %d, next %v", len(p.Post.Comments.Edges), len(all), replies, p.Post.Comments.TotalCount, p.Post.Comments.PageInfo.HasNextPage)
	save("post_missing.json", qPost, map[string]any{"slug": "no-such-product-zzz-404"})
}

// Live round: PH_LIVE=1 PH_TOKEN=… go test -run TestLiveRound -v. Runs the four operations and one poll round
// against the real API to catch what the fixtures cannot (complexity, rate limits). Not part of the normal run.
func TestLiveRound(t *testing.T) {
	if os.Getenv("PH_LIVE") == "" {
		t.Skip("set PH_LIVE=1 PH_TOKEN=… to run against the real API")
	}
	ctx := &fakeCtx{Context: context.Background(), cred: map[string]string{"developer_token": os.Getenv("PH_TOKEN"), "proxy": os.Getenv("PH_PROXY")}}
	h, err := opHealthCheck(ctx, &HealthCheckIn{})
	if err != nil || !h.OK {
		t.Fatalf("health: %+v %v", h, err)
	}
	t.Logf("health: %s", h.Username)
	c, err := opComments(ctx, &PhCommentsIn{Post: "rill-browser", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("comments: %d items, total %d", c.Count, c.Total)
	l, err := opLeaderboard(ctx, &PhLeaderboardIn{Limit: 25})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("leaderboard today: %d items", l.Count)
	src := &recordSource{fakeCtx: ctx}
	cred := Cred{DeveloperToken: ctx.cred["developer_token"], Proxy: ctx.cred["proxy"], WatchPosts: "rill-browser", WatchMyPosts: "on"}
	cur := &cursor{T: time.Now().Add(-24 * time.Hour).Unix(), Seen: map[string]int64{}}
	if err := pollRound(src, cred, true, slugList(cred.WatchPosts), cur); err != nil {
		t.Fatal(err)
	}
	t.Logf("poll round: %d events pushed, cursor now %d", len(src.events), cur.T)
}
