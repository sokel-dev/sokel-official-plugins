package main

// Event source: watches the data sources configured on the credential and emits new-row /
// row-updated events.
//
// **Why polling instead of webhooks**: a Notion webhook subscription can only be created by hand on
// its integration settings page (and you have to paste the verification_token back in to verify it);
// the API can't create one, and one integration can only register a single URL. In other words,
// Notion simply doesn't offer "the plugin sets up the webhook for the user". For real-time updates,
// paste the canvas's webhook trigger node address into Notion instead (it's in the manual) — that
// path hands you an entity id, which you follow up with a "get page" to fetch content.
//
// Three rules learned from the gmail/synology event sources:
//   - **Never emit history on first start**: connecting to a database that's three years old would
//     instantly flood the workflow with thousands of rows. With no cursor yet, just record the
//     current position.
//   - **Write the cursor back to the credential**: keeping it only in memory means a restart either
//     re-emits everything or misses changes made while the process was down.
//   - **Prefer duplicates over loss**: the event id includes last_edited_time, so editing the same
//     row twice produces two events, while re-emitting the same edit is deduplicated on the
//     platform side.

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

const (
	defaultPollSeconds = 60
	// minPollSeconds: Notion's rate limit is 3 req/sec, and watching N tables means N requests per
	// round. Going faster than this buys nothing — the event's own latency already dwarfs it.
	minPollSeconds = 15
	// watchCursorField: the field name the cursor is written back to on the credential (one
	// timestamp per data source, as JSON).
	watchCursorField = "watch_cursor"
	// maxPerRound: the max number of events emitted per round. When hundreds of rows change at
	// once (a bulk paste), not capping this would instantly flood the workflow engine; the rest
	// carries over to the next round, and the cursor only advances as far as what was emitted.
	maxPerRound = 50
)

// runWatchSource: one instance per credential (the SDK's per-credential supervisor handles
// starting and stopping it).
func runWatchSource(ctx plugin.SourceCtx) error {
	cred := sokel.SourceCredentialAs[Cred](ctx)
	targets := splitList(cred.WatchDataSources)
	if len(targets) == 0 {
		// Nothing configured means events aren't used. Sit quietly rather than report an error
		// every minute.
		log.Printf("notion: 凭证未配置监听的数据源，事件源空转")
		ctx.ReportStatus("running", "未配置监听的数据源")
		<-ctx.Done()
		return ctx.Err()
	}
	interval := pollInterval(cred.PollSeconds)
	cursors := parseCursors(cred.WatchCursor)

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		changed := false
		for _, t := range targets {
			dsID, err := resolveDataSourceSource(ctx, t)
			if err != nil {
				log.Printf("notion: 认不出监听目标 %s: %v", t, err)
				ctx.ReportStatus("error", fmt.Sprintf("监听目标 %s 不可用: %v", t, err))
				continue
			}
			next, err := pollOnce(ctx, dsID, cursors[dsID])
			if err != nil {
				log.Printf("notion: 轮询 %s 失败（%v），%s 后重试", dsID, err, interval)
				ctx.ReportStatus("error", err.Error())
				continue
			}
			if next != cursors[dsID] {
				cursors[dsID] = next
				changed = true
			}
			ctx.ReportStatus("running", "")
		}
		if changed {
			// A write failure is only logged: events already emitted this round can't be taken
			// back, and the next round will replay from the old cursor (prefer duplicates over
			// loss — the platform side deduplicates events).
			if err := ctx.UpdateCredential(map[string]string{watchCursorField: dumpCursors(cursors)}); err != nil {
				log.Printf("notion: 游标写回凭证失败（重启后可能重推）: %v", err)
			}
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

// pollOnce fetches one round of changes, emits events, and returns the new cursor (the largest
// last_edited_time seen this round).
func pollOnce(ctx plugin.SourceCtx, dsID, cursor string) (string, error) {
	body := map[string]any{
		// Sorted descending by last-edited time: both creates and updates touch this field, so
		// one sort order covers both kinds of events.
		"sorts":     []any{map[string]any{"timestamp": "last_edited_time", "direction": "descending"}},
		"page_size": maxPerRound,
	}
	if cursor != "" {
		body["filter"] = map[string]any{
			"timestamp":        "last_edited_time",
			"last_edited_time": map[string]any{"after": cursor},
		}
	}
	var lr listResponse
	if err := callAPI(ctx, reqOpts{method: http.MethodPost, path: "/data_sources/" + dsID + "/query", body: body}, &lr); err != nil {
		return "", err
	}

	pages := make([]notionPage, 0, len(lr.Results))
	for _, raw := range lr.Results {
		var p notionPage
		if json.Unmarshal(raw, &p) == nil {
			pages = append(pages, p)
		}
	}
	// First start: just record the current position, **no history is emitted**.
	if cursor == "" {
		now := time.Now().UTC().Format(time.RFC3339)
		if len(pages) > 0 {
			now = pages[0].LastEditedTime
		}
		log.Printf("notion: 数据源 %s 首次启动，从 %s 开始（不推历史行）", dsID, now)
		return now, nil
	}
	// Fetched in descending order; emit in ascending order so downstream sees events in the same
	// order they actually happened.
	sort.Slice(pages, func(i, j int) bool { return pages[i].LastEditedTime < pages[j].LastEditedTime })

	next := cursor
	for _, p := range pages {
		if err := emitPageEvent(ctx, dsID, p); err != nil {
			// Stop right here on failure, cursor doesn't advance: the next round retries from
			// this row.
			log.Printf("notion: 推事件失败 %s: %v", p.ID, err)
			return next, nil
		}
		if p.LastEditedTime > next {
			next = p.LastEditedTime
		}
	}
	return next, nil
}

// emitPageEvent converts one row into one event.
//
// Created vs. updated is decided by whether created_time equals last_edited_time — Notion doesn't
// give us this distinction directly, yet the two mean completely different things to a workflow
// ("a new task arrived" vs. "a task changed"). Editing properties right after creating a row still
// counts as a create, which is the safer bias: missing a "created" event is a real loss.
func emitPageEvent(ctx plugin.SourceCtx, dsID string, p notionPage) error {
	item := toPageItem(p)
	// The event id includes the edit timestamp: editing the same row twice produces two events,
	// while re-emitting the same edit gets deduplicated on the platform side.
	eventID := p.ID + "@" + p.LastEditedTime
	if isCreated(p) {
		return TriggerPageCreated(ctx, eventID, &PageCreatedEvent{
			PageID: p.ID, DataSourceID: dsID, URL: p.URL, Title: item.Title,
			CreatedTime: p.CreatedTime, LastEditedTime: p.LastEditedTime, Props: item.Props,
		})
	}
	return TriggerPageUpdated(ctx, eventID, &PageUpdatedEvent{
		PageID: p.ID, DataSourceID: dsID, URL: p.URL, Title: item.Title,
		CreatedTime: p.CreatedTime, LastEditedTime: p.LastEditedTime, Props: item.Props,
	})
}

// isCreated treats a row as "created" when created and last-edited are within a minute of each
// other. Both of Notion's timestamps only have minute precision; they're usually equal right at
// row creation, but filling in properties immediately after can put them one tick apart.
func isCreated(p notionPage) bool {
	if p.CreatedTime == "" || p.LastEditedTime == "" {
		return false
	}
	if p.CreatedTime == p.LastEditedTime {
		return true
	}
	c, err1 := time.Parse(time.RFC3339, p.CreatedTime)
	e, err2 := time.Parse(time.RFC3339, p.LastEditedTime)
	if err1 != nil || err2 != nil {
		return false
	}
	return e.Sub(c) <= time.Minute
}

// resolveDataSourceSource uses the same "link/database id -> data source id" resolution as the
// operations side, but **caches the result**: the event source runs a round every minute, and
// asking Notion again for the same target every round would be pure waste (the rate limit is only
// 3 req/sec).
var resolvedTargets = map[string]string{}

func resolveDataSourceSource(ctx plugin.SourceCtx, target string) (string, error) {
	if id, ok := resolvedTargets[target]; ok {
		return id, nil
	}
	id, err := resolveDataSource(ctx, target)
	if err != nil {
		return "", err
	}
	resolvedTargets[target] = id
	return id, nil
}

// —— A few small formats stored on the credential ——

func splitList(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '\n' || r == ' ' || r == '\t' }) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func pollInterval(s string) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		n = defaultPollSeconds
	}
	if n < minPollSeconds {
		n = minPollSeconds
	}
	return time.Duration(n) * time.Second
}

// parseCursors / dumpCursors: the cursor is JSON mapping "data source id -> timestamp".
// One credential can watch multiple tables, each tracked separately — sharing a single timestamp
// would let heavy changes on one table drag another table's progress forward too, and that
// table's rows would then never fire.
func parseCursors(s string) map[string]string {
	m := map[string]string{}
	if strings.TrimSpace(s) == "" {
		return m
	}
	if json.Unmarshal([]byte(s), &m) != nil {
		return map[string]string{}
	}
	return m
}

func dumpCursors(m map[string]string) string {
	b, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(b)
}
