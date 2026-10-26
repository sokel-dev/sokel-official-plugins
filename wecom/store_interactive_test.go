package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// JSON fixtures deliberately also compile against the previous persisted model.
func interactiveStoreFixture(t *testing.T, id string) Alert {
	t.Helper()
	a := testAlert(id)
	if err := json.Unmarshal([]byte(`{"Severity":"","Buttons":[{"id":"approve_release","text":"Approve release","value":{"deployment":{"id":"release-17","targets":["production",{"region":"east"}]}}},{"id":"request_changes","text":"Request changes","value":{"reason":"needs_review"}}],"Context":{"ticket":{"id":"business-42","items":[{"count":3}]}},"ButtonText":"Review request"}`), &a); err != nil {
		t.Fatal(err)
	}
	return a
}

func createInteractiveStoreFixture(t *testing.T, s *Store, id string) Alert {
	t.Helper()
	a, _, err := s.CreateAlert("ns", interactiveStoreFixture(t, id))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetDelivery("ns", id, Delivery{MsgID: "upstream-" + id}); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestStoreInteractiveCustomActionAndContinuation(t *testing.T) {
	s := openTestStore(t)
	createInteractiveStoreFixture(t, s, "a")
	ticket := testTicket(t, s, "ns", "a", "alice")
	first, err := s.ApplyAction("ns", "a", ticket, "approve_release")
	if err != nil {
		t.Fatalf("configured business action must be accepted: %v", err)
	}
	if first.Alert.Revision != 1 || first.Ticket == "" || first.EventID == "" {
		t.Fatalf("missing persisted action or continuation: %#v", first)
	}
	second, err := s.ApplyAction("ns", "a", first.Ticket, "request_changes")
	if err != nil || second.Alert.Revision != 2 || second.EventID == first.EventID {
		t.Fatalf("a later action must not be blocked by an alert state machine: %#v %v", second, err)
	}
}

func TestStoreInteractiveLegacyRecordsRemainReadableWithoutActions(t *testing.T) {
	s := openTestStore(t)
	a := testAlert("legacy")
	if err := json.Unmarshal([]byte(`{"Buttons":null}`), &a); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreateAlert("ns", a); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDelivery("ns", a.ID, Delivery{MsgID: "legacy-message"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetAlert("ns", a.ID); err != nil {
		t.Fatal(err)
	}
	_, err := s.ApplyAction("ns", a.ID, testTicket(t, s, "ns", a.ID, "alice"), "confirm")
	assertStoreStatus(t, err, 400)
}

func TestStoreInteractiveDedupIncludesConfiguredActionsAndData(t *testing.T) {
	s := openTestStore(t)
	original := interactiveStoreFixture(t, "a")
	if _, _, err := s.CreateAlert("ns", original); err != nil {
		t.Fatal(err)
	}
	for _, patch := range []string{
		`{"Buttons":[{"id":"approve_release","text":"Changed label"}]}`,
		`{"Context":{"ticket":"different"}}`,
		`{"ButtonText":"Different entry"}`,
	} {
		changed := interactiveStoreFixture(t, "a")
		if err := json.Unmarshal([]byte(patch), &changed); err != nil {
			t.Fatal(err)
		}
		_, _, err := s.CreateAlert("ns", changed)
		assertStoreStatus(t, err, 409)
	}
}

func TestStoreInteractiveCopiesAllNestedInputAndOutputData(t *testing.T) {
	s := openTestStore(t)
	input := interactiveStoreFixture(t, "a")
	created, _, err := s.CreateAlert("ns", input)
	if err != nil {
		t.Fatal(err)
	}
	mutate := func(a *Alert) {
		a.Buttons[0].Value["deployment"].(map[string]any)["targets"].([]any)[1].(map[string]any)["region"] = "tampered"
		a.Context["ticket"].(map[string]any)["items"].([]any)[0].(map[string]any)["count"] = 999
		a.Buttons[0].Text = "Wrong label"
	}
	mutate(&input)
	mutate(&created)
	queried, err := s.GetAlert("ns", "a")
	if err != nil {
		t.Fatal(err)
	}
	mutate(&queried)
	if err := s.SetDelivery("ns", "a", Delivery{MsgID: "upstream-a"}); err != nil {
		t.Fatal(err)
	}
	ticket := testTicket(t, s, "ns", "a", "alice")
	result, err := s.ApplyAction("ns", "a", ticket, "approve_release")
	if err != nil {
		t.Fatal(err)
	}
	mutate(&result.Alert)
	events := s.PendingEvents("ns")
	if len(events) != 1 {
		t.Fatal("expected one action event")
	}
	events[0].Value["deployment"].(map[string]any)["targets"].([]any)[1].(map[string]any)["region"] = "tampered-event"
	events[0].Context["ticket"].(map[string]any)["items"].([]any)[0].(map[string]any)["count"] = 999
	event := s.PendingEvents("ns")[0]
	if event.MsgID != "upstream-a" || event.ActionText != "Approve release" || event.Action != "approve_release" {
		t.Fatalf("event did not use configured server values: %#v", event)
	}
	assertInteractiveJSON(t, event.Value, `{"deployment":{"id":"release-17","targets":["production",{"region":"east"}]}}`)
	assertInteractiveJSON(t, event.Context, `{"ticket":{"id":"business-42","items":[{"count":3}]}}`)
	replay, err := s.ApplyAction("ns", "a", ticket, "approve_release")
	if err != nil {
		t.Fatal(err)
	}
	if replay.Alert.Buttons[0].Text != "Approve release" {
		t.Fatal("caller changed the durable replay snapshot")
	}
	assertInteractiveJSON(t, replay.Alert.Context, `{"ticket":{"id":"business-42","items":[{"count":3}]}}`)
}

func assertInteractiveJSON(t *testing.T, got any, want string) {
	t.Helper()
	b, err := json.Marshal(got)
	if err != nil || string(b) != want {
		t.Fatalf("JSON mismatch: got %s, want %s, error %v", b, want, err)
	}
}

func TestStoreInteractiveReplayContinuesWithoutRestoringOldRevision(t *testing.T) {
	s := openTestStore(t)
	createInteractiveStoreFixture(t, s, "a")
	old := testTicket(t, s, "ns", "a", "alice")
	first, err := s.ApplyAction("ns", "a", old, "approve_release")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApplyAction("ns", "a", testTicket(t, s, "ns", "a", "bob"), "request_changes"); err != nil {
		t.Fatal(err)
	}
	replay, err := s.ApplyAction("ns", "a", old, "approve_release")
	if err != nil || !replay.Replayed || replay.EventID != first.EventID || replay.Ticket == "" || replay.Ticket == first.Ticket || replay.Alert.Revision != 1 {
		t.Fatalf("lost response cannot be resumed safely: %#v %v", replay, err)
	}
	current, err := s.GetAlert("ns", "a")
	if err != nil || current.Revision != 2 || current.LastActionID != "request_changes" || current.ActorUserID != "bob" {
		t.Fatalf("replay overwrote current state: %#v %v", current, err)
	}
	continued, err := s.ApplyAction("ns", "a", replay.Ticket, "request_changes")
	if err != nil || continued.Alert.Revision != 3 || continued.UserID != "alice" || len(s.PendingEvents("ns")) != 3 {
		t.Fatalf("reissued ticket failed to continue current state: %#v %v", continued, err)
	}
}

func TestStoreInteractiveRejectsUnknownActionWithoutConsumingTicket(t *testing.T) {
	s := openTestStore(t)
	createInteractiveStoreFixture(t, s, "a")
	ticket := testTicket(t, s, "ns", "a", "alice")
	for _, action := range []string{"", "claim", "APPROVE_RELEASE", "unconfigured"} {
		_, err := s.ApplyAction("ns", "a", ticket, action)
		assertStoreStatus(t, err, 400)
	}
	result, err := s.ApplyAction("ns", "a", ticket, "approve_release")
	if err != nil || result.Replayed || len(s.PendingEvents("ns")) != 1 {
		t.Fatalf("invalid action consumed a valid ticket: %#v %v", result, err)
	}
	for _, action := range []string{"request_changes", "unconfigured"} {
		_, err := s.ApplyAction("ns", "a", ticket, action)
		assertStoreStatus(t, err, 409)
	}
}

func TestStoreInteractiveConcurrentRetriesProduceOneEvent(t *testing.T) {
	s := openTestStore(t)
	createInteractiveStoreFixture(t, s, "a")
	ticket := testTicket(t, s, "ns", "a", "alice")
	const count = 8
	var wg sync.WaitGroup
	results := make([]ActionResult, count)
	errs := make([]error, count)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = s.ApplyAction("ns", "a", ticket, "approve_release")
		}(i)
	}
	wg.Wait()
	newEvents := 0
	continuations := map[string]bool{}
	for i, result := range results {
		if errs[i] != nil || result.EventID != results[0].EventID || result.Alert.Revision != 1 || result.Ticket == "" || continuations[result.Ticket] {
			t.Fatalf("unsafe concurrent replay: %#v %v", result, errs[i])
		}
		continuations[result.Ticket] = true
		if !result.Replayed {
			newEvents++
		}
	}
	if newEvents != 1 || len(s.PendingEvents("ns")) != 1 {
		t.Fatal("concurrent retries produced duplicate events")
	}
}

func TestStoreInteractiveRestartPreservesJSONNumbersAndDedup(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	a := interactiveStoreFixture(t, "a")
	a.Context = map[string]any{"large_id": json.Number("9007199254740993"), "nested": []map[string]any{{"count": 3}}}
	if _, _, err := s.CreateAlert("ns", a); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDelivery("ns", "a", Delivery{MsgID: "m"}); err != nil {
		t.Fatal(err)
	}
	result, err := s.ApplyAction("ns", "a", testTicket(t, s, "ns", "a", "alice"), "approve_release")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	again, reused, err := s.CreateAlert("ns", a)
	if err != nil || !reused || again.ID != a.ID || again.Revision != 1 {
		t.Fatalf("JSON normalization broke restart dedup: %#v %v %v", again, reused, err)
	}
	assertInteractiveJSON(t, again.Context, `{"large_id":9007199254740993,"nested":[{"count":3}]}`)
	events := s.PendingEvents("ns")
	if len(events) != 1 || events[0].ID != result.EventID {
		t.Fatal("restart lost pending interaction event")
	}
	assertInteractiveJSON(t, events[0].Context, `{"large_id":9007199254740993,"nested":[{"count":3}]}`)
}

func TestStoreInteractiveOptionalRequestIDAndJSONValidation(t *testing.T) {
	s := openTestStore(t)
	for _, id := range []string{"a", "b"} {
		a := interactiveStoreFixture(t, id)
		a.ExternalID = ""
		got, reused, err := s.CreateAlert("ns", a)
		if err != nil || reused || got.ID != id {
			t.Fatalf("missing request ID must not deduplicate separate messages: %#v %v %v", got, reused, err)
		}
	}
	for _, mutate := range []func(*Alert){
		func(a *Alert) { a.Context["unsupported"] = make(chan string) },
		func(a *Alert) { a.Context["cycle"] = a.Context },
		func(a *Alert) { a.Buttons[1].ID = a.Buttons[0].ID },
		func(a *Alert) { a.Buttons[0].Text = " " },
	} {
		a := interactiveStoreFixture(t, "invalid")
		mutate(&a)
		_, _, err := s.CreateAlert("ns", a)
		assertStoreStatus(t, err, 400)
	}
}

func TestStoreInteractiveTicketCapacityIsAtomicForActionAndReplay(t *testing.T) {
	s := openTestStore(t)
	createInteractiveStoreFixture(t, s, "a")
	ticket := testTicket(t, s, "ns", "a", "alice")
	fill := func() {
		for i := len(s.data.Tickets); i < maxAuthRecords; i++ {
			s.data.Tickets[fmt.Sprintf("fixture-%d", i)] = ticketRecord{ExpiresAt: formatTime(time.Now().Add(time.Hour))}
		}
	}
	fill()
	_, err := s.ApplyAction("ns", "a", ticket, "approve_release")
	assertStoreStatus(t, err, 409)
	if len(s.PendingEvents("ns")) != 0 || s.data.Tickets[tokenHash(ticket)].Action != "" || s.data.Alerts[recordKey("ns", "a")].Alert.Revision != 0 {
		t.Fatal("capacity failure partially applied action")
	}
	delete(s.data.Tickets, "fixture-19999")
	first, err := s.ApplyAction("ns", "a", ticket, "approve_release")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ApplyAction("ns", "a", ticket, "approve_release")
	assertStoreStatus(t, err, 409)
	if len(s.PendingEvents("ns")) != 1 || s.PendingEvents("ns")[0].ID != first.EventID {
		t.Fatal("replay capacity failure changed durable event")
	}
}

func TestStoreInteractiveLegacySnapshotAndPendingEventsRemainReadable(t *testing.T) {
	dir := t.TempDir()
	legacy := map[string]any{
		"Version": 1,
		"Alerts":  map[string]any{recordKey("ns", "old"): map[string]any{"Namespace": "ns", "Alert": map[string]any{"ID": "old", "ExternalID": "legacy-request", "Severity": "critical", "Status": "claimed", "Revision": 1, "ActorUserID": "alice", "Users": []string{"alice"}, "Sent": true, "Delivery": Delivery{MsgID: "old-message"}, "ExpiresAt": formatTime(time.Now().Add(time.Hour))}}},
		"Logins":  map[string]any{}, "Tickets": map[string]any{},
		"Events":     map[string]any{"pending": map[string]any{"ID": "pending", "Namespace": "ns", "AlertID": "old", "ExternalID": "legacy-request", "Action": "claim", "Status": "claimed", "Revision": 1}},
		"EventOrder": []string{"pending"},
	}
	b, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "store.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, err := s.GetAlert("ns", "old")
	if err != nil || a.Status != "claimed" || a.Severity != "critical" || a.Revision != 1 || len(a.Buttons) != 0 {
		t.Fatalf("legacy data changed during load: %#v %v", a, err)
	}
	_, err = s.ApplyAction("ns", "old", testTicket(t, s, "ns", "old", "alice"), "claim")
	assertStoreStatus(t, err, 400)
	createInteractiveStoreFixture(t, s, "new")
	pending := s.PendingEvents("ns")
	if len(pending) != 1 || pending[0].ID != "pending" || pending[0].Action != "claim" || pending[0].Status != "claimed" {
		t.Fatalf("legacy pending event was dropped or reinterpreted: %#v", pending)
	}
}

func TestStoreInteractiveReplayWriteFailureDoesNotLeakTicket(t *testing.T) {
	s := openTestStore(t)
	createInteractiveStoreFixture(t, s, "a")
	ticket := testTicket(t, s, "ns", "a", "alice")
	first, err := s.ApplyAction("ns", "a", ticket, "approve_release")
	if err != nil {
		t.Fatal(err)
	}
	before := len(s.data.Tickets)
	path := filepath.Join(s.dir, "store.json")
	if err := os.Rename(path, path+".backup"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	_, err = s.ApplyAction("ns", "a", ticket, "approve_release")
	assertStoreStatus(t, err, 500)
	if len(s.data.Tickets) != before || len(s.PendingEvents("ns")) != 1 {
		t.Fatal("failed replay write changed ticket or outbox state")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".backup", path); err != nil {
		t.Fatal(err)
	}
	replay, err := s.ApplyAction("ns", "a", ticket, "approve_release")
	if err != nil || !replay.Replayed || replay.EventID != first.EventID || replay.Ticket == "" {
		t.Fatalf("replay did not recover after storage repair: %#v %v", replay, err)
	}
	b, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(b), replay.Ticket) {
		t.Fatal("raw reissued ticket leaked into persisted snapshot")
	}
}
