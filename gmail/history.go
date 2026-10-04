package main

// Incremental discovery of new messages.
//
// Uses users.history.list (**not** repeatedly listing recent messages): history is Gmail's
// change log, and pulling forward from the last historyId naturally avoids duplicates and gaps.
// Polling messages.list over a time window has two blind spots instead: multiple arrivals
// within a polling interval get cut off by the previous window, while widening the window
// causes repeated deliveries.
//
// Three things must be handled (each has a corresponding test case):
//
//  1. **The first run must not push the entire history at once.** Without a cursor, first fetch
//     the current historyId as the starting point and only push what arrives afterward —
//     otherwise connecting a mailbox that's been in use for three years would flood the
//     workflow with tens of thousands of messages instantly.
//  2. **The same message can show up multiple times in one fetch.** history is a change
//     stream, and a message's "arrival" and its subsequent "label applied" are two separate
//     records, both carrying the same message. Dedup by id.
//  3. **The cursor must be persisted back to the credential.** Keeping it only in memory would
//     put the plugin back in the situation of point 1 on every restart (either re-pushing
//     everything or missing messages that arrived while it was down).

import (
	"sort"
	"strconv"
)

// historyResponse is the users.history.list response (only the fields we use).
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

// newMessageIDs extracts "newly arrived message ids" from a batch of history records,
// deduplicated and order-preserving.
//
// Only messagesAdded is looked at: history also has labelsAdded/labelsRemoved/messagesDeleted,
// which are "an existing message's state changed," not a new message. Counting those too would
// trigger the workflow every time you mark a message as read.
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

// maxHistoryID returns the largest historyId in this batch of records, used as the next cursor.
//
// **Take the max, not the last entry**: the response's order isn't guaranteed to be strictly
// increasing (especially after paginated results are concatenated); using the last entry as the
// cursor means anything in between gets re-pushed the moment it isn't actually the max.
// historyId is a uint64 and can exceed int32, so compare numerically, not as strings —
// a string comparison would make "9999999" greater than "10000000", pushing the cursor backward.
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

// sortMessageIDs: message ids are a hex time-ordering (newer is larger), so sorting by them
// makes delivery order roughly match receipt order. This only affects appearance, not
// correctness — so if parsing fails, keep the original order rather than dropping anything for
// the sake of sorting.
func sortMessageIDs(ids []string) []string {
	out := append([]string{}, ids...)
	sort.SliceStable(out, func(i, j int) bool {
		a, errA := strconv.ParseUint(out[i], 16, 64)
		b, errB := strconv.ParseUint(out[j], 16, 64)
		if errA != nil || errB != nil {
			return false // treat unparseable as "not less than"; SliceStable keeps the original order
		}
		return a < b
	})
	return out
}
