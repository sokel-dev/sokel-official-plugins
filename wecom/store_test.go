package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func testAlert(id string) Alert {
	return Alert{ID: id, ExternalID: "external-" + id, Title: "Release review", Description: "Review the deployment details", Buttons: []InteractiveButton{{ID: "view_details", Text: "View details"}, {ID: "acknowledge", Text: "Acknowledge"}, {ID: "request_help", Text: "Request help"}}, Users: []string{"alice", "bob"}, CreatedAt: formatTime(time.Now()), ExpiresAt: formatTime(time.Now().Add(time.Hour)), PortalURL: "https://alerts.example.test"}
}
func createSentAlert(t *testing.T, s *Store, ns, id string) Alert {
	t.Helper()
	a, _, err := s.CreateAlert(ns, testAlert(id))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetDelivery(ns, id, Delivery{MsgID: "message-" + id}); err != nil {
		t.Fatal(err)
	}
	a, err = s.GetAlert(ns, id)
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func testTicket(t *testing.T, s *Store, ns, id, user string) string {
	t.Helper()
	v, _, err := s.NewTicket(ns, id, user)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func assertStoreStatus(t *testing.T, err error, status int) {
	t.Helper()
	var se *StoreError
	if !errors.As(err, &se) || se.Status != status {
		t.Fatalf("want status %d, got %v", status, err)
	}
}

func TestStoreRestartPersistsActionsAndOutbox(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	createSentAlert(t, s, "tenant-a", "a")
	ticket := testTicket(t, s, "tenant-a", "a", "alice")
	result, err := s.ApplyAction("tenant-a", "a", ticket, "view_details")
	if err != nil {
		t.Fatal(err)
	}
	if result.Ticket == "" {
		t.Fatal("each action must return a continuation ticket")
	}
	state, _ := randomID()
	if err = s.StartLogin("tenant-a", "a", state); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, err := s.GetAlert("tenant-a", "a")
	if err != nil || a.LastActionID != "view_details" || a.Revision != 1 || a.ActorUserID != "alice" {
		t.Fatalf("lost state: %#v %v", a, err)
	}
	if err = s.ConsumeLogin("tenant-a", "a", state); err != nil {
		t.Fatal(err)
	}
	if events := s.PendingEvents("tenant-a"); len(events) != 1 || events[0].ID != result.EventID {
		t.Fatalf("lost outbox: %#v", events)
	}
	replay, err := s.ApplyAction("tenant-a", "a", ticket, "view_details")
	if err != nil || !replay.Replayed || replay.EventID != result.EventID || replay.Ticket == "" {
		t.Fatalf("bad replay: %#v %v", replay, err)
	}
	if err = s.MarkDispatched("tenant-a", result.EventID); err != nil {
		t.Fatal(err)
	}
	if len(s.PendingEvents("tenant-a")) != 0 {
		t.Fatal("dispatched event still pending")
	}
	acknowledged, err := s.ApplyAction("tenant-a", "a", result.Ticket, "acknowledge")
	if err != nil || acknowledged.Alert.LastActionID != "acknowledge" || acknowledged.Alert.Revision != 2 {
		t.Fatalf("continuation failed: %#v %v", acknowledged, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if events := s.PendingEvents("tenant-a"); len(events) != 1 || events[0].Action != "acknowledge" {
		t.Fatalf("unexpected pending events: %#v", events)
	}
	data, err := os.ReadFile(filepath.Join(dir, "store.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{state, ticket, result.Ticket, replay.Ticket} {
		if strings.Contains(string(data), secret) {
			t.Fatal("raw authentication token persisted")
		}
	}
	for _, name := range []string{"store.json", "store.lock"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("unsafe file permissions: %s %v", name, err)
		}
	}
	info, err := os.Stat(dir)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("unsafe directory permissions")
	}
}

func TestStoreConcurrentActionsPreserveEveryActor(t *testing.T) {
	s := openTestStore(t)
	createSentAlert(t, s, "ns", "a")
	tickets := []string{testTicket(t, s, "ns", "a", "alice"), testTicket(t, s, "ns", "a", "bob")}
	var wg sync.WaitGroup
	errs := make([]error, len(tickets))
	results := make([]ActionResult, len(tickets))
	start := make(chan struct{})
	for i := range tickets {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = s.ApplyAction("ns", "a", tickets[i], "view_details")
		}(i)
	}
	close(start)
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatalf("independent recipients may interact concurrently: %v", err)
		}
	}
	events := s.PendingEvents("ns")
	if len(events) != 2 || events[0].Revision != 1 || events[1].Revision != 2 || events[0].ActorUserID == events[1].ActorUserID {
		t.Fatalf("concurrent actions lost attribution or ordering: %#v", events)
	}
	for i, result := range results {
		if result.Ticket == "" || result.UserID != []string{"alice", "bob"}[i] {
			t.Fatalf("continuation lost recipient identity: %#v", result)
		}
		_, err := s.ApplyAction("ns", "a", tickets[i], "acknowledge")
		assertStoreStatus(t, err, 409)
	}
}

func TestStoreAuthorizationAndNamespaceIsolation(t *testing.T) {
	s := openTestStore(t)
	createSentAlert(t, s, "one", "a")
	createSentAlert(t, s, "two", "a")
	createSentAlert(t, s, "one", "b")
	_, _, err := s.NewTicket("one", "a", "mallory")
	assertStoreStatus(t, err, 403)
	ticket := testTicket(t, s, "one", "a", "Alice")
	_, err = s.ApplyAction("two", "a", ticket, "acknowledge")
	assertStoreStatus(t, err, 403)
	_, err = s.ApplyAction("one", "b", ticket, "acknowledge")
	assertStoreStatus(t, err, 403)
	_, err = s.GetAlert("other", "a")
	assertStoreStatus(t, err, 404)
	result, err := s.ApplyAction("one", "a", ticket, "acknowledge")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.PendingEvents("two")) != 0 {
		t.Fatal("event leaked to another namespace")
	}
	assertStoreStatus(t, s.MarkDispatched("two", result.EventID), 404)
	other, err := s.ApplyAction("one", "a", testTicket(t, s, "one", "a", "bob"), "request_help")
	if err != nil || other.UserID != "bob" || other.Alert.Revision != 2 {
		t.Fatalf("another authorized recipient must remain able to interact: %#v %v", other, err)
	}
	unsent := testAlert("unsent")
	if _, _, err = s.CreateAlert("one", unsent); err != nil {
		t.Fatal(err)
	}
	_, _, err = s.NewTicket("one", "unsent", "alice")
	assertStoreStatus(t, err, 403)
	state, _ := randomID()
	assertStoreStatus(t, s.StartLogin("one", "unsent", state), 409)
}

func TestStoreRejectsPartialDeliveryFailures(t *testing.T) {
	s := openTestStore(t)
	a := testAlert("a")
	a.Users = []string{"alice", "bob", "carol"}
	if _, _, err := s.CreateAlert("ns", a); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDelivery("ns", "a", Delivery{MsgID: "m", InvalidUser: "ALICE", UnlicensedUser: "Bob"}); err != nil {
		t.Fatal(err)
	}
	for _, user := range []string{"alice", "BOB"} {
		_, _, err := s.NewTicket("ns", "a", user)
		assertStoreStatus(t, err, 403)
	}
	ticket := testTicket(t, s, "ns", "a", "CAROL")
	if _, err := s.ApplyAction("ns", "a", ticket, "acknowledge"); err != nil {
		t.Fatal(err)
	}
}

func TestStoreLoginStateReplayExpiryAndLimits(t *testing.T) {
	s := openTestStore(t)
	createSentAlert(t, s, "one", "a")
	createSentAlert(t, s, "two", "a")
	state, _ := randomID()
	if err := s.StartLogin("one", "a", state); err != nil {
		t.Fatal(err)
	}
	assertStoreStatus(t, s.ConsumeLogin("two", "a", state), 403)
	if err := s.ConsumeLogin("one", "a", state); err != nil {
		t.Fatal(err)
	}
	assertStoreStatus(t, s.ConsumeLogin("one", "a", state), 403)
	assertStoreStatus(t, s.StartLogin("one", "a", "bad-state"), 400)
	for i := 0; i < 20; i++ {
		state, _ = randomID()
		if err := s.StartLogin("one", "a", state); err != nil {
			t.Fatal(err)
		}
	}
	extra, _ := randomID()
	assertStoreStatus(t, s.StartLogin("one", "a", extra), 409)
	future := time.Now().Add(6 * time.Minute)
	s.now = func() time.Time { return future }
	assertStoreStatus(t, s.ConsumeLogin("one", "a", state), 410)
	if err := s.StartLogin("one", "a", extra); err != nil {
		t.Fatalf("expired states not pruned: %v", err)
	}
}

func TestStoreTicketAndAlertExpiry(t *testing.T) {
	s := openTestStore(t)
	createSentAlert(t, s, "ns", "a")
	ticket := testTicket(t, s, "ns", "a", "alice")
	state, _ := randomID()
	if err := s.StartLogin("ns", "a", state); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	s.now = func() time.Time { return now.Add(6 * time.Minute) }
	_, err := s.ApplyAction("ns", "a", ticket, "acknowledge")
	assertStoreStatus(t, err, 410)
	s.now = func() time.Time { return now.Add(2 * time.Hour) }
	a, err := s.GetAlert("ns", "a")
	if err != nil || a.ID != "a" {
		t.Fatalf("expired retained alert is not queryable: %#v %v", a, err)
	}
	_, _, err = s.NewTicket("ns", "a", "alice")
	assertStoreStatus(t, err, 410)
	assertStoreStatus(t, s.StartLogin("ns", "a", state), 410)
	assertStoreStatus(t, s.ConsumeLogin("ns", "a", state), 410)
	_, err = s.ApplyAction("ns", "a", ticket, "acknowledge")
	assertStoreStatus(t, err, 410)
}

func TestStoreIdempotentActionsAndExternalIDs(t *testing.T) {
	s := openTestStore(t)
	original := createSentAlert(t, s, "ns", "a")
	repeat := original
	repeat.ID = "new-random-id"
	repeat.CreatedAt = formatTime(time.Now().Add(time.Minute))
	repeat.Users = []string{"BOB", "Alice"}
	a, reused, err := s.CreateAlert("ns", repeat)
	if err != nil || !reused || a.ID != "a" || !a.Sent {
		t.Fatalf("dedup failed: %#v %v %v", a, reused, err)
	}
	repeat.Title = "different"
	_, _, err = s.CreateAlert("ns", repeat)
	assertStoreStatus(t, err, 409)
	ticket := testTicket(t, s, "ns", "a", "alice")
	result, err := s.ApplyAction("ns", "a", ticket, "request_help")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		replay, err := s.ApplyAction("ns", "a", ticket, "request_help")
		if err != nil || !replay.Replayed || replay.EventID != result.EventID {
			t.Fatalf("retry failed: %#v %v", replay, err)
		}
	}
	_, err = s.ApplyAction("ns", "a", ticket, "acknowledge")
	assertStoreStatus(t, err, 409)
	if len(s.PendingEvents("ns")) != 1 {
		t.Fatal("retry generated duplicate events")
	}
	a.Users[0] = "intruder"
	got, err := s.GetAlert("ns", "a")
	if err != nil || got.Users[0] != "alice" {
		t.Fatal("caller mutated store through returned slice")
	}
}

func TestStoreSecondInstanceRejected(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := OpenStore(dir); err == nil {
		other.Close()
		t.Fatal("second store acquired lock")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	other, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	other.Close()
}

func TestStoreFailedWriteLeavesStateAndTicketUnchanged(t *testing.T) {
	s := openTestStore(t)
	createSentAlert(t, s, "ns", "a")
	ticket := testTicket(t, s, "ns", "a", "alice")
	path := filepath.Join(s.dir, "store.json")
	if err := os.Rename(path, path+".backup"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	_, err := s.ApplyAction("ns", "a", ticket, "acknowledge")
	assertStoreStatus(t, err, 500)
	a, err := s.GetAlert("ns", "a")
	if err != nil || a.Revision != 0 || a.LastActionID != "" || len(s.PendingEvents("ns")) != 0 {
		t.Fatal("failed write mutated memory")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".backup", path); err != nil {
		t.Fatal(err)
	}
	result, err := s.ApplyAction("ns", "a", ticket, "acknowledge")
	if err != nil || result.Replayed {
		t.Fatalf("failed write consumed ticket: %#v %v", result, err)
	}
}

func TestStoreCleanupKeepsPendingEvents(t *testing.T) {
	s := openTestStore(t)
	createSentAlert(t, s, "ns", "a")
	ticket := testTicket(t, s, "ns", "a", "alice")
	result, err := s.ApplyAction("ns", "a", ticket, "acknowledge")
	if err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(48 * time.Hour)
	s.now = func() time.Time { return future }
	for i := 0; i < 2; i++ {
		a := testAlert(fmt.Sprintf("new-%d", i))
		a.ExpiresAt = formatTime(future.Add(time.Hour))
		if _, _, err = s.CreateAlert("ns", a); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.PendingEvents("ns")) != 1 {
		t.Fatal("cleanup dropped pending event")
	}
	if err = s.MarkDispatched("ns", result.EventID); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.data.Alerts[recordKey("ns", "a")]; ok {
		t.Fatal("old completed alert was not pruned")
	}
}

func TestStoreOutboxPreservesActionOrderWithinSameSecond(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	s.now = func() time.Time { return now }
	createSentAlert(t, s, "ns", "a")
	first, err := s.ApplyAction("ns", "a", testTicket(t, s, "ns", "a", "alice"), "view_details")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ApplyAction("ns", "a", first.Ticket, "acknowledge"); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	events := s.PendingEvents("ns")
	if len(events) != 2 || events[0].Action != "view_details" || events[1].Action != "acknowledge" || events[0].Revision != 1 || events[1].Revision != 2 {
		t.Fatalf("incorrect action order: %#v", events)
	}
}

func TestStoreDirectorySyncFailureRequiresReopen(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	createSentAlert(t, s, "ns", "a")
	if err := s.Check(); err != nil {
		t.Fatalf("healthy store check failed: %v", err)
	}
	ticket := testTicket(t, s, "ns", "a", "alice")
	s.syncDir = func(*os.File) error { return errors.New("injected directory sync failure") }
	_, err = s.ApplyAction("ns", "a", ticket, "acknowledge")
	assertStoreStatus(t, err, 500)
	if !strings.Contains(err.Error(), "durability uncertain") {
		t.Fatalf("missing durability warning: %v", err)
	}
	assertStoreStatus(t, s.Check(), 500)
	if s.data.Alerts[recordKey("ns", "a")].Alert.LastActionID != "acknowledge" {
		t.Fatal("memory diverged from renamed file")
	}
	_, err = s.GetAlert("ns", "a")
	assertStoreStatus(t, err, 500)
	_, _, err = s.CreateAlert("ns", testAlert("b"))
	assertStoreStatus(t, err, 500)
	_, err = s.ApplyAction("ns", "a", ticket, "acknowledge")
	assertStoreStatus(t, err, 500)
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	assertStoreStatus(t, s.Check(), 500)
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Check(); err != nil {
		t.Fatalf("reopened store check failed: %v", err)
	}
	replay, err := s.ApplyAction("ns", "a", ticket, "acknowledge")
	if err != nil || !replay.Replayed || replay.Alert.Revision != 1 {
		t.Fatalf("reopen did not recover visible commit: %#v %v", replay, err)
	}
	if len(s.PendingEvents("ns")) != 1 {
		t.Fatal("recovery lost committed outbox")
	}
}

func TestStoreLoginCapacityScalesWithRecipients(t *testing.T) {
	s := openTestStore(t)
	a := testAlert("many")
	a.Users = nil
	for i := 0; i < 30; i++ {
		a.Users = append(a.Users, fmt.Sprintf("member%d", i))
	}
	if _, _, err := s.CreateAlert("ns", a); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDelivery("ns", a.ID, Delivery{MsgID: "m"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 60; i++ {
		state, _ := randomID()
		if err := s.StartLogin("ns", a.ID, state); err != nil {
			t.Fatalf("login %d: %v", i+1, err)
		}
	}
	state, _ := randomID()
	assertStoreStatus(t, s.StartLogin("ns", a.ID, state), 409)
}

func TestStoreRejectsOversizedSnapshotWithoutReadingIt(t *testing.T) {
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "store.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(maxSnapshotBytes + 1); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(dir)
	if s != nil {
		s.Close()
		t.Fatal("oversized snapshot accepted")
	}
	assertStoreStatus(t, err, 500)
	if !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("snapshot was parsed before size rejection: %v", err)
	}
}
