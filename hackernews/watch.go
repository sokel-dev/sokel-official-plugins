package main

// Event source: poll for new comments (on the watched user's stories, on extra watched stories, and replies to the
// watched user's comments anywhere) and for keyword matches, and push each one as an event.
//
// Rules carried over from the other polling sources (x, notion, gmail):
//   - The first run pushes no history: connecting an account that has years of comments must not flood a workflow.
//     With no cursor, only the current position is recorded.
//   - The cursor is written back to the credential, so a restart neither replays everything nor drops what arrived
//     while the plugin was down.
//   - A failed push stops the round where it is: the cursor does not move past it, the next round retries (the
//     platform deduplicates on the event id, so a retry never triggers twice).
//   - The interval has a floor.
//
// HN-specific: Algolia indexes a new comment up to about a minute after it is posted, so "created after the cursor"
// alone would miss a comment that was created before the cursor moved but indexed after. Each query therefore looks
// back lookbackSeconds before the cursor, and ids already pushed in that window are remembered and skipped.

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

const (
	defaultPollSeconds = 120
	minPollSeconds     = 60
	defaultWatchDays   = 14
	lookbackSeconds    = 300
	// maxPerRound caps one round: a story on the front page can draw hundreds of comments an hour, and the rest
	// simply goes out next round.
	maxPerRound = 50
	// maxWatched caps how many stories / own comments one query filters on (Algolia accepts long OR lists, but
	// a request URL has limits and older items rarely get new replies).
	maxWatched = 50
)

// stream is the cursor of one feed: the newest created_at seen, plus the ids pushed in the lookback window.
type stream struct {
	T    int64            `json:"t"`
	Seen map[string]int64 `json:"seen,omitempty"`
}

type cursors struct {
	Comments *stream `json:"comments,omitempty"`
	Keyword  *stream `json:"keyword,omitempty"`
	// Query is the keyword the keyword stream was started for: a changed keyword starts from now again instead
	// of replaying the new keyword's history.
	Query string `json:"query,omitempty"`
}

func parseCursors(s string) cursors {
	var c cursors
	_ = json.Unmarshal([]byte(s), &c)
	return c
}

func dumpCursors(c cursors) string {
	b, _ := json.Marshal(c)
	return string(b)
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

// fresh starts a stream at now: nothing before this moment is pushed.
func fresh(now int64) *stream { return &stream{T: now, Seen: map[string]int64{}} }

// since is where a query starts: the cursor minus the lookback.
func (s *stream) since() int64 { return s.T - lookbackSeconds }

// take reports whether a hit is new, and records it.
func (s *stream) take(id string, at int64) bool {
	if _, ok := s.Seen[id]; ok {
		return false
	}
	if s.Seen == nil {
		s.Seen = map[string]int64{}
	}
	s.Seen[id] = at
	if at > s.T {
		s.T = at
	}
	return true
}

// prune drops remembered ids that fell out of the lookback window.
func (s *stream) prune() {
	for id, at := range s.Seen {
		if at < s.since() {
			delete(s.Seen, id)
		}
	}
}

func runWatchSource(ctx plugin.SourceCtx) error {
	cred := sokel.SourceCredentialAs[Cred](ctx)
	user := strings.TrimSpace(cred.WatchUser)
	items := parseItemIDs(cred.WatchItems)
	query := strings.TrimSpace(cred.WatchQuery)
	if user == "" && len(items) == 0 && query == "" {
		// Not configured means events are not used. Stay quiet rather than reporting an error every minute.
		log.Printf("hackernews: 凭证没配监听（用户名、帖子、关键词都为空），事件源空转")
		ctx.ReportStatus("running", "未开启监听")
		<-ctx.Done()
		return ctx.Err()
	}
	interval := pollInterval(cred.PollSeconds)
	days := atoiOr(cred.WatchDays, defaultWatchDays)
	hc := clientFor(cred.Proxy)
	cur := parseCursors(cred.WatchCursor)

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		before := dumpCursors(cur)
		errs := watchRound(ctx, hc, watchConfig{user: user, items: items, query: query, days: days}, &cur, time.Now().Unix())
		for _, e := range errs {
			log.Printf("hackernews: 轮询失败（%s），%s 后重试", e, interval)
		}
		if after := dumpCursors(cur); after != before {
			// A failed write only gets logged: events already pushed cannot be taken back, and the next round
			// starts from the old cursor (deduplicated by event id on the platform).
			if err := ctx.UpdateCredential(map[string]string{"watch_cursor": after}); err != nil {
				log.Printf("hackernews: 游标写回凭证失败（重启后可能重推）: %v", err)
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

type watchConfig struct {
	user  string
	items []string
	query string
	days  int
}

// watchRound is one polling round. A stream with no cursor yet only records "now": the first round pushes nothing.
func watchRound(ctx plugin.SourceCtx, hc *http.Client, cfg watchConfig, cur *cursors, now int64) []string {
	var errs []string
	if cfg.user != "" || len(cfg.items) > 0 {
		if cur.Comments == nil {
			cur.Comments = fresh(now)
		} else if err := pollComments(ctx, hc, cfg.user, cfg.items, cfg.days, cur.Comments); err != nil {
			errs = append(errs, "评论："+err.Error())
		}
	}
	if cfg.query != "" {
		if cur.Keyword == nil || cur.Query != cfg.query {
			cur.Keyword, cur.Query = fresh(now), cfg.query
		} else if err := pollKeyword(ctx, hc, cfg.query, cur.Keyword); err != nil {
			errs = append(errs, "关键词："+err.Error())
		}
	}
	return errs
}

// pollComments pushes new comments on the watched stories, and replies to the watched user's comments.
func pollComments(ctx plugin.SourceCtx, hc *http.Client, user string, extra []string, days int, s *stream) error {
	window := strconv.FormatInt(time.Now().Add(-time.Duration(days)*24*time.Hour).Unix(), 10)
	stories := map[string]string{} // id -> title
	mine := map[string]bool{}      // ids the user wrote (stories and comments): replies to them are "to me"
	for _, id := range extra {
		stories[id] = ""
	}
	if user != "" {
		own, err := searchByDate(ctx, hc, url.Values{"tags": {"story,author_" + user},
			"numericFilters": {"created_at_i>" + window}, "hitsPerPage": {strconv.Itoa(maxWatched)}})
		if err != nil {
			return err
		}
		for _, h := range own {
			stories[h.ObjectID], mine[h.ObjectID] = h.Title, true
		}
		myComments, err := searchByDate(ctx, hc, url.Values{"tags": {"comment,author_" + user},
			"numericFilters": {"created_at_i>" + window}, "hitsPerPage": {strconv.Itoa(maxWatched)}})
		if err != nil {
			return err
		}
		for _, h := range myComments {
			mine[h.ObjectID] = true
		}
	}

	since := "created_at_i>=" + strconv.FormatInt(s.since(), 10)
	var hits []algoliaHit
	if len(stories) > 0 {
		var tags []string
		for id := range stories {
			tags = append(tags, "story_"+id)
		}
		sort.Strings(tags)
		got, err := searchByDate(ctx, hc, url.Values{"tags": {"comment,(" + strings.Join(tags, ",") + ")"},
			"numericFilters": {since}, "hitsPerPage": {strconv.Itoa(maxPerRound)}})
		if err != nil {
			return err
		}
		hits = append(hits, got...)
	}
	// Replies to the user's comments on other people's stories: filter on parent_id directly.
	var parents []string
	for id := range mine {
		if _, isStory := stories[id]; !isStory {
			parents = append(parents, "parent_id="+id)
		}
	}
	if len(parents) > 0 {
		sort.Strings(parents)
		got, err := searchByDate(ctx, hc, url.Values{"tags": {"comment"},
			"numericFilters": {since + ",(" + strings.Join(parents, ",") + ")"}, "hitsPerPage": {strconv.Itoa(maxPerRound)}})
		if err != nil {
			return err
		}
		hits = append(hits, got...)
	}

	// Oldest first, so the cursor moves forward with what has been pushed.
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].CreatedAtI < hits[j].CreatedAtI })
	pushed := 0
	for _, h := range hits {
		if pushed >= maxPerRound {
			break
		}
		if strings.EqualFold(h.Author, user) || h.Author == "" {
			continue // the user's own comments are not news to them
		}
		if _, ok := s.Seen[h.ObjectID]; ok {
			continue
		}
		ev := commentEvent(h)
		ev.IsReplyToMe = mine[ev.ParentID]
		if ev.StoryTitle == "" {
			ev.StoryTitle = stories[ev.StoryID]
		}
		if err := TriggerCommentReceived(ctx, "hn:comment:"+h.ObjectID, &ev); err != nil {
			return err
		}
		s.take(h.ObjectID, h.CreatedAtI)
		pushed++
	}
	s.prune()
	return nil
}

func pollKeyword(ctx plugin.SourceCtx, hc *http.Client, query string, s *stream) error {
	hits, err := searchByDate(ctx, hc, url.Values{"query": {query}, "tags": {"(story,comment)"},
		"numericFilters": {"created_at_i>=" + strconv.FormatInt(s.since(), 10)}, "hitsPerPage": {strconv.Itoa(maxPerRound)}})
	if err != nil {
		return err
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].CreatedAtI < hits[j].CreatedAtI })
	for _, h := range hits {
		if _, ok := s.Seen[h.ObjectID]; ok {
			continue
		}
		ev := keywordEvent(h, query)
		if err := TriggerKeywordMatched(ctx, "hn:keyword:"+h.ObjectID, &ev); err != nil {
			return err
		}
		s.take(h.ObjectID, h.CreatedAtI)
	}
	s.prune()
	return nil
}

// commentEvent maps a comment hit onto the "new comment" payload.
func commentEvent(h algoliaHit) CommentReceivedEvent {
	it := itemFromHit(h)
	ev := CommentReceivedEvent{
		CommentID: it.ID, Text: it.Text, TextHTML: it.TextHTML, Author: it.Author, AuthorURL: userURL(it.Author),
		URL: it.Permalink, StoryID: it.StoryID, StoryTitle: it.StoryTitle, ParentID: it.ParentID, CreatedAt: it.CreatedAt,
	}
	if ev.StoryID != "" {
		ev.StoryURL = permalink(ev.StoryID)
	}
	return ev
}

// keywordEvent maps a story or comment hit onto the "keyword matched" payload.
func keywordEvent(h algoliaHit, query string) KeywordMatchedEvent {
	it := itemFromHit(h)
	return KeywordMatchedEvent{
		Kind: it.Type, ItemID: it.ID, Title: it.Title, Text: it.Text, TextHTML: it.TextHTML, Link: it.URL,
		Author: it.Author, AuthorURL: userURL(it.Author), URL: it.Permalink, StoryID: it.StoryID,
		StoryTitle: it.StoryTitle, Points: it.Score, CreatedAt: it.CreatedAt, MatchedQuery: query,
	}
}
