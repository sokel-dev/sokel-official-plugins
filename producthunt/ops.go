package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-official-plugins/producthunt/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

func postFrom(p phPost) schema.Post {
	var makers []string
	for _, m := range p.Makers {
		makers = append(makers, m.Username)
	}
	return schema.Post{
		ID: p.ID, Slug: p.Slug, Name: p.Name, Tagline: p.Tagline, Description: p.Description, URL: p.URL,
		Website: p.Website, VotesCount: p.VotesCount, CommentsCount: p.CommentsCount, CreatedAt: p.CreatedAt,
		FeaturedAt: p.FeaturedAt, Makers: strings.Join(makers, ","),
	}
}

func commentFrom(c phComment) schema.Comment {
	out := schema.Comment{ID: c.ID, Body: c.Body, URL: c.URL, VotesCount: c.VotesCount, ParentID: c.ParentID, CreatedAt: c.CreatedAt, IsMine: c.User.IsViewer}
	if !c.User.redacted() {
		out.Username = c.User.Username
	}
	return out
}

// flatComments lists top-level comments with their replies right after them.
func flatComments(p phPost) []phComment {
	var out []phComment
	for _, e := range p.Comments.Edges {
		out = append(out, e.Node)
		for _, r := range e.Node.Replies.Edges {
			out = append(out, r.Node)
		}
	}
	return out
}

func clamp(n, def, max int) int {
	if n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

func fetchPost(ctx context.Context, c Cred, query, slug string, vars map[string]any) (phPost, error) {
	if vars == nil {
		vars = map[string]any{}
	}
	vars["slug"] = slug
	var out struct {
		Post *phPost `json:"post"`
	}
	if err := gql(ctx, c, query, vars, &out); err != nil {
		return phPost{}, err
	}
	if out.Post == nil {
		return phPost{}, fmt.Errorf("Product Hunt 上没有 slug 为「%s」的产品", slug)
	}
	return *out.Post, nil
}

func opPostGet(ctx plugin.Ctx, in *PhPostGetIn) (*PhPostGetOut, error) {
	slug, err := slugOf(in.Post)
	if err != nil {
		return nil, err
	}
	p, err := fetchPost(ctx, credOf(ctx), qPost, slug, nil)
	if err != nil {
		return nil, err
	}
	return &PhPostGetOut{Post: postFrom(p)}, nil
}

// fetchComments pages through a post's newest comments (pageSize per request) until `limit` top-level comments
// are in hand or stop(page) says the rest is older than needed. Returns the post with all pages merged.
func fetchComments(ctx context.Context, c Cred, slug string, limit int, stop func(page phPost) bool) (phPost, error) {
	var merged phPost
	after := ""
	for len(merged.Comments.Edges) < limit {
		vars := map[string]any{"first": pageSize}
		if after != "" {
			vars["after"] = after
		}
		p, err := fetchPost(ctx, c, qComments, slug, vars)
		if err != nil {
			return phPost{}, err
		}
		if after == "" {
			merged = p
		} else {
			merged.Comments.Edges = append(merged.Comments.Edges, p.Comments.Edges...)
		}
		if !p.Comments.PageInfo.HasNextPage || p.Comments.PageInfo.EndCursor == "" || (stop != nil && stop(p)) {
			break
		}
		after = p.Comments.PageInfo.EndCursor
	}
	if len(merged.Comments.Edges) > limit {
		merged.Comments.Edges = merged.Comments.Edges[:limit]
	}
	return merged, nil
}

func opComments(ctx plugin.Ctx, in *PhCommentsIn) (*PhCommentsOut, error) {
	slug, err := slugOf(in.Post)
	if err != nil {
		return nil, err
	}
	p, err := fetchComments(ctx, credOf(ctx), slug, clamp(in.Limit, pageSize, 50), nil)
	if err != nil {
		return nil, err
	}
	var items []schema.Comment
	for _, c := range flatComments(p) {
		items = append(items, commentFrom(c))
	}
	return &PhCommentsOut{Items: items, Count: len(items), Total: p.Comments.TotalCount}, nil
}

// pacific is the time zone Product Hunt's daily leaderboard is cut in.
var pacific = func() *time.Location {
	if l, err := time.LoadLocation("America/Los_Angeles"); err == nil {
		return l
	}
	return time.FixedZone("PT", -8*3600)
}()

func opLeaderboard(ctx plugin.Ctx, in *PhLeaderboardIn) (*PhLeaderboardOut, error) {
	day := time.Now().In(pacific)
	if s := strings.TrimSpace(in.Date); s != "" {
		d, err := time.ParseInLocation("2006-01-02", s, pacific)
		if err != nil {
			return nil, fmt.Errorf("日期要写成 YYYY-MM-DD：%v", err)
		}
		day = d
	}
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, pacific)
	order := in.Order
	if order == "" {
		order = "RANKING"
	}
	limit := clamp(in.Limit, pageSize, 50)
	var items []schema.Post
	cursor := ""
	for len(items) < limit {
		var out struct {
			Posts struct {
				PageInfo phPageInfo `json:"pageInfo"`
				Edges    []struct {
					Node phPost `json:"node"`
				} `json:"edges"`
			} `json:"posts"`
		}
		vars := map[string]any{
			"after": start.Format(time.RFC3339), "before": start.Add(24 * time.Hour).Format(time.RFC3339),
			"order": order, "first": pageSize,
		}
		if cursor != "" {
			vars["cursor"] = cursor
		}
		if err := gql(ctx, credOf(ctx), qLeaderboard, vars, &out); err != nil {
			return nil, err
		}
		for _, e := range out.Posts.Edges {
			if len(items) < limit {
				items = append(items, postFrom(e.Node))
			}
		}
		if !out.Posts.PageInfo.HasNextPage || out.Posts.PageInfo.EndCursor == "" || len(out.Posts.Edges) == 0 {
			break
		}
		cursor = out.Posts.PageInfo.EndCursor
	}
	return &PhLeaderboardOut{Items: items, Count: len(items)}, nil
}

type viewerResp struct {
	Viewer *struct {
		User struct {
			ID        string `json:"id"`
			Username  string `json:"username"`
			MadePosts struct {
				Edges []struct {
					Node struct {
						Slug string `json:"slug"`
					} `json:"node"`
				} `json:"edges"`
			} `json:"madePosts"`
		} `json:"user"`
	} `json:"viewer"`
}

func opHealthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	var v viewerResp
	if err := gql(ctx, credOf(ctx), qViewer, nil, &v); err != nil {
		return &HealthCheckOut{OK: false, Message: err.Error()}, nil
	}
	if v.Viewer == nil {
		return &HealthCheckOut{OK: false, Message: "token 可用，但读不到对应的账号（要用应用页下方的 Developer Token，不是 API key）"}, nil
	}
	return &HealthCheckOut{OK: true, Message: "凭证可用", Username: v.Viewer.User.Username}, nil
}
