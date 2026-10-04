package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// history 是**变更流**，不是新邮件列表。一封信的「到达」与随后的「打标签」是两条记录，
// 都驮着同一个 message——不去重就会把同一封信推给工作流两次。
func TestNewMessageIDsDedupes(t *testing.T) {
	recs := []historyRecord{
		{ID: "100", MessagesAdded: []historyMsgItem{{Message: gmailMessage{ID: "m1"}}}},
		{ID: "101", MessagesAdded: []historyMsgItem{{Message: gmailMessage{ID: "m1"}}}}, // 同一封又出现
		{ID: "102", MessagesAdded: []historyMsgItem{{Message: gmailMessage{ID: "m2"}}}},
	}
	got := newMessageIDs(recs)
	if len(got) != 2 || got[0] != "m1" || got[1] != "m2" {
		t.Fatalf("去重失败: %v", got)
	}
}

// 只认 messagesAdded：labelsAdded/messagesDeleted 是「已有邮件状态变了」，不是新邮件。
// 算进来的话，你每标一封已读，工作流就被触发一次。
func TestOnlyMessagesAddedCounts(t *testing.T) {
	raw := `[
	  {"id":"1","labelsAdded":[{"message":{"id":"old1"}}]},
	  {"id":"2","messagesDeleted":[{"message":{"id":"old2"}}]},
	  {"id":"3","messagesAdded":[{"message":{"id":"new1"}}]}
	]`
	var recs []historyRecord
	if err := json.Unmarshal([]byte(raw), &recs); err != nil {
		t.Fatal(err)
	}
	got := newMessageIDs(recs)
	if len(got) != 1 || got[0] != "new1" {
		t.Errorf("只有新到达的才算，got %v —— 标已读也触发工作流是灾难", got)
	}
}

// 游标取**最大**而不是最后一条：应答顺序不保证严格递增（分页拼接后尤其），
// 拿最后一条当游标，一旦它不是最大的，中间那些会被再推一遍。
func TestMaxHistoryIDTakesMaxNotLast(t *testing.T) {
	recs := []historyRecord{{ID: "500"}, {ID: "900"}, {ID: "700"}}
	if got := maxHistoryID(recs, "100"); got != "900" {
		t.Errorf("应取最大值 900, got %s —— 取最后一条会导致重复推送", got)
	}
}

// historyId 是 uint64，会超过 int32。**按数值比而不是按字符串比**：
// 字符串比会让 "9999999" > "10000000"，游标直接倒退，然后重推一大批。
func TestMaxHistoryIDComparesNumerically(t *testing.T) {
	recs := []historyRecord{{ID: "9999999"}, {ID: "10000000"}}
	if got := maxHistoryID(recs, "0"); got != "10000000" {
		t.Errorf("按数值比应得 10000000, got %s —— 按字符串比会让游标倒退", got)
	}
	// 超过 int32 的真实量级
	recs = []historyRecord{{ID: "2147483647"}, {ID: "4294967296"}}
	if got := maxHistoryID(recs, "0"); got != "4294967296" {
		t.Errorf("大于 int32 的 historyId 处理错: %s", got)
	}
}

// 没有新记录时游标必须**保持不动**，不能清零——清零下次就从头拉，把整个邮箱重推一遍。
func TestMaxHistoryIDKeepsCursorWhenEmpty(t *testing.T) {
	if got := maxHistoryID(nil, "12345"); got != "12345" {
		t.Errorf("空批次应保持原游标, got %s", got)
	}
	if got := maxHistoryID([]historyRecord{{ID: "bad"}}, "12345"); got != "12345" {
		t.Errorf("解析不了的 id 不该顶掉游标, got %s", got)
	}
}

// 排序只影响观感：解析不了的 id 不能被丢掉。
func TestSortMessageIDsNeverDropsAny(t *testing.T) {
	in := []string{"18f2a", "18f01", "不是十六进制", "18f99"}
	got := sortMessageIDs(in)
	if len(got) != len(in) {
		t.Fatalf("排序丢了条目: %v", got)
	}
	joined := strings.Join(got, ",")
	for _, id := range in {
		if !strings.Contains(joined, id) {
			t.Errorf("丢了 %s: %v", id, got)
		}
	}
	// 能解析的按时间序（id 是十六进制时间序，越新越大）
	a, b := indexOf(got, "18f01"), indexOf(got, "18f2a")
	if a > b {
		t.Errorf("可解析的应按数值升序: %v", got)
	}
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

// 真实应答形状回归（字段名写错是静默失效：解出来永远是空，看起来像"没有新邮件"）。
func TestParseHistoryResponse(t *testing.T) {
	raw := `{"history":[
	  {"id":"11111","messagesAdded":[{"message":{"id":"18f","threadId":"18t","labelIds":["INBOX","UNREAD"]}}]}
	],"historyId":"22222","nextPageToken":"tok"}`
	var r historyResponse
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.History) != 1 || r.HistoryID != "22222" || r.NextPageToken != "tok" {
		t.Fatalf("应答没解出来: %+v", r)
	}
	ids := newMessageIDs(r.History)
	if len(ids) != 1 || ids[0] != "18f" {
		t.Errorf("新邮件 id: %v", ids)
	}
	if got := r.History[0].MessagesAdded[0].Message.ThreadID; got != "18t" {
		t.Errorf("嵌套 message 字段没解出: %q", got)
	}
}
