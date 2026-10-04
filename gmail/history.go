package main

// 新邮件的增量发现。
//
// 用 users.history.list（**不是**反复 list 最近邮件）：history 是 Gmail 的变更日志，
// 从上次的 historyId 往后拉，天然「不重不漏」。而按时间窗轮询 messages.list 有两个死角：
// 轮询间隔内到达多封会被上一次的窗口截断，而窗口开大又会重复推送。
//
// 三件必须处理的事（每件都有对应用例）：
//
//  1. **首次启动不能把历史邮件全推一遍**。没有游标时先取当前 historyId 作为起点，
//     只推此后到达的——否则接上一个用了三年的邮箱，工作流会被几万封信瞬间冲垮。
//  2. **同一封信会在一次拉取里出现多次**。history 是变更流，一封信的"到达"与随后的
//     "打标签"是两条记录，都带着同一个 message。按 id 去重。
//  3. **游标要存回凭证**。只放内存的话，插件一重启就回到第 1 条的处境
//     （要么重推、要么漏掉停机期间的信）。

import (
	"sort"
	"strconv"
)

// historyResponse：users.history.list 的应答（只取用得上的）。
type historyResponse struct {
	History       []historyRecord `json:"history"`
	NextPageToken string          `json:"nextPageToken"`
	HistoryID     string          `json:"historyId"`
}

type historyRecord struct {
	ID            string           `json:"id"`
	MessagesAdded []historyMsgItem `json:"messagesAdded"`
}

type historyMsgItem struct {
	Message gmailMessage `json:"message"`
}

// newMessageIDs：从一批 history 记录里理出「新到达的邮件 id」，去重且保持顺序。
//
// 只看 messagesAdded：history 里还有 labelsAdded/labelsRemoved/messagesDeleted，
// 那些是「已有邮件的状态变了」，不是新邮件。把它们也算进来的话，
// 你每标一封已读，工作流就被触发一次。
func newMessageIDs(recs []historyRecord) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, r := range recs {
		for _, m := range r.MessagesAdded {
			id := m.Message.ID
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// maxHistoryID：这批记录里最大的 historyId，作为下次的游标。
//
// **取最大而不是取最后一条**：应答里的顺序不保证严格递增（分页拼接后尤其如此），
// 拿最后一条当游标，一旦它不是最大的，中间那些就会被再推一遍。
// historyId 是 uint64 且会超过 int32，按数值比而不是按字符串比——
// 字符串比会让 "9999999" 大于 "10000000"，游标直接倒退。
func maxHistoryID(recs []historyRecord, fallback string) string {
	best := parseHistoryID(fallback)
	for _, r := range recs {
		if v := parseHistoryID(r.ID); v > best {
			best = v
		}
	}
	if best == 0 {
		return fallback
	}
	return strconv.FormatUint(best, 10)
}

func parseHistoryID(s string) uint64 {
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// sortMessageIDs：邮件 id 是十六进制的时间序（越新越大），按它排能让推送顺序接近收信顺序。
// 仅影响观感，不影响正确性——所以解析不了就保持原序，不要为了排序丢掉任何一条。
func sortMessageIDs(ids []string) []string {
	out := append([]string{}, ids...)
	sort.SliceStable(out, func(i, j int) bool {
		a, errA := strconv.ParseUint(out[i], 16, 64)
		b, errB := strconv.ParseUint(out[j], 16, 64)
		if errA != nil || errB != nil {
			return false // 解析不了就当"不小于"，SliceStable 会保持原序
		}
		return a < b
	})
	return out
}
