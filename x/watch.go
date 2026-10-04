package main

// Event source: polls "mentions" and "keywords", emitting new tweets as events.
//
// **Why polling**: X's real-time push (filtered stream / Account Activity API) is only available on
// the Enterprise tier, unreachable on the self-serve plan — this isn't laziness, that path simply
// isn't open to us. n8n's and Make's X nodes don't even have a trigger at all.
//
// Four rules, learned from the notion/gmail event sources:
//   - **Never emit history on first start**: connecting a ten-year-old account would instantly
//     flood the workflow with thousands of mentions. With no cursor yet, just record the current
//     position.
//   - **Write the cursor back to the credential**: keeping it only in memory means a restart either
//     re-emits everything or misses changes made while the process was down.
//   - **Stop right where a push fails**: the cursor doesn't advance, and the next round retries
//     from here (prefer duplicates over loss — the platform side deduplicates).
//   - **The interval has a floor**: X charges per read, and an accidental 10-second poll turns into
//     a bill for tens of thousands of calls a day.

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-official-plugins/x/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

const (
	defaultPollSeconds = 300
	// minPollSeconds: going faster than this buys nothing — a mention's own latency is already
	// well over a minute, and every round costs money (reading one costs roughly $0.005).
	minPollSeconds   = 60
	watchCursorField = "watch_cursor"
	// maxPerRound: the max number of events emitted per round. A retweet from a big account can
	// cause mentions to surge in all at once; not capping this would flood the workflow engine,
	// so the rest carries over to the next round.
	maxPerRound = 25
)

// cursors holds one cursor per stream (mentions / keywords), stored together as JSON.
type cursors struct {
	Mentions string `json:"mentions"`
	Query    string `json:"query"`
}

func runWatchSource(ctx plugin.SourceCtx) error {
	cred := sokel.SourceCredentialAs[Cred](ctx)
	watchMentions := strings.EqualFold(strings.TrimSpace(cred.WatchMentions), "on")
	query := strings.TrimSpace(cred.WatchQuery)
	if !watchMentions && query == "" {
		// Nothing configured means events aren't used. Sit quietly rather than report an error
		// every minute.
		log.Printf("x: 凭证没开监听（提及关闭且无查询式），事件源空转")
		ctx.ReportStatus("running", "未开启监听")
		<-ctx.Done()
		return ctx.Err()
	}
	interval := pollInterval(cred.PollSeconds)
	cur := parseCursors(cred.WatchCursor)

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		changed := false
		if watchMentions {
			// Fetched fresh every round: who the authorized account is gets cached per token by
			// me(), so what's returned here is almost always the cached value; and when
			// authorization has expired it errors — which is exactly what should be surfaced.
			u, err := me(ctx)
			switch {
			case err != nil:
				log.Printf("x: 取授权账号失败（%v），跳过本轮提及", err)
				ctx.ReportStatus("error", err.Error())
			default:
				next, err := pollOnce(ctx, "/users/"+u.ID+"/mentions", nil, cur.Mentions, "", true)
				switch {
				case err != nil:
					log.Printf("x: 轮询提及失败（%v），%s 后重试", err, interval)
					ctx.ReportStatus("error", err.Error())
				case next != cur.Mentions:
					cur.Mentions, changed = next, true
				}
			}
		}
		if query != "" {
			q := url.Values{"query": {query}, "sort_order": {"recency"}}
			next, err := pollOnce(ctx, "/tweets/search/recent", q, cur.Query, query, false)
			switch {
			case err != nil:
				log.Printf("x: 轮询关键词失败（%v），%s 后重试", err, interval)
				ctx.ReportStatus("error", err.Error())
			case next != cur.Query:
				cur.Query, changed = next, true
			}
		}
		if changed {
			// A write failure is only logged: events already emitted this round can't be taken
			// back, and the next round will replay from the old cursor.
			if err := ctx.UpdateCredential(map[string]string{watchCursorField: dumpCursors(cur)}); err != nil {
				log.Printf("x: 游标写回凭证失败（重启后可能重推）: %v", err)
			}
		}
		ctx.ReportStatus("running", "")
		t := time.NewTimer(interval)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		}
	}
}

// pollOnce fetches one round of changes and emits events, returning the new cursor.
func pollOnce(ctx plugin.SourceCtx, path string, extra url.Values, cursor, matched string, mention bool) (string, error) {
	q := readQuery()
	for k, vs := range extra {
		q[k] = vs
	}
	q.Set("max_results", strconv.Itoa(pageSize(maxPerRound, path)))
	if cursor != "" {
		q.Set("since_id", cursor)
	}
	var env listEnvelope
	if err := callRead(ctx, reqOpts{method: http.MethodGet, path: path, query: q}, &env); err != nil {
		return cursor, err
	}
	items := toPosts(env)
	// First start: just record the current position, **no history is emitted**.
	if cursor == "" {
		if env.Meta.NewestID == "" {
			return "", nil
		}
		log.Printf("x: %s 首次启动，从 %s 开始（不推历史）", path, env.Meta.NewestID)
		return env.Meta.NewestID, nil
	}
	// Fetched in descending order; emitted in ascending order so downstream sees events in the
	// same order they actually happened.
	sort.Slice(items, func(i, j int) bool { return idLess(items[i].ID, items[j].ID) })

	next := cursor
	for _, p := range items {
		if err := emit(ctx, p, matched, mention); err != nil {
			log.Printf("x: 推事件失败 %s: %v", p.ID, err)
			return next, nil // Stop right here, cursor doesn't advance
		}
		if idLess(next, p.ID) {
			next = p.ID
		}
	}
	return next, nil
}

func emit(ctx plugin.SourceCtx, p schema.Post, matched string, mention bool) error {
	if mention {
		return TriggerMentionReceived(ctx, p.ID, &MentionReceivedEvent{
			PostID: p.ID, Text: p.Text, AuthorID: p.AuthorID, AuthorUsername: p.AuthorUsername,
			AuthorName: p.AuthorName, ConversationID: p.ConversationID, URL: p.URL,
			CreatedAt: p.CreatedAt, Kind: p.Kind,
		})
	}
	return TriggerKeywordMatched(ctx, p.ID, &KeywordMatchedEvent{
		PostID: p.ID, Text: p.Text, AuthorID: p.AuthorID, AuthorUsername: p.AuthorUsername,
		AuthorName: p.AuthorName, ConversationID: p.ConversationID, URL: p.URL,
		CreatedAt: p.CreatedAt, Kind: p.Kind, MatchedQuery: matched,
	})
}

func pollInterval(raw string) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n <= 0 {
		n = defaultPollSeconds
	}
	if n < minPollSeconds {
		n = minPollSeconds
	}
	return time.Duration(n) * time.Second
}

func parseCursors(raw string) cursors {
	var c cursors
	if s := strings.TrimSpace(raw); s != "" {
		_ = json.Unmarshal([]byte(s), &c)
	}
	return c
}

func dumpCursors(c cursors) string {
	b, _ := json.Marshal(c)
	return string(b)
}
