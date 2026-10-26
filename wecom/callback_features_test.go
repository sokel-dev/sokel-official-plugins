package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func callbackTestCredential() Cred {
	return Cred{CorpID: callbackVectorCorp, AgentID: "218", Secret: "local-test-only", CallbackToken: "QDG6eK", CallbackAesKey: callbackVectorKey}
}

func callbackTestKey(cred Cred) string {
	sum := sha256.Sum256([]byte(cred.CorpID + cred.AgentID + cred.Secret))
	return hex.EncodeToString(sum[:])
}

func newCallbackTestService(t *testing.T) (*CallbackService, *CallbackStore, Cred) {
	t.Helper()
	store, err := OpenCallbackStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	service := NewCallbackService(store)
	now := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	store.now = service.now
	return service, store, callbackTestCredential()
}

func callbackPlain(cred Cred, id, msgType, extra string) string {
	return fmt.Sprintf("<xml><ToUserName>%s</ToUserName><FromUserName>zhangsan</FromUserName><CreateTime>1793008800</CreateTime><MsgType>%s</MsgType><MsgId>%s</MsgId><AgentID>%s</AgentID>%s</xml>", cred.CorpID, msgType, id, cred.AgentID, extra)
}

func callbackRequest(t *testing.T, cred Cred, plain string, at time.Time) (url.Values, []byte) {
	t.Helper()
	crypt, err := NewCallbackCrypt(cred.CallbackToken, cred.CallbackAesKey, cred.CorpID)
	if err != nil {
		t.Fatal(err)
	}
	out, err := crypt.EncryptReply([]byte(plain), strconv.FormatInt(at.Unix(), 10), "test-request-nonce")
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct{ Encrypt, MsgSignature, TimeStamp, Nonce string }
	if err = xml.Unmarshal(out, &envelope); err != nil {
		t.Fatal(err)
	}
	query := url.Values{"msg_signature": {envelope.MsgSignature}, "timestamp": {envelope.TimeStamp}, "nonce": {envelope.Nonce}}
	body := []byte("<xml><ToUserName>" + cred.CorpID + "</ToUserName><AgentID>" + cred.AgentID + "</AgentID><Encrypt>" + envelope.Encrypt + "</Encrypt></xml>")
	return query, body
}

func callbackHTTP(service *CallbackService, cred Cred, query url.Values, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "https://plugin.invalid/callback?"+query.Encode(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/xml")
	w := httptest.NewRecorder()
	http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		status, typ, response, _ := service.Handle(r.Context(), cred, callbackTestKey(cred), r.Method, r.URL.Query(), b)
		w.Header().Set("Content-Type", typ)
		w.WriteHeader(status)
		_, _ = w.Write(response)
	}).ServeHTTP(w, req)
	return w
}

func decodedCallbackReply(t *testing.T, cred Cred, body []byte) map[string]string {
	t.Helper()
	var envelope struct{ Encrypt, MsgSignature, TimeStamp, Nonce string }
	if err := xml.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	crypt, err := NewCallbackCrypt(cred.CallbackToken, cred.CallbackAesKey, cred.CorpID)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := crypt.Decrypt(envelope.MsgSignature, envelope.TimeStamp, envelope.Nonce, envelope.Encrypt)
	if err != nil {
		t.Fatal(err)
	}
	fields, err := callbackXMLFields(plain)
	if err != nil {
		t.Fatal(err)
	}
	return fields
}

func TestCallbackHTTPAcceptanceReplayAndPersistence(t *testing.T) {
	service, store, cred := newCallbackTestService(t)
	rule := AutoReplyRule{ID: "ack", Enabled: true, MatchType: "text_exact", Keyword: "状态", Reply: "收到，正在查询 <服务>&状态", Priority: 10}
	if err := store.SetRule(cred, rule); err != nil {
		t.Fatal(err)
	}
	plain := callbackPlain(cred, "4561255354251345929", "text", "<Content>状态</Content>")
	query, body := callbackRequest(t, cred, plain, service.now())
	first := callbackHTTP(service, cred, query, body)
	if first.Code != 200 || !strings.HasPrefix(first.Header().Get("Content-Type"), "application/xml") {
		t.Fatalf("initial callback: %d %s", first.Code, first.Body)
	}
	fields := decodedCallbackReply(t, cred, first.Body.Bytes())
	if fields["Content"] != rule.Reply || fields["ToUserName"] != "zhangsan" || fields["FromUserName"] != cred.CorpID {
		t.Fatalf("bad passive reply: %#v", fields)
	}
	pending, err := store.Pending(cred, callbackTestKey(cred), 20)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending: %+v %v", pending, err)
	}
	if pending[0].Message.MsgID != "4561255354251345929" || pending[0].Message.RawXML != plain {
		t.Fatal("message ID precision or raw XML lost")
	}
	rule.Reply = "changed configuration"
	if err := store.SetRule(cred, rule); err != nil {
		t.Fatal(err)
	}
	second := callbackHTTP(service, cred, query, body)
	if second.Code != 200 || !bytes.Equal(first.Body.Bytes(), second.Body.Bytes()) {
		t.Fatal("retry did not return exact cached reply")
	}
	if err := store.MarkDispatched(pending[0].ID); err != nil {
		t.Fatal(err)
	}
	dir := store.dir
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenCallbackStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	service.store = reopened
	third := callbackHTTP(service, cred, query, body)
	if third.Code != 200 || !bytes.Equal(first.Body.Bytes(), third.Body.Bytes()) {
		t.Fatal("restart lost replay cache")
	}
	list, err := reopened.ListMessages(cred, 10)
	if err != nil || len(list) != 1 || !list[0].Dispatched {
		t.Fatalf("persisted message: %+v %v", list, err)
	}
	if got, err := reopened.Pending(cred, callbackTestKey(cred), 10); err != nil || len(got) != 0 {
		t.Fatalf("re-emitted dispatched event: %+v %v", got, err)
	}
	info, err := os.Stat(filepath.Join(dir, "callbacks.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("snapshot permissions: %v %v", info, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "callbacks.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{cred.Secret, cred.CallbackToken, cred.CallbackAesKey} {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatal("persisted plaintext credential material")
		}
	}
}

func TestCallbackRejectsInvalidRequestsWithoutRecording(t *testing.T) {
	service, store, cred := newCallbackTestService(t)
	good := callbackPlain(cred, "1", "text", "<Content>hello</Content>")
	for _, tc := range []struct {
		name, plain string
		delta       time.Duration
		tamper      string
	}{
		{"wrong signed application", strings.Replace(good, "<AgentID>218", "<AgentID>219", 1), 0, ""},
		{"wrong signed recipient", strings.Replace(good, cred.CorpID, "another-corp", 1), 0, ""},
		{"missing member application", strings.Replace(good, "<AgentID>218</AgentID>", "", 1), 0, ""},
		{"duplicate sender", strings.Replace(good, "</xml>", "<FromUserName>evil</FromUserName></xml>", 1), 0, ""},
		{"nested scalar", strings.Replace(good, "<Content>hello", "<Content><Nested>hidden</Nested>hello", 1), 0, ""},
		{"extra root", good + "<xml/>", 0, ""},
		{"DTD", "<!DOCTYPE xml [<!ENTITY x 'bad'>]>" + good, 0, ""},
		{"stale timestamp", good, -301 * time.Second, ""},
		{"future timestamp", good, 301 * time.Second, ""},
		{"invalid signature", good, 0, "signature"},
		{"outer application mismatch", good, 0, "outer"},
		{"duplicate query field", good, 0, "query"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q, b := callbackRequest(t, cred, tc.plain, service.now().Add(tc.delta))
			switch tc.tamper {
			case "signature":
				q.Set("msg_signature", strings.Repeat("0", 40))
			case "outer":
				b = bytes.Replace(b, []byte("<AgentID>218"), []byte("<AgentID>219"), 1)
			case "query":
				q.Add("nonce", "extra")
			}
			result := callbackHTTP(service, cred, q, b)
			if result.Code == 200 {
				t.Fatal("accepted invalid callback")
			}
		})
	}
	if list, err := store.ListMessages(cred, 50); err != nil || len(list) != 0 {
		t.Fatalf("recorded rejected messages: %+v %v", list, err)
	}
}

func TestCallbackGETChallengeAndNoStoreDependency(t *testing.T) {
	service := NewCallbackService(nil)
	service.now = func() time.Time { return time.Unix(1409659589, 0) }
	q := url.Values{"msg_signature": {"5c45ff5e21c57e6ad56bac8758b79b1d9ac89fd3"}, "timestamp": {"1409659589"}, "nonce": {"263014780"}, "echostr": {"P9nAzCzyDtyTWESHep1vC5X9xho/qYX3Zpb4yKa9SKld1DsH3Iyt3tP3zNdtp+4RPcs8TgAE7OaBO+FZXvnaqQ=="}}
	status, _, body, err := service.Handle(context.Background(), callbackTestCredential(), "", http.MethodGet, q, nil)
	if err != nil || status != 200 || string(body) != "1616140317555161061" {
		t.Fatalf("challenge %d %q %v", status, body, err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if status, _, _, _ := service.Handle(canceled, callbackTestCredential(), "", http.MethodGet, q, nil); status == 200 {
		t.Fatal("processed canceled request")
	}
}

func TestCallbackRulePriorityMenuAndSystemEvents(t *testing.T) {
	service, store, cred := newCallbackTestService(t)
	for _, rule := range []AutoReplyRule{
		{ID: "low", Enabled: true, MatchType: "text_contains", Keyword: "status", Reply: "low", Priority: 1},
		{ID: "high", Enabled: true, MatchType: "text_exact", Keyword: "status", Reply: "high", Priority: 10},
		{ID: "disabled", Enabled: false, MatchType: "text_exact", Keyword: "status", Reply: "disabled", Priority: 100},
		{ID: "menu", Enabled: true, MatchType: "menu_click", Keyword: "ALERTS", Reply: "menu reply", Priority: 10},
	} {
		if err := store.SetRule(cred, rule); err != nil {
			t.Fatal(err)
		}
	}
	for i, tc := range []struct{ kind, extra, want string }{
		{"text", "<Content>status</Content>", "high"},
		{"text", "<Content>current status please</Content>", "low"},
		{"image", "<MediaId>test-media</MediaId>", ""},
		{"event", "<Event>click</Event><EventKey>ALERTS</EventKey>", "menu reply"},
		{"event", "<Event>click</Event><EventKey>OTHER</EventKey>", ""},
		{"event", "<Event>template_card_event</Event><EventKey>ALERTS</EventKey>", ""},
	} {
		id := strconv.Itoa(i + 1)
		if tc.kind == "event" {
			id = ""
		}
		q, b := callbackRequest(t, cred, callbackPlain(cred, id, tc.kind, tc.extra), service.now())
		result := callbackHTTP(service, cred, q, b)
		if result.Code != 200 {
			t.Fatalf("case %d: %d %s", i, result.Code, result.Body)
		}
		if tc.want == "" {
			if result.Body.String() != "success" {
				t.Fatal("unexpected passive reply")
			}
		} else if fields := decodedCallbackReply(t, cred, result.Body.Bytes()); fields["Content"] != tc.want {
			t.Fatalf("matched wrong rule: %+v", fields)
		}
	}
	system := "<xml><ToUserName>" + cred.CorpID + "</ToUserName><FromUserName>sys</FromUserName><CreateTime>1793008800</CreateTime><MsgType>event</MsgType><Event>change_contact</Event><ChangeType>create_user</ChangeType><UserID>new-user</UserID></xml>"
	q, b := callbackRequest(t, cred, system, service.now())
	if result := callbackHTTP(service, cred, q, b); result.Code != 200 {
		t.Fatalf("documented system event without AgentID: %d", result.Code)
	}
	list, err := store.ListMessages(cred, 100)
	if err != nil || len(list) != 7 {
		t.Fatalf("same-second events merged: %d %v", len(list), err)
	}
	if err := store.DeleteRule(cred, "high"); err != nil {
		t.Fatal(err)
	}
	rules, err := store.ListRules(cred)
	if err != nil || len(rules) != 3 {
		t.Fatalf("rule deletion: %+v %v", rules, err)
	}
}

func TestCallbackCredentialIsolationAndFilterBeforeLimit(t *testing.T) {
	service, store, cred := newCallbackTestService(t)
	other := cred
	other.AgentID = "219"
	rule := AutoReplyRule{ID: "same-id", Enabled: true, MatchType: "text_exact", Keyword: "hello", Reply: "private reply"}
	if err := store.SetRule(cred, rule); err != nil {
		t.Fatal(err)
	}
	if rules, err := store.ListRules(other); err != nil || len(rules) != 0 {
		t.Fatal("rules crossed application namespace")
	}
	for i, c := range []Cred{other, other, cred} {
		q, b := callbackRequest(t, c, callbackPlain(c, strconv.Itoa(i+1), "text", "<Content>hello</Content>"), service.now())
		if result := callbackHTTP(service, c, q, b); result.Code != 200 {
			t.Fatalf("case %d failed", i)
		}
	}
	pending, err := store.Pending(cred, callbackTestKey(cred), 1)
	if err != nil || len(pending) != 1 || pending[0].Message.AgentID != cred.AgentID {
		t.Fatalf("limit applied before credential filtering: %+v %v", pending, err)
	}
	if pending, err := store.Pending(cred, callbackTestKey(other), 50); err != nil || len(pending) != 0 {
		t.Fatal("pending event crossed credential binding")
	}
	rotated := cred
	rotated.CallbackAesKey = strings.Repeat("A", 43)
	if list, err := store.ListMessages(rotated, 100); err != nil || len(list) != 0 {
		t.Fatal("AES key rotation did not isolate stored data")
	}
}

func TestCallbackConcurrentRetriesAreSingleDurableEvent(t *testing.T) {
	service, store, cred := newCallbackTestService(t)
	q, b := callbackRequest(t, cred, callbackPlain(cred, "1", "text", "<Content>hello</Content>"), service.now())
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if result := callbackHTTP(service, cred, q, b); result.Code != 200 {
				t.Errorf("retry failed: %d", result.Code)
			}
		}()
	}
	wg.Wait()
	if list, err := store.ListMessages(cred, 100); err != nil || len(list) != 1 {
		t.Fatalf("duplicate outbox records: %+v %v", list, err)
	}
}

func TestCallbackDurabilityFailureNeverAcknowledges(t *testing.T) {
	service, store, cred := newCallbackTestService(t)
	q, b := callbackRequest(t, cred, callbackPlain(cred, "1", "text", "<Content>hello</Content>"), service.now())
	store.syncDir = func(*os.File) error { return errors.New("injected sync failure") }
	if result := callbackHTTP(service, cred, q, b); result.Code != 503 {
		t.Fatalf("acknowledged uncertain commit: %d", result.Code)
	}
	if _, err := store.Pending(cred, callbackTestKey(cred), 10); err == nil {
		t.Fatal("continued using poisoned store")
	}
	dir := store.dir
	_ = store.Close()
	reopened, err := OpenCallbackStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	service.store = reopened
	if result := callbackHTTP(service, cred, q, b); result.Code != 200 {
		t.Fatal("durable retry failed after restart")
	}
	if list, err := reopened.Pending(cred, callbackTestKey(cred), 10); err != nil || len(list) != 1 {
		t.Fatal("durability recovery lost or duplicated event")
	}
}

func TestCallbackStoreExclusivityAndRuleValidation(t *testing.T) {
	_, store, cred := newCallbackTestService(t)
	if second, err := OpenCallbackStore(store.dir); err == nil {
		second.Close()
		t.Fatal("allowed competing store writers")
	}
	for _, rule := range []AutoReplyRule{
		{ID: "../bad", MatchType: "text_exact", Keyword: "x", Reply: "y"},
		{ID: "bad", MatchType: "everything", Keyword: "x", Reply: "y"},
		{ID: "bad", MatchType: "text_contains", Keyword: "", Reply: "y"},
		{ID: "bad", MatchType: "text_exact", Keyword: "x", Reply: strings.Repeat("中", 683)},
		{ID: "bad", MatchType: "text_exact", Keyword: "x", Reply: "bad\x00reply"},
	} {
		if err := store.SetRule(cred, rule); err == nil {
			t.Fatalf("accepted invalid rule %+v", rule)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListMessages(cred, 10); err == nil {
		t.Fatal("read closed store")
	}
}

func TestCallbackCapacityPreservesUndispatchedEvents(t *testing.T) {
	service, store, cred := newCallbackTestService(t)
	ns, _ := callbackNamespace(cred)
	old := service.now().Add(-8 * 24 * time.Hour).Format(time.RFC3339Nano)
	for i := 0; i < maxCallbackMessages; i++ {
		id := fmt.Sprintf("old-%d", i)
		store.data.Messages[id] = callbackStoredMessage{Pending: CallbackPending{ID: id, Namespace: ns, CredentialKey: callbackTestKey(cred), ReceivedAt: old, Message: CallbackMessage{RawXML: "old"}}, Response: []byte("success"), ContentType: "text/plain"}
		store.data.Order = append(store.data.Order, id)
	}
	q, b := callbackRequest(t, cred, callbackPlain(cred, "new", "text", "<Content>hello</Content>"), service.now())
	if result := callbackHTTP(service, cred, q, b); result.Code != 503 {
		t.Fatal("accepted message beyond store capacity")
	}
	if len(store.data.Messages) != maxCallbackMessages {
		t.Fatal("evicted undispatched events")
	}
	oldRecord := store.data.Messages["old-0"]
	oldRecord.Pending.Dispatched = true
	store.data.Messages["old-0"] = oldRecord
	if result := callbackHTTP(service, cred, q, b); result.Code != 200 {
		t.Fatalf("did not reclaim old dispatched record: %d", result.Code)
	}
	if _, exists := store.data.Messages["old-1"]; !exists {
		t.Fatal("cleanup removed pending event")
	}
}
