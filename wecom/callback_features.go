package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	maxCallbackMessages = 5000
	maxCallbackRules    = 2000
	maxCallbackSnapshot = 64 << 20
	callbackRetention   = 7 * 24 * time.Hour
)

// CallbackMessage preserves the complete authenticated payload for new and
// nested event fields while exposing common fields without 64-bit ID rounding.
type CallbackMessage struct {
	ToUserName, FromUserName, MsgType, MsgID, AgentID                           string
	CreateTime                                                                  int64
	Content, Event, EventKey, ChangeType, RawXML                                string
	MediaID, ThumbMediaID, PicURL, Format, Title, Description, URL              string
	LocationX, LocationY, Scale, Label, Latitude, Longitude, Precision, AppType string
	TaskID, CardType, ResponseCode                                              string
}

type AutoReplyRule struct {
	ID                        string
	Enabled                   bool
	MatchType, Keyword, Reply string
	Priority                  int
}

type CallbackPending struct {
	ID, Namespace, CredentialKey, ReceivedAt string
	Message                                  CallbackMessage
	Dispatched                               bool
}

type callbackStoredRule struct {
	Namespace string
	Rule      AutoReplyRule
}

type callbackStoredMessage struct {
	Pending     CallbackPending
	Response    []byte
	ContentType string
}

type callbackData struct {
	Version  int
	Rules    map[string]callbackStoredRule
	Messages map[string]callbackStoredMessage
	Order    []string
}

// CallbackStore owns a dedicated features directory for its lifetime. Pending
// events are never evicted: full storage produces retryable callback failures.
type CallbackStore struct {
	mu                  sync.Mutex
	dir                 string
	lock                *os.File
	data                callbackData
	closed, unavailable bool
	now                 func() time.Time
	syncDir             func(*os.File) error
}

func OpenCallbackStore(dir string) (*CallbackStore, error) {
	if dir == "" {
		return nil, errors.New("callback store directory is required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("callback store must be a real directory")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	lock, err := acquireStoreLock(filepath.Join(dir, "callbacks.lock"))
	if err != nil {
		return nil, err
	}
	s := &CallbackStore{dir: dir, lock: lock, now: time.Now, syncDir: (*os.File).Sync, data: callbackData{Version: 1, Rules: map[string]callbackStoredRule{}, Messages: map[string]callbackStoredMessage{}}}
	fail := func(err error) (*CallbackStore, error) { _ = s.Close(); return nil, err }
	path := filepath.Join(dir, "callbacks.json")
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Size() > maxCallbackSnapshot {
			return fail(errors.New("invalid callback snapshot"))
		}
		if err := os.Chmod(path, 0600); err != nil {
			return fail(err)
		}
		f, err := os.Open(path)
		if err != nil {
			return fail(err)
		}
		dec := json.NewDecoder(io.LimitReader(f, maxCallbackSnapshot+1))
		err = dec.Decode(&s.data)
		if err == nil {
			if next := dec.Decode(new(json.RawMessage)); next != io.EOF {
				err = errors.New("trailing callback data")
			}
		}
		_ = f.Close()
		if err != nil {
			return fail(errors.New("invalid callback snapshot"))
		}
		if err := s.data.validate(); err != nil {
			return fail(err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fail(err)
	}
	return s, nil
}

func (d callbackData) validate() error {
	if d.Version != 1 || d.Rules == nil || d.Messages == nil || len(d.Messages) > maxCallbackMessages || len(d.Rules) > maxCallbackRules || len(d.Order) != len(d.Messages) {
		return errors.New("invalid callback snapshot")
	}
	seen := map[string]bool{}
	for _, id := range d.Order {
		v, ok := d.Messages[id]
		if !ok || seen[id] || v.Pending.ID != id || len(v.Pending.Namespace) != 64 || len(v.Pending.CredentialKey) != 64 || len(v.Pending.Message.RawXML) > maxCallbackPlaintext || len(v.Response) > maxCallbackEncrypted+1024 {
			return errors.New("invalid callback record")
		}
		if _, err := time.Parse(time.RFC3339Nano, v.Pending.ReceivedAt); err != nil {
			return errors.New("invalid callback time")
		}
		seen[id] = true
	}
	for id, v := range d.Rules {
		if id != v.Namespace+":"+v.Rule.ID || len(v.Namespace) != 64 || validateAutoReply(v.Rule) != nil {
			return errors.New("invalid callback rule")
		}
	}
	return nil
}

func (s *CallbackStore) Close() error {
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

func (s *CallbackStore) available() error {
	if s.closed || s.unavailable {
		return errors.New("callback store unavailable")
	}
	return nil
}

func (s *CallbackStore) clone() callbackData {
	d := callbackData{Version: 1, Rules: make(map[string]callbackStoredRule, len(s.data.Rules)), Messages: make(map[string]callbackStoredMessage, len(s.data.Messages)), Order: append([]string(nil), s.data.Order...)}
	for k, v := range s.data.Rules {
		d.Rules[k] = v
	}
	for k, v := range s.data.Messages {
		d.Messages[k] = v
	}
	return d
}

func (s *CallbackStore) commit(d callbackData) error {
	if err := s.available(); err != nil {
		return err
	}
	b, err := json.Marshal(d)
	if err != nil {
		return errors.New("encode callback snapshot failed")
	}
	if len(b) > maxCallbackSnapshot {
		return errors.New("callback snapshot capacity reached")
	}
	f, err := os.CreateTemp(s.dir, ".callbacks-*")
	if err != nil {
		return errors.New("create callback snapshot failed")
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return errors.New("write callback snapshot failed")
	}
	if err = os.Rename(name, filepath.Join(s.dir, "callbacks.json")); err != nil {
		return errors.New("replace callback snapshot failed")
	}
	s.data = d
	dir, err := os.Open(s.dir)
	if err == nil {
		err = s.syncDir(dir)
		_ = dir.Close()
	}
	if err != nil {
		s.unavailable = true
		return errors.New("callback durability uncertain; restart required")
	}
	return nil
}

func (d *callbackData) cleanup(now time.Time) {
	order := make([]string, 0, len(d.Order))
	for _, id := range d.Order {
		v := d.Messages[id]
		at, err := time.Parse(time.RFC3339Nano, v.Pending.ReceivedAt)
		if v.Pending.Dispatched && err == nil && now.Sub(at) > callbackRetention {
			delete(d.Messages, id)
		} else {
			order = append(order, id)
		}
	}
	d.Order = order
}

func callbackNamespace(cred Cred) (string, error) {
	if _, err := NewCallbackCrypt(cred.CallbackToken, cred.CallbackAesKey, cred.CorpID); err != nil {
		return "", err
	}
	if n, err := strconv.ParseUint(cred.AgentID, 10, 64); err != nil || n == 0 {
		return "", errors.New("invalid callback application")
	}
	sum := sha256.Sum256([]byte(cred.CorpID + "\x00" + cred.AgentID + "\x00" + cred.CallbackAesKey))
	return hex.EncodeToString(sum[:]), nil
}

func validateAutoReply(rule AutoReplyRule) error {
	if rule.ID == "" || len(rule.ID) > 64 {
		return errors.New("rule ID must contain 1–64 letters, digits, underscores or hyphens")
	}
	for _, ch := range rule.ID {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-') {
			return errors.New("invalid rule ID")
		}
	}
	if rule.MatchType != "text_exact" && rule.MatchType != "text_contains" && rule.MatchType != "menu_click" {
		return errors.New("invalid rule match type")
	}
	if rule.Keyword == "" || len(rule.Keyword) > 2048 || rule.Reply == "" || len(rule.Reply) > 2048 || !utf8.ValidString(rule.Keyword) || !utf8.ValidString(rule.Reply) {
		return errors.New("keyword and reply must be UTF-8 text of 1–2048 bytes")
	}
	if rule.Priority < -1000000 || rule.Priority > 1000000 {
		return errors.New("rule priority exceeds range")
	}
	// XML 1.0 forbids certain control characters even inside CDATA.
	if err := xml.EscapeText(io.Discard, []byte(rule.Reply)); err != nil {
		return errors.New("reply contains invalid XML characters")
	}
	for _, ch := range rule.Reply {
		if ch < 0x20 && ch != '\n' && ch != '\r' && ch != '\t' {
			return errors.New("reply contains invalid XML characters")
		}
	}
	return nil
}

func (s *CallbackStore) SetRule(cred Cred, rule AutoReplyRule) error {
	if err := validateAutoReply(rule); err != nil {
		return err
	}
	ns, err := callbackNamespace(cred)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = s.available(); err != nil {
		return err
	}
	key := ns + ":" + rule.ID
	_, exists := s.data.Rules[key]
	if !exists && len(s.data.Rules) >= maxCallbackRules {
		return errors.New("callback rule capacity reached")
	}
	d := s.clone()
	d.Rules[key] = callbackStoredRule{ns, rule}
	return s.commit(d)
}

func (s *CallbackStore) ListRules(cred Cred) ([]AutoReplyRule, error) {
	ns, err := callbackNamespace(cred)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = s.available(); err != nil {
		return nil, err
	}
	return callbackRules(s.data, ns), nil
}

func callbackRules(d callbackData, ns string) []AutoReplyRule {
	out := []AutoReplyRule{}
	for _, v := range d.Rules {
		if v.Namespace == ns {
			out = append(out, v.Rule)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority > out[j].Priority
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (s *CallbackStore) DeleteRule(cred Cred, id string) error {
	ns, err := callbackNamespace(cred)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = s.available(); err != nil {
		return err
	}
	key := ns + ":" + id
	if _, ok := s.data.Rules[key]; !ok {
		return nil
	}
	d := s.clone()
	delete(d.Rules, key)
	return s.commit(d)
}

func callbackLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > 500 {
		return 500
	}
	return limit
}

func (s *CallbackStore) ListMessages(cred Cred, limit int) ([]CallbackPending, error) {
	ns, err := callbackNamespace(cred)
	if err != nil {
		return nil, err
	}
	limit = callbackLimit(limit)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = s.available(); err != nil {
		return nil, err
	}
	out := []CallbackPending{}
	for i := len(s.data.Order) - 1; i >= 0 && len(out) < limit; i-- {
		p := s.data.Messages[s.data.Order[i]].Pending
		if p.Namespace == ns {
			out = append(out, p)
		}
	}
	return out, nil
}

func (s *CallbackStore) Pending(cred Cred, credentialKey string, limit int) ([]CallbackPending, error) {
	ns, err := callbackNamespace(cred)
	if err != nil {
		return nil, err
	}
	limit = callbackLimit(limit)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = s.available(); err != nil {
		return nil, err
	}
	out := []CallbackPending{}
	for _, id := range s.data.Order {
		p := s.data.Messages[id].Pending
		if !p.Dispatched && p.Namespace == ns && p.CredentialKey == credentialKey {
			out = append(out, p)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (s *CallbackStore) MarkDispatched(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.available(); err != nil {
		return err
	}
	v, ok := s.data.Messages[id]
	if !ok {
		return errors.New("callback event not found")
	}
	if v.Pending.Dispatched {
		return nil
	}
	d := s.clone()
	v.Pending.Dispatched = true
	d.Messages[id] = v
	d.cleanup(s.now())
	return s.commit(d)
}

type CallbackService struct {
	store *CallbackStore
	now   func() time.Time
}

func NewCallbackService(store *CallbackStore) *CallbackService {
	return &CallbackService{store: store, now: time.Now}
}

// Handle never makes upstream requests. Authentication uses the callback key;
// durable acceptance and cached passive replies finish before the HTTP 200.
func (s *CallbackService) Handle(ctx context.Context, cred Cred, credentialKey, method string, query url.Values, body []byte) (int, string, []byte, error) {
	fail := func(status int, err error) (int, string, []byte, error) {
		return status, "text/plain; charset=utf-8", []byte("callback request rejected"), err
	}
	if err := ctx.Err(); err != nil {
		return fail(503, err)
	}
	if method != http.MethodGet && method != http.MethodPost {
		return fail(405, errors.New("unsupported callback method"))
	}
	for _, key := range []string{"msg_signature", "timestamp", "nonce"} {
		if len(query[key]) != 1 || query.Get(key) == "" {
			return fail(400, ErrCallbackCrypto)
		}
	}
	ts, err := strconv.ParseInt(query.Get("timestamp"), 10, 64)
	now := s.now()
	if err != nil || ts < now.Unix()-300 || ts > now.Unix()+300 {
		return fail(403, ErrCallbackCrypto)
	}
	crypt, err := NewCallbackCrypt(cred.CallbackToken, cred.CallbackAesKey, cred.CorpID)
	if err != nil {
		return fail(503, errors.New("callback credentials are not configured"))
	}
	if method == http.MethodGet {
		if len(query["echostr"]) != 1 {
			return fail(400, ErrCallbackCrypto)
		}
		echo, err := crypt.VerifyURL(query.Get("msg_signature"), query.Get("timestamp"), query.Get("nonce"), query.Get("echostr"))
		if err != nil {
			return fail(403, err)
		}
		return 200, "text/plain; charset=utf-8", echo, nil
	}
	if len(credentialKey) != 64 {
		return fail(503, errors.New("callback credential identity missing"))
	}
	if len(body) > 2<<20 {
		return fail(413, errors.New("callback request exceeds size limit"))
	}
	outer, err := callbackXMLFields(body)
	if err != nil || outer["Encrypt"] == "" {
		return fail(400, errors.New("invalid encrypted callback XML"))
	}
	plain, err := crypt.Decrypt(query.Get("msg_signature"), query.Get("timestamp"), query.Get("nonce"), outer["Encrypt"])
	if err != nil {
		return fail(403, err)
	}
	message, err := parseCallbackMessage(plain, cred)
	if err != nil {
		return fail(403, err)
	}
	// Outer routing fields are not signed independently. They are optional, but
	// when supplied must agree with the authenticated payload and configured app.
	if v := outer["ToUserName"]; v != "" && v != cred.CorpID {
		return fail(403, ErrCallbackCrypto)
	}
	if v := outer["AgentID"]; v != "" && v != cred.AgentID {
		return fail(403, ErrCallbackCrypto)
	}
	ns, err := callbackNamespace(cred)
	if err != nil {
		return fail(503, err)
	}
	if s.store == nil {
		return fail(503, errors.New("callback store missing"))
	}
	if err = ctx.Err(); err != nil {
		return fail(503, err)
	}
	response, contentType, err := s.store.accept(ns, credentialKey, message, crypt, now)
	if err != nil {
		return fail(503, err)
	}
	return 200, contentType, response, nil
}

func (s *CallbackStore) accept(ns, credentialKey string, message CallbackMessage, crypt *CallbackCrypt, now time.Time) ([]byte, string, error) {
	idValue := message.MsgID
	if idValue == "" {
		sum := sha256.Sum256([]byte(message.RawXML))
		idValue = hex.EncodeToString(sum[:])
	}
	sum := sha256.Sum256([]byte(ns + "\x00" + idValue))
	id := hex.EncodeToString(sum[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.available(); err != nil {
		return nil, "", err
	}
	if old, ok := s.data.Messages[id]; ok {
		if old.Pending.Message.RawXML != message.RawXML {
			return nil, "", errors.New("callback message ID was reused with different content")
		}
		return bytes.Clone(old.Response), old.ContentType, nil
	}
	d := s.clone()
	d.cleanup(now)
	if len(d.Messages) >= maxCallbackMessages {
		return nil, "", errors.New("callback message capacity reached")
	}
	response := []byte("success")
	contentType := "text/plain; charset=utf-8"
	for _, rule := range callbackRules(d, ns) {
		if !matchAutoReply(rule, message) {
			continue
		}
		inner := struct {
			XMLName      xml.Name `xml:"xml"`
			ToUserName   string   `xml:"ToUserName"`
			FromUserName string   `xml:"FromUserName"`
			CreateTime   int64    `xml:"CreateTime"`
			MsgType      string   `xml:"MsgType"`
			Content      string   `xml:"Content"`
		}{ToUserName: message.FromUserName, FromUserName: message.ToUserName, CreateTime: now.Unix(), MsgType: "text", Content: rule.Reply}
		payload, err := xml.Marshal(inner)
		if err != nil {
			return nil, "", errors.New("encode callback reply failed")
		}
		nonce, err := randomID()
		if err != nil {
			return nil, "", err
		}
		response, err = crypt.EncryptReply(payload, strconv.FormatInt(now.Unix(), 10), nonce)
		if err != nil {
			return nil, "", err
		}
		contentType = "application/xml; charset=utf-8"
		break
	}
	pending := CallbackPending{ID: id, Namespace: ns, CredentialKey: credentialKey, ReceivedAt: now.UTC().Format(time.RFC3339Nano), Message: message}
	d.Messages[id] = callbackStoredMessage{Pending: pending, Response: response, ContentType: contentType}
	d.Order = append(d.Order, id)
	if err := s.commit(d); err != nil {
		return nil, "", err
	}
	return bytes.Clone(response), contentType, nil
}

func matchAutoReply(rule AutoReplyRule, m CallbackMessage) bool {
	if !rule.Enabled || m.FromUserName == "sys" {
		return false
	}
	switch rule.MatchType {
	case "text_exact":
		return m.MsgType == "text" && m.Content == rule.Keyword
	case "text_contains":
		return m.MsgType == "text" && strings.Contains(m.Content, rule.Keyword)
	case "menu_click":
		return m.MsgType == "event" && m.Event == "click" && m.EventKey == rule.Keyword
	}
	return false
}

// A token walk rejects duplicate scalar fields, extra roots, namespace tricks,
// DTD/directives and pathological depth while retaining unknown nested events.
func callbackXMLFields(body []byte) (map[string]string, error) {
	if len(body) == 0 || len(body) > 2<<20 {
		return nil, errors.New("invalid callback XML size")
	}
	dec := xml.NewDecoder(bytes.NewReader(body))
	fields := map[string]string{}
	depth, roots, tokens := 0, 0, 0
	field := ""
	var value strings.Builder
	for {
		token, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errors.New("invalid callback XML")
		}
		tokens++
		if tokens > 50000 {
			return nil, errors.New("callback XML exceeds complexity limit")
		}
		switch v := token.(type) {
		case xml.StartElement:
			depth++
			if depth > 64 || v.Name.Space != "" || len(v.Attr) > 0 {
				return nil, errors.New("invalid callback XML element")
			}
			if depth == 1 {
				roots++
				if roots != 1 || v.Name.Local != "xml" {
					return nil, errors.New("invalid callback XML root")
				}
			}
			if depth == 2 {
				field = v.Name.Local
				if _, exists := fields[field]; exists {
					return nil, errors.New("duplicate callback XML field")
				}
				fields[field] = ""
				value.Reset()
			}
			if depth == 3 && callbackScalarField(field) {
				return nil, errors.New("callback scalar field contains nested XML")
			}
		case xml.EndElement:
			if depth == 2 {
				fields[field] = value.String()
				field = ""
			}
			depth--
		case xml.CharData:
			if depth == 2 {
				value.Write(v)
			} else if depth == 0 && strings.TrimSpace(string(v)) != "" {
				return nil, errors.New("trailing callback XML content")
			}
		case xml.Directive:
			return nil, errors.New("callback XML directives are forbidden")
		case xml.ProcInst:
			if roots > 0 || v.Target != "xml" {
				return nil, errors.New("invalid callback XML instruction")
			}
		}
	}
	if roots != 1 || depth != 0 {
		return nil, errors.New("incomplete callback XML")
	}
	return fields, nil
}

func callbackScalarField(field string) bool {
	switch field {
	case "Encrypt", "ToUserName", "FromUserName", "CreateTime", "MsgType", "MsgId", "AgentID", "Content", "Event", "EventKey", "ChangeType", "MediaId", "ThumbMediaId", "PicUrl", "Format", "Title", "Description", "Url", "Location_X", "Location_Y", "Scale", "Label", "Latitude", "Longitude", "Precision", "AppType", "TaskId", "CardType", "ResponseCode":
		return true
	default:
		return false
	}
}

func parseCallbackMessage(plain []byte, cred Cred) (CallbackMessage, error) {
	f, err := callbackXMLFields(plain)
	if err != nil {
		return CallbackMessage{}, err
	}
	m := CallbackMessage{ToUserName: f["ToUserName"], FromUserName: f["FromUserName"], MsgType: f["MsgType"], MsgID: f["MsgId"], AgentID: f["AgentID"], Content: f["Content"], Event: f["Event"], EventKey: f["EventKey"], ChangeType: f["ChangeType"], RawXML: string(plain), MediaID: f["MediaId"], ThumbMediaID: f["ThumbMediaId"], PicURL: f["PicUrl"], Format: f["Format"], Title: f["Title"], Description: f["Description"], URL: f["Url"], LocationX: f["Location_X"], LocationY: f["Location_Y"], Scale: f["Scale"], Label: f["Label"], Latitude: f["Latitude"], Longitude: f["Longitude"], Precision: f["Precision"], AppType: f["AppType"], TaskID: f["TaskId"], CardType: f["CardType"], ResponseCode: f["ResponseCode"]}
	m.CreateTime, err = strconv.ParseInt(f["CreateTime"], 10, 64)
	if err != nil || m.CreateTime <= 0 || m.ToUserName != cred.CorpID || m.FromUserName == "" || len(m.FromUserName) > 256 || m.MsgType == "" || len(m.MsgType) > 64 || len(m.MsgID) > 128 || len(m.Event) > 128 || len(m.EventKey) > 4096 {
		return CallbackMessage{}, errors.New("invalid callback identity")
	}
	if m.AgentID != "" && m.AgentID != cred.AgentID {
		return CallbackMessage{}, errors.New("callback application does not match")
	}
	if m.AgentID == "" && (m.MsgType != "event" || (m.FromUserName != "sys" && m.Event != "batch_job_result")) {
		return CallbackMessage{}, errors.New("callback application is missing")
	}
	if m.MsgType == "event" && m.Event == "" {
		return CallbackMessage{}, errors.New("callback event is missing")
	}
	if m.MsgType != "event" && m.MsgID == "" {
		return CallbackMessage{}, errors.New("callback message ID is missing")
	}
	return m, nil
}
