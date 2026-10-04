package main

// 事件源：盯住凭证里配的数据源，把新增行 / 被改的行推成事件。
//
// **为什么是轮询而不是 webhook**：Notion 的 webhook 订阅只能在它的集成设置页手工建
// （还要把 verification_token 粘回去验证），API 建不了，一个集成也只能挂一个 URL。
// 也就是说「插件替用户装好 webhook」这条路 Notion 根本不给。要实时的话，
// 把画布上 webhook 触发节点的地址粘进 Notion 即可（说明书里有），那条路拿到的是
// entity id，接一个「读页面」补内容。
//
// 三条按 gmail/synology 那两个源踩出来的规矩：
//   - **首次启动不推历史**：接上一个用了三年的库，工作流会被几千行瞬间冲垮。
//     没有游标时只记下当前位置。
//   - **游标写回凭证**：只放内存的话，插件一重启要么重推全部、要么漏掉停机期间的改动。
//   - **宁可重复不丢**：事件 id 带上 last_edited_time，同一行改两次是两条事件，
//     而同一次改动重复推只会被平台去重掉。

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
	// minPollSeconds：Notion 限流是 3 次/秒，而盯 N 张表每轮就是 N 次请求。
	// 比这更快没有意义——事件本身的延迟远不止这点。
	minPollSeconds = 15
	// watchCursorField：游标存回凭证的字段名（每个数据源一个时间戳，JSON）。
	watchCursorField = "watch_cursor"
	// maxPerRound：一轮最多推多少条。一次性改了几百行（批量粘贴）时，
	// 不封顶会把工作流引擎瞬间打满；剩下的下一轮继续，游标只推进到已推的位置。
	maxPerRound = 50
)

// runWatchSource：一个凭证一个实例（SDK 的 per-credential supervisor 负责起停）。
func runWatchSource(ctx plugin.SourceCtx) error {
	cred := sokel.SourceCredentialAs[Cred](ctx)
	targets := splitList(cred.WatchDataSources)
	if len(targets) == 0 {
		// 不配就是不用事件。安静地待着，别每分钟报一次错。
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
			// 写失败只记日志：本轮已经推出去的事件收不回来，下一轮会从旧游标再来一遍
			// （宁可重复也不丢，平台侧对事件有去重）。
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

// pollOnce：拉一轮增量，推事件，返回新游标（本轮见到的最大 last_edited_time）。
func pollOnce(ctx plugin.SourceCtx, dsID, cursor string) (string, error) {
	body := map[string]any{
		// 按最后编辑时间倒序：新增与修改都会更新这个字段，一条排序覆盖两种事件。
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
	// 首次启动：只记下当前位置，**一条历史都不推**。
	if cursor == "" {
		now := time.Now().UTC().Format(time.RFC3339)
		if len(pages) > 0 {
			now = pages[0].LastEditedTime
		}
		log.Printf("notion: 数据源 %s 首次启动，从 %s 开始（不推历史行）", dsID, now)
		return now, nil
	}
	// 倒序拉回来的，按时间正序推——下游看到的顺序才与实际发生的顺序一致。
	sort.Slice(pages, func(i, j int) bool { return pages[i].LastEditedTime < pages[j].LastEditedTime })

	next := cursor
	for _, p := range pages {
		if err := emitPageEvent(ctx, dsID, p); err != nil {
			// 推失败就地停下，游标不再前进：下一轮会从这一条重来。
			log.Printf("notion: 推事件失败 %s: %v", p.ID, err)
			return next, nil
		}
		if p.LastEditedTime > next {
			next = p.LastEditedTime
		}
	}
	return next, nil
}

// emitPageEvent：一行 → 一条事件。
//
// 新增还是修改，看 created_time 是不是就是 last_edited_time —— Notion 不给这个区分，
// 而两者对工作流的意义完全不同（「新任务来了」vs「任务改了」）。
// 建行后立刻改属性会被判成新增，这比反过来好：漏掉一次「新增」是真的丢事件。
func emitPageEvent(ctx plugin.SourceCtx, dsID string, p notionPage) error {
	item := toPageItem(p)
	// 事件 id 带上编辑时刻：同一行改两次是两条事件，而同一次改动重复推会被平台去重。
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

// isCreated：创建与最后编辑相差在一分钟内就当成「新增」。
// Notion 的两个时间戳都只精确到分钟，建行时它们通常相等，但建完立刻填属性会差一格。
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

// resolveDataSourceSource：与操作侧同一套「链接/库 id → 数据源 id」，但结果**缓存住**：
// 事件源每分钟跑一轮，每轮都为同一个目标多问一次 Notion 是纯浪费（限流只有 3 次/秒）。
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

// —— 凭证里的几个小格式 ——

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

// parseCursors / dumpCursors：游标是「数据源 id → 时间戳」的 JSON。
// 一个凭证可以盯多张表，各盯各的进度——共用一个时间戳的话，
// 一张表被大量修改会把另一张表的进度也推过去，那些行就永远不会触发了。
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
