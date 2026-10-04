package main

// 事件源：轮询「提及」与「关键词」，把新推文推成事件。
//
// **为什么是轮询**：X 的实时推送（filtered stream / Account Activity API）只在 Enterprise 档，
// 自助档拿不到——这不是偷懒，是那条路根本不开放。n8n / Make 的 X 节点干脆连触发器都没有。
//
// 四条按 notion/gmail 那两个源踩出来的规矩：
//   - **首次启动不推历史**：接上一个用了十年的账号，工作流会被几千条提及瞬间冲垮。
//     没有游标时只记下当前位置。
//   - **游标写回凭证**：只放内存的话，插件一重启要么重推全部、要么漏掉停机期间的。
//   - **推失败就地停下**：游标不前进，下一轮从这一条重来（宁可重复不丢，平台侧有去重）。
//   - **间隔有下限**：X 的读是按条计费的，一个手滑的 10 秒轮询就是一天几万条的账单。

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
	// minPollSeconds：比这更快没有意义——提及的延迟本来就不止一分钟，
	// 而每轮都在花钱（读一条约 $0.005）。
	minPollSeconds   = 60
	watchCursorField = "watch_cursor"
	// maxPerRound：一轮最多推多少条。被大 V 转发时提及会瞬间涌进来，
	// 不封顶会把工作流引擎打满；剩下的下一轮继续。
	maxPerRound = 25
)

// cursors：两条流各一个游标（提及 / 关键词），存成一个 JSON。
type cursors struct {
	Mentions string `json:"mentions"`
	Query    string `json:"query"`
}

func runWatchSource(ctx plugin.SourceCtx) error {
	cred := sokel.SourceCredentialAs[Cred](ctx)
	watchMentions := strings.EqualFold(strings.TrimSpace(cred.WatchMentions), "on")
	query := strings.TrimSpace(cred.WatchQuery)
	if !watchMentions && query == "" {
		// 不配就是不用事件。安静待着，别每分钟报一次错。
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
			// 每轮现取：授权账号是谁由 me() 按 token 缓存，这里拿到的几乎总是缓存值；
			// 而授权失效时它会报错——那正是应该报出来的东西。
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
			// 写失败只记日志：本轮推出去的事件收不回来，下一轮会从旧游标再来一遍。
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

// pollOnce：拉一轮增量并推事件，返回新游标。
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
	// 首次启动：只记下当前位置，**一条历史都不推**。
	if cursor == "" {
		if env.Meta.NewestID == "" {
			return "", nil
		}
		log.Printf("x: %s 首次启动，从 %s 开始（不推历史）", path, env.Meta.NewestID)
		return env.Meta.NewestID, nil
	}
	// 倒序拉回来的，按 id 正序推——下游看到的顺序才与发生顺序一致。
	sort.Slice(items, func(i, j int) bool { return idLess(items[i].ID, items[j].ID) })

	next := cursor
	for _, p := range items {
		if err := emit(ctx, p, matched, mention); err != nil {
			log.Printf("x: 推事件失败 %s: %v", p.ID, err)
			return next, nil // 就地停下，游标不再前进
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
