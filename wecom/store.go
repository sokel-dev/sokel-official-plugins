package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	maxAlerts        = 10000
	maxAuthRecords   = 20000
	maxOutboxRecords = 20000
	maxSnapshotBytes = (128 << 20) - 1
	authLifetime     = 5 * time.Minute
	storeRetention   = 24 * time.Hour
)

var opaqueTokenPattern = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

type InteractiveButton struct {
	ID    string         `json:"id"`
	Text  string         `json:"text"`
	Value map[string]any `json:"value,omitempty"`
}

type Alert struct {
	ID, ExternalID, Title, Description, Severity, Status, ActorUserID string
	CreatedAt, UpdatedAt, ExpiresAt, PortalURL                        string
	Users                                                             []string
	Sent                                                              bool
	Delivery                                                          Delivery
	Revision                                                          int
	Buttons                                                           []InteractiveButton
	Context                                                           map[string]any
	ButtonText, LastActionID                                          string
}

type ActionRecord struct {
	ID, Namespace, AlertID, ExternalID, Action, Status, ActorUserID, OccurredAt string
	Dispatched                                                                  bool
	Revision                                                                    int
	MsgID, ActionText                                                           string
	Value, Context                                                              map[string]any
}

type ActionResult struct {
	Alert                   Alert
	UserID, Ticket, EventID string
	Replayed                bool
}

type StoreError struct {
	Status  int
	Message string
}

func (e *StoreError) Error() string { return e.Message }
func storeError(status int, message string) error {
	return &StoreError{Status: status, Message: message}
}

type loginRecord struct{ Namespace, AlertID, ExpiresAt string }
type ticketRecord struct {
	Namespace, AlertID, UserID, ExpiresAt, Action, EventID string
	Result                                                 Alert
}
type storedAlert struct {
	Namespace string
	Alert     Alert
}
type storeData struct {
	Version    int
	Alerts     map[string]storedAlert
	Logins     map[string]loginRecord
	Tickets    map[string]ticketRecord
	Events     map[string]ActionRecord
	EventOrder []string
}

// Store owns its directory for its complete lifetime. A mutation publishes its
// snapshot after rename. If the directory sync then fails, durability is uncertain
// and the store refuses further use until reopened.
type Store struct {
	mu          sync.Mutex
	dir         string
	lock        *os.File
	data        storeData
	closed      bool
	unavailable bool
	now         func() time.Time
	syncDir     func(*os.File) error
}

func OpenStore(dir string) (*Store, error) {
	if dir == "" {
		return nil, storeError(500, "store directory is required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create store directory: %w", err)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, storeError(500, "store directory must be a real directory")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	lock, err := acquireStoreLock(filepath.Join(dir, "store.lock"))
	if err != nil {
		return nil, err
	}
	s := &Store{dir: dir, lock: lock, now: time.Now, syncDir: (*os.File).Sync, data: storeData{Version: 1, Alerts: map[string]storedAlert{}, Logins: map[string]loginRecord{}, Tickets: map[string]ticketRecord{}, Events: map[string]ActionRecord{}}}
	path := filepath.Join(dir, "store.json")
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			s.Close()
			return nil, storeError(500, "store file must be a regular file")
		}
		if info.Size() > maxSnapshotBytes {
			s.Close()
			return nil, storeError(500, "store file exceeds size limit")
		}
		if err := os.Chmod(path, 0600); err != nil {
			s.Close()
			return nil, err
		}
		f, err := os.Open(path)
		if err != nil {
			s.Close()
			return nil, err
		}
		dec := json.NewDecoder(io.LimitReader(f, maxSnapshotBytes+1))
		dec.UseNumber()
		err = dec.Decode(&s.data)
		if err == nil {
			var extra any
			if next := dec.Decode(&extra); next != io.EOF {
				err = errors.New("trailing or excessive store data")
			}
		}
		f.Close()
		if err != nil || s.data.Version != 1 || s.data.Alerts == nil || s.data.Logins == nil || s.data.Tickets == nil || s.data.Events == nil {
			s.Close()
			return nil, storeError(500, "invalid store data")
		}
		if len(s.data.Alerts) > maxAlerts || len(s.data.Logins) > maxAuthRecords || len(s.data.Tickets) > maxAuthRecords || len(s.data.Events) > maxOutboxRecords {
			s.Close()
			return nil, storeError(500, "store exceeds resource limits")
		}
		seen := map[string]bool{}
		for _, id := range s.data.EventOrder {
			if _, ok := s.data.Events[id]; !ok || seen[id] {
				s.Close()
				return nil, storeError(500, "invalid event outbox order")
			}
			seen[id] = true
		}
		if len(seen) != len(s.data.Events) {
			s.Close()
			return nil, storeError(500, "incomplete event outbox order")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		s.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.lock != nil {
		return s.lock.Close()
	}
	return nil
}

// Check reports whether the store can safely serve requests. A commit with
// uncertain directory durability remains unhealthy until the store is reopened.
func (s *Store) Check() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.available()
}

func randomID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func recordKey(ns, id string) string { return fmt.Sprintf("%d:%s%s", len(ns), ns, id) }

// Persisted arbitrary data is normalized through JSON at ingress. Recursive
// copies therefore only encounter maps, arrays and immutable JSON scalars.
func cloneJSONValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return cloneJSONObject(v)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = cloneJSONValue(item)
		}
		return out
	default:
		return value
	}
}

func cloneJSONObject(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	out := make(map[string]any, len(value))
	for key, item := range value {
		out[key] = cloneJSONValue(item)
	}
	return out
}

func cloneAlert(a Alert) Alert {
	a.Users = append([]string(nil), a.Users...)
	a.Buttons = append([]InteractiveButton(nil), a.Buttons...)
	for i := range a.Buttons {
		a.Buttons[i].Value = cloneJSONObject(a.Buttons[i].Value)
	}
	a.Context = cloneJSONObject(a.Context)
	return a
}

func cloneActionRecord(a ActionRecord) ActionRecord {
	a.Value = cloneJSONObject(a.Value)
	a.Context = cloneJSONObject(a.Context)
	return a
}

func normalizeAlert(a Alert) (Alert, error) {
	b, err := json.Marshal(a)
	if err != nil {
		return Alert{}, storeError(400, "interactive message contains invalid JSON data")
	}
	if len(b) > maxSnapshotBytes {
		return Alert{}, storeError(409, "message exceeds storage capacity")
	}
	var out Alert
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&out); err != nil {
		return Alert{}, storeError(400, "interactive message contains invalid JSON data")
	}
	seen := make(map[string]bool, len(out.Buttons))
	for _, button := range out.Buttons {
		if strings.TrimSpace(button.ID) == "" || strings.TrimSpace(button.Text) == "" || seen[button.ID] {
			return Alert{}, storeError(400, "interactive buttons require unique IDs and nonempty text")
		}
		seen[button.ID] = true
	}
	return out, nil
}
func (d storeData) clone() storeData {
	n := storeData{Version: d.Version, Alerts: make(map[string]storedAlert, len(d.Alerts)), Logins: make(map[string]loginRecord, len(d.Logins)), Tickets: make(map[string]ticketRecord, len(d.Tickets)), Events: make(map[string]ActionRecord, len(d.Events))}
	for k, v := range d.Alerts {
		v.Alert = cloneAlert(v.Alert)
		n.Alerts[k] = v
	}
	for k, v := range d.Logins {
		n.Logins[k] = v
	}
	for k, v := range d.Tickets {
		v.Result = cloneAlert(v.Result)
		n.Tickets[k] = v
	}
	for k, v := range d.Events {
		n.Events[k] = cloneActionRecord(v)
	}
	n.EventOrder = append([]string(nil), d.EventOrder...)
	return n
}
func expired(at string, now time.Time) bool {
	t, err := time.Parse(time.RFC3339, at)
	return err != nil || !now.Before(t)
}
func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func (d *storeData) cleanup(now time.Time) {
	for k, v := range d.Logins {
		if expired(v.ExpiresAt, now) {
			delete(d.Logins, k)
		}
	}
	for k, v := range d.Tickets {
		if expired(v.ExpiresAt, now) {
			delete(d.Tickets, k)
		}
	}
	pending := map[string]bool{}
	for k, v := range d.Events {
		if !v.Dispatched {
			pending[recordKey(v.Namespace, v.AlertID)] = true
			continue
		}
		if t, err := time.Parse(time.RFC3339, v.OccurredAt); err == nil && now.Sub(t) > storeRetention {
			delete(d.Events, k)
		}
	}
	for k, v := range d.Alerts {
		if t, err := time.Parse(time.RFC3339, v.Alert.ExpiresAt); err == nil && now.Sub(t) > storeRetention && !pending[k] {
			delete(d.Alerts, k)
		}
	}
	order := d.EventOrder[:0]
	for _, id := range d.EventOrder {
		if _, ok := d.Events[id]; ok {
			order = append(order, id)
		}
	}
	d.EventOrder = order
}

func (s *Store) commit(d storeData) error {
	if err := s.available(); err != nil {
		return err
	}
	b, err := json.Marshal(d)
	if err != nil {
		return storeError(500, "encode store failed")
	}
	if len(b) > maxSnapshotBytes {
		return storeError(409, "store snapshot capacity reached")
	}
	f, err := os.CreateTemp(s.dir, ".store-*")
	if err != nil {
		return storeError(500, "create store snapshot failed")
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return storeError(500, "write store snapshot failed")
	}
	if err = os.Rename(name, filepath.Join(s.dir, "store.json")); err != nil {
		return storeError(500, "replace store snapshot failed")
	}
	// The new file is already visible. Never continue with the old in-memory
	// snapshot if syncing the directory fails after this point.
	s.data = d
	dir, err := os.Open(s.dir)
	if err != nil {
		s.unavailable = true
		return storeError(500, "store commit durability uncertain; restart required")
	}
	err = s.syncDir(dir)
	dir.Close()
	if err != nil {
		s.unavailable = true
		return storeError(500, "store commit durability uncertain; restart required")
	}
	return nil
}

func (s *Store) available() error {
	if s.closed {
		return storeError(500, "store is closed")
	}
	if s.unavailable {
		return storeError(500, "store commit durability uncertain; restart required")
	}
	return nil
}

func (s *Store) alert(ns, id string, active bool) (Alert, error) {
	if err := s.available(); err != nil {
		return Alert{}, err
	}
	v, ok := s.data.Alerts[recordKey(ns, id)]
	if !ok {
		return Alert{}, storeError(404, "interactive message not found")
	}
	if active && expired(v.Alert.ExpiresAt, s.now()) {
		return Alert{}, storeError(410, "interactive message expired")
	}
	return cloneAlert(v.Alert), nil
}
func userSet(users []string) []string {
	m := map[string]bool{}
	for _, v := range users {
		m[strings.ToLower(v)] = true
	}
	r := make([]string, 0, len(m))
	for v := range m {
		r = append(r, v)
	}
	sort.Strings(r)
	return r
}
func sameContent(a, b Alert) bool {
	if a.Title != b.Title || a.Description != b.Description || a.Severity != b.Severity || a.PortalURL != b.PortalURL || a.ButtonText != b.ButtonText || !reflect.DeepEqual(a.Buttons, b.Buttons) || !reflect.DeepEqual(a.Context, b.Context) {
		return false
	}
	x, y := userSet(a.Users), userSet(b.Users)
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}
func (s *Store) CreateAlert(ns string, a Alert) (Alert, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.available(); err != nil {
		return Alert{}, false, err
	}
	if ns == "" || a.ID == "" {
		return Alert{}, false, storeError(400, "namespace and message ID are required")
	}
	if _, err := time.Parse(time.RFC3339, a.ExpiresAt); err != nil {
		return Alert{}, false, storeError(400, "invalid message expiry")
	}
	var err error
	a, err = normalizeAlert(a)
	if err != nil {
		return Alert{}, false, err
	}
	n := s.data.clone()
	n.cleanup(s.now())
	for _, v := range n.Alerts {
		if v.Namespace == ns && a.ExternalID != "" && v.Alert.ExternalID == a.ExternalID {
			if !sameContent(v.Alert, a) {
				return Alert{}, false, storeError(409, "request ID already has different message content")
			}
			return cloneAlert(v.Alert), true, nil
		}
	}
	if len(n.Alerts) >= maxAlerts {
		return Alert{}, false, storeError(409, "message storage capacity reached")
	}
	k := recordKey(ns, a.ID)
	if _, ok := n.Alerts[k]; ok {
		return Alert{}, false, storeError(409, "message ID already exists")
	}
	a = cloneAlert(a)
	a.Status = ""
	a.Revision = 0
	a.ActorUserID = ""
	a.LastActionID = ""
	a.Sent = false
	a.Delivery = Delivery{}
	if a.CreatedAt == "" {
		a.CreatedAt = formatTime(s.now())
	}
	a.UpdatedAt = a.CreatedAt
	n.Alerts[k] = storedAlert{Namespace: ns, Alert: a}
	if err := s.commit(n); err != nil {
		return Alert{}, false, err
	}
	return cloneAlert(a), false, nil
}
func (s *Store) GetAlert(ns, id string) (Alert, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.alert(ns, id, false)
}
func (s *Store) SetDelivery(ns, id string, d Delivery) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, err := s.alert(ns, id, true)
	if err != nil {
		return err
	}
	n := s.data.clone()
	n.cleanup(s.now())
	a.Delivery = d
	a.Sent = true
	a.UpdatedAt = formatTime(s.now())
	n.Alerts[recordKey(ns, id)] = storedAlert{ns, a}
	return s.commit(n)
}
func (s *Store) StartLogin(ns, id, state string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !opaqueTokenPattern.MatchString(state) {
		return storeError(400, "invalid login state")
	}
	a, err := s.alert(ns, id, true)
	if err != nil {
		return err
	}
	if !a.Sent {
		return storeError(409, "message has not been sent")
	}
	n := s.data.clone()
	n.cleanup(s.now())
	hash := tokenHash(state)
	if _, ok := n.Logins[hash]; ok {
		return storeError(409, "login state already exists")
	}
	count := 0
	for _, v := range n.Logins {
		if v.Namespace == ns && v.AlertID == id {
			count++
		}
	}
	if count >= max(20, len(a.Users)*2) || len(n.Logins) >= maxAuthRecords {
		return storeError(409, "too many pending logins")
	}
	n.Logins[hash] = loginRecord{ns, id, formatTime(s.now().Add(authLifetime))}
	return s.commit(n)
}
func (s *Store) ConsumeLogin(ns, id, state string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !opaqueTokenPattern.MatchString(state) {
		return storeError(400, "invalid login state")
	}
	if _, err := s.alert(ns, id, true); err != nil {
		return err
	}
	hash := tokenHash(state)
	v, ok := s.data.Logins[hash]
	if !ok || v.Namespace != ns || v.AlertID != id {
		return storeError(403, "invalid or consumed login state")
	}
	if expired(v.ExpiresAt, s.now()) {
		return storeError(410, "login state expired")
	}
	n := s.data.clone()
	delete(n.Logins, hash)
	n.cleanup(s.now())
	return s.commit(n)
}
func authorized(a Alert, user string) bool {
	if !a.Sent || user == "" {
		return false
	}
	for _, v := range strings.Split(a.Delivery.InvalidUser+"|"+a.Delivery.UnlicensedUser, "|") {
		if strings.EqualFold(v, user) {
			return false
		}
	}
	for _, v := range a.Users {
		if strings.EqualFold(v, user) {
			return true
		}
	}
	return false
}
func (s *Store) NewTicket(ns, id, userID string) (string, Alert, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, err := s.alert(ns, id, true)
	if err != nil {
		return "", Alert{}, err
	}
	if !authorized(a, userID) {
		return "", Alert{}, storeError(403, "user is not an authorized message recipient")
	}
	n := s.data.clone()
	n.cleanup(s.now())
	if len(n.Tickets) >= maxAuthRecords {
		return "", Alert{}, storeError(409, "too many action tickets")
	}
	ticket, err := randomID()
	if err != nil {
		return "", Alert{}, storeError(500, "generate action ticket failed")
	}
	n.Tickets[tokenHash(ticket)] = ticketRecord{Namespace: ns, AlertID: id, UserID: userID, ExpiresAt: formatTime(s.now().Add(authLifetime))}
	if err := s.commit(n); err != nil {
		return "", Alert{}, err
	}
	return ticket, a, nil
}
func (s *Store) ApplyAction(ns, id, ticket, action string) (ActionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !opaqueTokenPattern.MatchString(ticket) {
		return ActionResult{}, storeError(403, "invalid action ticket")
	}
	a, err := s.alert(ns, id, true)
	if err != nil {
		return ActionResult{}, err
	}
	hash := tokenHash(ticket)
	v, ok := s.data.Tickets[hash]
	if !ok || v.Namespace != ns || v.AlertID != id {
		return ActionResult{}, storeError(403, "invalid action ticket")
	}
	if expired(v.ExpiresAt, s.now()) {
		return ActionResult{}, storeError(410, "action ticket expired")
	}
	if !authorized(a, v.UserID) {
		return ActionResult{}, storeError(403, "user is not an authorized message recipient")
	}
	if v.Action != "" && v.Action != action {
		return ActionResult{}, storeError(409, "action ticket already used for another action")
	}
	var button *InteractiveButton
	for i := range a.Buttons {
		if a.Buttons[i].ID == action {
			button = &a.Buttons[i]
			break
		}
	}
	if button == nil {
		return ActionResult{}, storeError(400, "action is not configured on this message")
	}
	n := s.data.clone()
	n.cleanup(s.now())
	if len(n.Tickets) >= maxAuthRecords {
		return ActionResult{}, storeError(409, "too many action tickets")
	}
	if v.Action == "" && len(n.Events) >= maxOutboxRecords {
		return ActionResult{}, storeError(409, "event outbox capacity reached")
	}
	nextTicket, err := randomID()
	if err != nil {
		return ActionResult{}, storeError(500, "generate action ticket failed")
	}
	n.Tickets[tokenHash(nextTicket)] = ticketRecord{Namespace: ns, AlertID: id, UserID: v.UserID, ExpiresAt: formatTime(s.now().Add(authLifetime))}
	if v.Action != "" {
		// A lost response may also lose the continuation ticket. Reissuing one
		// never restores the old message snapshot or creates another event.
		if err := s.commit(n); err != nil {
			return ActionResult{}, err
		}
		return ActionResult{Alert: cloneAlert(v.Result), UserID: v.UserID, Ticket: nextTicket, EventID: v.EventID, Replayed: true}, nil
	}
	eventID, err := randomID()
	if err != nil {
		return ActionResult{}, storeError(500, "generate action event failed")
	}
	a.Revision++
	a.ActorUserID = v.UserID
	a.LastActionID = action
	a.UpdatedAt = formatTime(s.now())
	result := ActionResult{Alert: cloneAlert(a), UserID: v.UserID, Ticket: nextTicket, EventID: eventID}
	v.Action = action
	v.EventID = eventID
	v.Result = cloneAlert(a)
	n.Tickets[hash] = v
	n.Alerts[recordKey(ns, id)] = storedAlert{ns, cloneAlert(a)}
	n.Events[eventID] = ActionRecord{ID: eventID, Namespace: ns, AlertID: id, ExternalID: a.ExternalID, Action: action, ActorUserID: v.UserID, OccurredAt: a.UpdatedAt, Revision: a.Revision, MsgID: a.Delivery.MsgID, ActionText: button.Text, Value: cloneJSONObject(button.Value), Context: cloneJSONObject(a.Context)}
	n.EventOrder = append(n.EventOrder, eventID)
	if err := s.commit(n); err != nil {
		return ActionResult{}, err
	}
	return result, nil
}
func (s *Store) PendingEvents(ns string) []ActionRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	events := []ActionRecord{}
	if s.available() != nil {
		return events
	}
	for _, id := range s.data.EventOrder {
		v := s.data.Events[id]
		if v.Namespace == ns && !v.Dispatched {
			events = append(events, cloneActionRecord(v))
		}
	}
	return events
}
func (s *Store) MarkDispatched(ns, eventID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.available(); err != nil {
		return err
	}
	v, ok := s.data.Events[eventID]
	if !ok || v.Namespace != ns {
		return storeError(404, "event not found")
	}
	if v.Dispatched {
		return nil
	}
	n := s.data.clone()
	v.Dispatched = true
	n.Events[eventID] = v
	n.cleanup(s.now())
	return s.commit(n)
}
