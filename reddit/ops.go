package main

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/sokel-dev/sokel-official-plugins/reddit/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

func itemFrom(it item) schema.Item {
	return schema.Item{
		Name: it.Name, Kind: it.Kind, Subreddit: it.Subreddit, Author: it.Author, AuthorURL: it.AuthorURL,
		Title: it.Title, Text: plainText(it.HTML), TextHTML: it.HTML, URL: it.URL, PostID: it.PostID,
		CreatedAt: unixRFC3339(it.Created),
	}
}

func itemsFrom(items []item, kind string) []schema.Item {
	var out []schema.Item
	for _, it := range items {
		if kind == "" || it.Kind == kind {
			out = append(out, itemFrom(it))
		}
	}
	return out
}

// searchPosts: /search.rss (site-wide) or /r/<subs>/search.rss?restrict_sr=1.
func searchPosts(ctx context.Context, c Cred, query, subs, sort, timeRange string) ([]item, error) {
	q := url.Values{"q": {query}, "sort": {sort}, "t": {timeRange}}
	path := "/search.rss"
	if subs != "" {
		path = "/r/" + subs + "/search.rss"
		q.Set("restrict_sr", "1")
	}
	return fetchFeed(ctx, c, path, q)
}

func opSearch(ctx plugin.Ctx, in *RedditSearchIn) (*RedditSearchOut, error) {
	query := strings.TrimSpace(in.Query)
	if query == "" {
		return nil, fmt.Errorf("关键词不能为空")
	}
	subs, err := subsOf(in.Subreddits)
	if err != nil {
		return nil, err
	}
	sort, timeRange := in.Sort, in.Time
	if sort == "" {
		sort = "new"
	}
	if timeRange == "" {
		timeRange = "week"
	}
	items, err := searchPosts(ctx, credOf(ctx), query, subs, sort, timeRange)
	if err != nil {
		return nil, err
	}
	out := itemsFrom(items, "post")
	return &RedditSearchOut{Items: out, Count: len(out)}, nil
}

func opSubredditNew(ctx plugin.Ctx, in *RedditSubredditNewIn) (*RedditSubredditNewOut, error) {
	subs, err := subsOf(in.Subreddits)
	if err != nil {
		return nil, err
	}
	if subs == "" {
		return nil, fmt.Errorf("版块不能为空")
	}
	items, err := fetchFeed(ctx, credOf(ctx), "/r/"+subs+"/new.rss", nil)
	if err != nil {
		return nil, err
	}
	out := itemsFrom(items, "post")
	return &RedditSubredditNewOut{Items: out, Count: len(out)}, nil
}

// postComments fetches /comments/<id>/.rss: the post itself leads, its comments follow.
func postComments(ctx context.Context, c Cred, id string) (post *item, comments []item, err error) {
	items, err := fetchFeed(ctx, c, "/comments/"+id+"/.rss", nil)
	if err != nil {
		return nil, nil, err
	}
	for i := range items {
		switch items[i].Kind {
		case "post":
			if post == nil {
				p := items[i]
				post = &p
			}
		case "comment":
			comments = append(comments, items[i])
		}
	}
	if post == nil {
		return nil, nil, fmt.Errorf("Reddit 上没有 id 为「%s」的帖子（或它已被删除）", id)
	}
	return post, comments, nil
}

func opPostComments(ctx plugin.Ctx, in *RedditPostCommentsIn) (*RedditPostCommentsOut, error) {
	id, err := postIDOf(in.Post)
	if err != nil {
		return nil, err
	}
	post, comments, err := postComments(ctx, credOf(ctx), id)
	if err != nil {
		return nil, err
	}
	out := itemsFrom(comments, "comment")
	return &RedditPostCommentsOut{Post: itemFrom(*post), Items: out, Count: len(out)}, nil
}

func userPosts(ctx context.Context, c Cred, user string) ([]item, error) {
	items, err := fetchFeed(ctx, c, "/user/"+user+"/submitted.rss", nil)
	if err != nil {
		return nil, err
	}
	var out []item
	for _, it := range items {
		if it.Kind == "post" {
			out = append(out, it)
		}
	}
	return out, nil
}

func opUserPosts(ctx plugin.Ctx, in *RedditUserPostsIn) (*RedditUserPostsOut, error) {
	user, err := userOf(in.Username)
	if err != nil {
		return nil, err
	}
	items, err := userPosts(ctx, credOf(ctx), user)
	if err != nil {
		return nil, err
	}
	out := itemsFrom(items, "post")
	return &RedditUserPostsOut{Items: out, Count: len(out)}, nil
}

// opHealthCheck: the watched user's feed when one is set (404 = no such user), otherwise any feed at all.
func opHealthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	c := credOf(ctx)
	if u := strings.TrimSpace(c.WatchUser); u != "" {
		user, err := userOf(u)
		if err != nil {
			return &HealthCheckOut{OK: false, Message: err.Error()}, nil
		}
		items, err := userPosts(ctx, c, user)
		if err != nil {
			return &HealthCheckOut{OK: false, Message: err.Error()}, nil
		}
		return &HealthCheckOut{OK: true, Message: fmt.Sprintf("能连上 Reddit；u/%s 最近有 %d 个帖子", user, len(items))}, nil
	}
	if _, err := fetchFeed(ctx, c, "/r/announcements/new.rss", nil); err != nil {
		return &HealthCheckOut{OK: false, Message: err.Error()}, nil
	}
	return &HealthCheckOut{OK: true, Message: "能连上 Reddit（没配监听的用户名）"}, nil
}
