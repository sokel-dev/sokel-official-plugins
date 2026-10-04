package main

// 增量游标。**这是本插件最容易做错的地方**，所以单独一个文件说清楚。
//
// 只按时间戳：同一秒发的第二条会被漏掉，而快讯类源一秒好几条是常态。
// 只按 id 集合：集合无限膨胀，游标最后会变成几十 KB 塞进数据表。
//
// 所以游标是「时间戳 + 最近见过的 id」两者合起来：
//   - 比时间戳新的 → 收；
//   - 时间戳相同但 id 没见过 → 也收（这就是同秒多条的解法）；
//   - id 见过 → 丢（重复推送的解法）。
// 见过的 id 只留最近 N 个——超过这个窗口的条目时间戳早就落后了，不会再撞上。

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-official-plugins/feed/schema"
)

// seenWindow：游标里保留多少个 id。50 条足以覆盖「同一时刻的一批」，
// 又不至于把游标撑大（50 × 8 字符哈希 ≈ 500 字节）。
const seenWindow = 50

// 游标的**外形**：`时间|id哈希,id哈希,…`，如
//
//	2026-08-19T08:35:52Z|2ceab3c4,6bb9e374,682f560a
//
// 早先是直接 dump JSON，语义没问题，但它要以**字符串**的形态落进数据表一格、
// 在调试台显示——于是满屏 `\"`，一串 147 字符里有三成是转义符。
// 这个形态短、不用转义、出问题时人眼能直接看懂「卡在哪个时间」。
// 老游标（`{` 开头的 JSON）继续认，否则已经在跑的工作流会当成「没有游标」重头来一遍。
type cursor struct {
	// T：本批最新条目的发布时间（RFC3339）。来源没给时间的，这里是空串，全靠 Seen 去重。
	T string `json:"t,omitempty"`
	// Seen：最近见过的条目 id 的短哈希。存哈希不存原文——有的源的 id 是一整条 URL。
	Seen []string `json:"seen,omitempty"`
}

func parseCursor(raw string) cursor {
	s := strings.TrimSpace(raw)
	if s == "" {
		return cursor{}
	}
	if strings.HasPrefix(s, "{") { // 旧形态：整份 JSON
		var c cursor
		if json.Unmarshal([]byte(s), &c) != nil {
			// 认不出的游标当成「没有游标」：宁可这一轮多推一点，也不要整条流卡死。
			return cursor{}
		}
		return c
	}
	t, seen, _ := strings.Cut(s, "|")
	c := cursor{T: strings.TrimSpace(t)}
	for _, h := range strings.Split(seen, ",") {
		if h = strings.TrimSpace(h); h != "" {
			c.Seen = append(c.Seen, h)
		}
	}
	return c
}

func (c cursor) dump() string {
	if c.T == "" && len(c.Seen) == 0 {
		return ""
	}
	return c.T + "|" + strings.Join(c.Seen, ",")
}

func (c cursor) hasSeen(id string) bool {
	h := shortHash(id)
	for _, s := range c.Seen {
		if s == h {
			return true
		}
	}
	return false
}

// filterNew：挑出比游标新的条目，**按时间正序**返回，并算出新游标。
//
// 首次拉取（游标为空）**不回溯全部历史**：只记下当前位置并返回最近一批——
// 接一个发了十年的源，工作流会被几千条瞬间冲垮（与 x/notion 事件源同一条判断）。
func filterNew(items []schema.Item, cur cursor, max int, firstRun bool) ([]schema.Item, cursor, bool) {
	sortByTime(items)

	kept := make([]schema.Item, 0, len(items))
	for _, it := range items {
		if cur.hasSeen(it.ID) {
			continue
		}
		// **只丢严格更老的**。时间戳相同的不能丢——快讯类源一秒好几条，
		// 同秒的第二条正是靠上面那行 hasSeen 去重，而不是靠时间戳。
		if cur.T != "" && it.PublishedAt != "" && olderThan(it.PublishedAt, cur.T) {
			continue
		}
		kept = append(kept, it)
	}

	hasMore := false
	if max > 0 && len(kept) > max {
		// 超量时**留最老的那批**：下一轮从游标继续，不会跳过中间。
		kept = kept[:max]
		hasMore = true
	}
	if firstRun && len(kept) > 0 {
		// 首次只给最近一批，避免十年历史一次性灌进来。
		if max > 0 && len(kept) > max {
			kept = kept[len(kept)-max:]
		}
		hasMore = false
	}

	next := cur
	for _, it := range kept {
		if it.PublishedAt != "" && newerThan(it.PublishedAt, next.T) {
			next.T = it.PublishedAt
		}
		next.Seen = append(next.Seen, shortHash(it.ID))
	}
	if len(next.Seen) > seenWindow {
		next.Seen = next.Seen[len(next.Seen)-seenWindow:]
	}
	return kept, next, hasMore
}

// sortByTime：按发布时间正序。没有时间的排在最后并保持原序——
// 多数源本来就是倒序给的，原序翻过来就是正序。
func sortByTime(items []schema.Item) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i].PublishedAt, items[j].PublishedAt
		if a == "" || b == "" {
			return b == "" && a != ""
		}
		return a < b
	})
}

// olderThan：a 严格早于 b。
func olderThan(a, b string) bool {
	if b == "" {
		return false
	}
	ta, ea := time.Parse(time.RFC3339, a)
	tb, eb := time.Parse(time.RFC3339, b)
	if ea == nil && eb == nil {
		return ta.Before(tb)
	}
	return a < b
}

func newerThan(a, b string) bool {
	if b == "" {
		return true
	}
	ta, ea := time.Parse(time.RFC3339, a)
	tb, eb := time.Parse(time.RFC3339, b)
	if ea == nil && eb == nil {
		return ta.After(tb)
	}
	return a > b // 解不开就按字符串比——RFC3339 的字典序与时间序一致
}

func shortHash(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:4])
}
