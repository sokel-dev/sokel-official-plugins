package main

// Event source: poll the comments of the user's recent posts (plus any post listed) and, if configured, new posts
// matching a keyword.
//
// Same rules as the other polling sources: the first round pushes no history, the cursor lives in the credential,
// a failed push stops the round without moving the cursor (the platform deduplicates on the event id), the interval
// has a floor. Each stream remembers the newest created time it pushed plus the names pushed in a short lookback
// window, so a late-indexed item is neither missed nor pushed twice.
//
// Budget: every feed costs one of Reddit's one-per-minute slots, so a round takes (posts watched + 2) minutes; the
// interval is counted from the end of a round and the number of posts per round is capped.

import (
	"context"
	"encoding/json"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

const (
	defaultPollSeconds = 600
	minPollSeconds     = 300
	defaultWatchDays   = 14
	lookbackSeconds    = 300
	maxPostsPerRound   = 5
)

type stream struct {
	T    int64            `json:"t"`
	Seen map[string]int64 `json:"seen,omitempty"`
}

type cursors struct {
	Comments *stream `json:"comments,omitempty"`
	Keyword  *stream `json:"keyword,omitempty"`
	Query    string  `json:"query,omitempty"` // the query + subreddits the keyword stream was started for
}

func fresh(now int64) *stream { return &stream{T: now, Seen: map[string]int64{}} }

func (s *stream) since() int64 { return s.T - lookbackSeconds }

func (s *stream) take(name string, at int64) {
	if s.Seen == nil {
		s.Seen = map[string]int64{}
	}
	s.Seen[name] = at
	if at > s.T {
		s.T = at
	}
}

func (s *stream) prune() {
	for n, at := range s.Seen {
		if at < s.since() {
			delete(s.Seen, n)
		}
	}
}

func atoiOr(s string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n > 0 {
		return n
	}
	return def
}

func pollInterval(s string) time.Duration {
	n := atoiOr(s, defaultPollSeconds)
	if n < minPollSeconds {
		n = minPollSeconds
	}
	return time.Duration(n) * time.Second
}

type watchConfig struct {
	user  string
	items []string
	query string
	subs  string
	days  int
}

func configOf(cred Cred) (watchConfig, error) {
	cfg := watchConfig{items: postIDList(cred.WatchItems), query: strings.TrimSpace(cred.WatchQuery), days: atoiOr(cred.WatchDays, defaultWatchDays)}
	if u := strings.TrimSpace(cred.WatchUser); u != "" {
		user, err := userOf(u)
		if err != nil {
			return cfg, err
		}
		cfg.user = user
	}
	subs, err := subsOf(cred.WatchSubreddits)
	if err != nil {
		return cfg, err
	}
	cfg.subs = subs
	return cfg, nil
}

func (c watchConfig) watching() bool { return c.user != "" || len(c.items) > 0 || c.query != "" }

func runWatchSource(ctx plugin.SourceCtx) error {
	cred := sokel.SourceCredentialAs[Cred](ctx)
	cfg, err := configOf(cred)
	if err != nil {
		log.Printf("reddit: 凭证的监听配置有误：%v", err)
		ctx.ReportStatus("error", err.Error())
		<-ctx.Done()
		return ctx.Err()
	}
	if !cfg.watching() {
		log.Printf("reddit: 凭证没配监听，事件源空转")
		ctx.ReportStatus("running", "未开启监听")
		<-ctx.Done()
		return ctx.Err()
	}
	interval := pollInterval(cred.PollSeconds)
	var cur cursors
	_ = json.Unmarshal([]byte(cred.WatchCursor), &cur)

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		before, _ := json.Marshal(cur)
		errs := watchRound(ctx, cred, cfg, &cur, time.Now().Unix())
		for _, e := range errs {
			log.Printf("reddit: 轮询失败（%s），%s 后重试", e, interval)
		}
		if after, _ := json.Marshal(cur); string(after) != string(before) {
			if err := ctx.UpdateCredential(map[string]string{"watch_cursor": string(after)}); err != nil {
				log.Printf("reddit: 游标写回凭证失败（重启后可能重推）: %v", err)
			}
		}
		if len(errs) > 0 {
			ctx.ReportStatus("error", strings.Join(errs, "；"))
		} else {
			ctx.ReportStatus("running", "")
		}
		t := time.NewTimer(interval)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		}
	}
}

func watchRound(ctx plugin.SourceCtx, cred Cred, cfg watchConfig, cur *cursors, now int64) []string {
	var errs []string
	if cfg.user != "" || len(cfg.items) > 0 {
		if cur.Comments == nil {
			cur.Comments = fresh(now)
		} else if err := pollComments(ctx, cred, cfg, cur.Comments, now); err != nil {
			errs = append(errs, "评论："+err.Error())
		}
	}
	if cfg.query != "" {
		key := cfg.query + "\x00" + cfg.subs
		if cur.Keyword == nil || cur.Query != key {
			cur.Keyword, cur.Query = fresh(now), key
		} else if err := pollKeyword(ctx, cred, cfg, cur.Keyword); err != nil {
			errs = append(errs, "关键词："+err.Error())
		}
	}
	return errs
}

func oldestFirst(items []item) {
	sort.SliceStable(items, func(i, j int) bool { return items[i].Created < items[j].Created })
}

// watchedPosts: the listed posts plus the user's newest posts within the window, capped per round.
func watchedPosts(ctx context.Context, cred Cred, cfg watchConfig, now int64) ([]string, error) {
	ids := append([]string{}, cfg.items...)
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	if cfg.user != "" {
		posts, err := userPosts(ctx, cred, cfg.user)
		if err != nil {
			return nil, err
		}
		sort.SliceStable(posts, func(i, j int) bool { return posts[i].Created > posts[j].Created })
		cutoff := now - int64(cfg.days)*86400
		for _, p := range posts {
			if p.Created < cutoff || seen[p.PostID] || p.PostID == "" {
				continue
			}
			seen[p.PostID] = true
			ids = append(ids, p.PostID)
		}
	}
	if len(ids) > maxPostsPerRound {
		ids = ids[:maxPostsPerRound]
	}
	return ids, nil
}

func pollComments(ctx plugin.SourceCtx, cred Cred, cfg watchConfig, s *stream, now int64) error {
	ids, err := watchedPosts(ctx, cred, cfg, now)
	if err != nil {
		return err
	}
	for _, id := range ids {
		post, comments, err := postComments(ctx, cred, id)
		if err != nil {
			return err
		}
		oldestFirst(comments)
		for _, c := range comments {
			if c.Created < s.since() || (cfg.user != "" && strings.EqualFold(c.Author, cfg.user)) {
				continue
			}
			if _, ok := s.Seen[c.Name]; ok {
				continue
			}
			ev := CommentReceivedEvent{
				CommentID: c.Name, Text: plainText(c.HTML), TextHTML: c.HTML, Author: c.Author, AuthorURL: c.AuthorURL,
				URL: c.URL, Subreddit: c.Subreddit, PostID: post.PostID, PostTitle: post.Title, PostURL: post.URL,
				CreatedAt: unixRFC3339(c.Created),
			}
			if err := TriggerCommentReceived(ctx, "reddit:comment:"+c.Name, &ev); err != nil {
				return err
			}
			s.take(c.Name, c.Created)
		}
	}
	s.prune()
	return nil
}

func pollKeyword(ctx plugin.SourceCtx, cred Cred, cfg watchConfig, s *stream) error {
	items, err := searchPosts(ctx, cred, cfg.query, cfg.subs, "new", "day")
	if err != nil {
		return err
	}
	oldestFirst(items)
	for _, it := range items {
		if it.Kind != "post" || it.Created < s.since() {
			continue
		}
		if _, ok := s.Seen[it.Name]; ok {
			continue
		}
		ev := KeywordMatchedEvent{
			Name: it.Name, Title: it.Title, Text: plainText(it.HTML), TextHTML: it.HTML, Author: it.Author, AuthorURL: it.AuthorURL,
			URL: it.URL, Subreddit: it.Subreddit, CreatedAt: unixRFC3339(it.Created), MatchedQuery: cfg.query,
		}
		if err := TriggerKeywordMatched(ctx, "reddit:keyword:"+it.Name, &ev); err != nil {
			return err
		}
		s.take(it.Name, it.Created)
	}
	s.prune()
	return nil
}
