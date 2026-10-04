package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// history is a **change stream**, not a new-messages list. A message's "arrival" and its
// subsequent "label applied" are two separate records, both carrying the same message —
// without deduping, the same message gets pushed to the workflow twice.
func TestNewMessageIDsDedupes(t *testing.T) {
	recs := []historyRecord{
		{ID: "100", MessagesAdded: []historyMsgItem{{Message: gmailMessage{ID: "m1"}}}},
		{ID: "101", MessagesAdded: []historyMsgItem{{Message: gmailMessage{ID: "m1"}}}}, // the same message shows up again
		{ID: "102", MessagesAdded: []historyMsgItem{{Message: gmailMessage{ID: "m2"}}}},
	}
	got := newMessageIDs(recs)
	if len(got) != 2 || got[0] != "m1" || got[1] != "m2" {
		t.Fatalf("去重失败: %v", got)
	}
}

// Only messagesAdded is recognized: labelsAdded/messagesDeleted are "an existing message's
// state changed," not a new message. Counting them in would trigger the workflow every time
// you mark a message as read.
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

// The cursor takes the **max**, not the last entry: the response's order isn't guaranteed to be
// strictly increasing (especially after paginated results are concatenated); using the last
// entry as the cursor means anything in between gets re-pushed the moment it isn't actually the max.
func TestMaxHistoryIDTakesMaxNotLast(t *testing.T) {
	recs := []historyRecord{{ID: "500"}, {ID: "900"}, {ID: "700"}}
	if got := maxHistoryID(recs, "100"); got != "900" {
		t.Errorf("应取最大值 900, got %s —— 取最后一条会导致重复推送", got)
	}
}

// historyId is a uint64 and can exceed int32. **Compare numerically, not as strings**: a string
// comparison would make "9999999" > "10000000", pushing the cursor backward and causing a large
// batch to be re-pushed.
func TestMaxHistoryIDComparesNumerically(t *testing.T) {
	recs := []historyRecord{{ID: "9999999"}, {ID: "10000000"}}
	if got := maxHistoryID(recs, "0"); got != "10000000" {
		t.Errorf("按数值比应得 10000000, got %s —— 按字符串比会让游标倒退", got)
	}
	// a magnitude genuinely beyond int32
	recs = []historyRecord{{ID: "2147483647"}, {ID: "4294967296"}}
	if got := maxHistoryID(recs, "0"); got != "4294967296" {
		t.Errorf("大于 int32 的 historyId 处理错: %s", got)
	}
}

// With no new records, the cursor must **stay unchanged** and not be reset to zero — resetting
// would make the next fetch start from scratch and re-push the entire mailbox.
func TestMaxHistoryIDKeepsCursorWhenEmpty(t *testing.T) {
	if got := maxHistoryID(nil, "12345"); got != "12345" {
		t.Errorf("空批次应保持原游标, got %s", got)
	}
	if got := maxHistoryID([]historyRecord{{ID: "bad"}}, "12345"); got != "12345" {
		t.Errorf("解析不了的 id 不该顶掉游标, got %s", got)
	}
}

// Sorting only affects appearance: an unparseable id must not be dropped.
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
	// parseable ones follow time order (ids are a hex time-ordering, newer is larger)
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

// A regression test against the real response shape (a wrong field name fails silently: it
// always decodes to empty, which looks like "no new messages").
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
