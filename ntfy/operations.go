package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-official-plugins/ntfy/schema"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var topicPattern = regexp.MustCompile(`^[-_A-Za-z0-9]{1,64}$`)
var idPattern = regexp.MustCompile(`^[A-Za-z0-9]{10,32}$`)

func (c *client) authorize(r *http.Request) error {
	if strings.ContainsAny(c.cred.AccessToken, "\r\n\t ") {
		return errors.New("access token must not contain whitespace")
	}
	if c.cred.AccessToken != "" {
		r.Header.Set("Authorization", "Bearer "+c.cred.AccessToken)
	}
	return nil
}
func (c *client) health(ctx context.Context, _ *HealthIn) (*HealthOut, error) {
	b, h, e := c.request(ctx, "GET", "/v1/health", nil, nil, nil)
	if e != nil {
		return nil, e
	}
	var r struct {
		Healthy *bool `json:"healthy"`
	}
	if e = decodeJSON(b, h, &r); e != nil {
		return nil, e
	}
	if r.Healthy == nil {
		return nil, errors.New("ntfy response is missing healthy")
	}
	return &HealthOut{Healthy: *r.Healthy}, nil
}
func validTopic(s string) error {
	if !topicPattern.MatchString(s) {
		return errors.New("topic must contain 1–64 ASCII letters, digits, underscores or hyphens")
	}
	return nil
}
func validMessage(m schema.Message, topic string) error {
	if !idPattern.MatchString(m.ID) || m.Topic != topic || m.Time <= 0 || m.Event == "" {
		return errors.New("ntfy returned invalid message identity")
	}
	return nil
}
func (c *client) publish(ctx context.Context, in *PublishIn) (*PublishOut, error) {
	if e := validTopic(in.Topic); e != nil {
		return nil, e
	}
	if strings.TrimSpace(in.Message) == "" || len(in.Message) > 4096 || !utf8.ValidString(in.Message) {
		return nil, errors.New("message must contain valid UTF-8 text, up to 4096 bytes")
	}
	o := in.Options
	if o.Title != nil && (len(*o.Title) > 512 || !utf8.ValidString(*o.Title)) {
		return nil, errors.New("title must contain valid UTF-8 text, up to 512 bytes")
	}
	if o.Priority != nil && (*o.Priority < 1 || *o.Priority > 5) {
		return nil, errors.New("priority must be between 1 and 5")
	}
	if len(o.Tags) > 20 {
		return nil, errors.New("at most 20 tags are supported")
	}
	for _, tag := range o.Tags {
		if strings.TrimSpace(tag) == "" || len(tag) > 64 || strings.ContainsAny(tag, ",\r\n") {
			return nil, errors.New("each tag must be nonempty, at most 64 bytes and contain no comma or newline")
		}
	}
	if o.Click != nil {
		u, e := url.Parse(*o.Click)
		if e != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
			return nil, errors.New("click must be an HTTP(S) URL without credentials")
		}
	}
	payload := struct {
		Topic    string   `json:"topic"`
		Message  string   `json:"message"`
		Title    *string  `json:"title,omitempty"`
		Priority *int     `json:"priority,omitempty"`
		Tags     []string `json:"tags,omitempty"`
		Click    *string  `json:"click,omitempty"`
	}{in.Topic, in.Message, o.Title, o.Priority, o.Tags, o.Click}
	raw, e := json.Marshal(payload)
	if e != nil {
		return nil, errors.New("could not encode notification")
	}
	headers := http.Header{"Firebase": []string{"no"}}
	if o.Cache != nil {
		if *o.Cache {
			headers.Set("Cache", "yes")
		} else {
			headers.Set("Cache", "no")
		}
	}
	b, h, e := c.request(ctx, "POST", "/", nil, raw, headers)
	if e != nil {
		return nil, e
	}
	var m schema.Message
	if e = decodeJSON(b, h, &m); e != nil {
		return nil, e
	}
	if e = validMessage(m, in.Topic); e != nil {
		return nil, e
	}
	if m.Event != "message" {
		return nil, errors.New("ntfy did not acknowledge a published message")
	}
	return &PublishOut{Message: m}, nil
}

type pollCursor struct {
	Version int      `json:"v"`
	Topic   string   `json:"topic"`
	Time    int64    `json:"time"`
	Seen    []string `json:"seen"`
}

func parseCursor(raw, topic string) (pollCursor, error) {
	var c pollCursor
	if len(raw) > 200000 {
		return c, errors.New("notification cursor exceeds its size limit")
	}
	b, e := base64.RawURLEncoding.DecodeString(raw)
	if e != nil || json.Unmarshal(b, &c) != nil || c.Version != 1 || c.Topic != topic || c.Time <= 0 || c.Time > 9999999999 || len(c.Seen) == 0 || len(c.Seen) > 4096 {
		return c, errors.New("invalid notification cursor for this topic")
	}
	seen := map[string]bool{}
	for _, id := range c.Seen {
		if !idPattern.MatchString(id) || seen[id] {
			return c, errors.New("invalid notification cursor IDs")
		}
		seen[id] = true
	}
	return c, nil
}
func encodeCursor(c pollCursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}
func validSince(s string) bool {
	if s == "all" {
		return true
	}
	if n, e := strconv.ParseInt(s, 10, 64); e == nil && n > 0 && n <= 9999999999 {
		return true
	}
	d, e := time.ParseDuration(s)
	return e == nil && d > 0
}
func (c *client) poll(ctx context.Context, in *PollIn) (*PollOut, error) {
	if e := validTopic(in.Topic); e != nil {
		return nil, e
	}
	since := in.Since
	if since == "" {
		since = "all"
	}
	if !validSince(since) {
		return nil, errors.New("since must be a positive Unix timestamp, duration, or all; use cursor for continuation")
	}
	current := pollCursor{Version: 1, Topic: in.Topic, Seen: []string{}}
	known := map[string]bool{}
	if in.Cursor != "" {
		if in.Since != "" {
			return nil, errors.New("cursor and since cannot be combined")
		}
		var e error
		current, e = parseCursor(in.Cursor, in.Topic)
		if e != nil {
			return nil, e
		}
		since = strconv.FormatInt(current.Time-1, 10)
		for _, id := range current.Seen {
			known[id] = true
		}
	}
	limit := in.Limit
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 1000 {
		return nil, errors.New("limit must be between 1 and 1000")
	}
	b, h, e := c.request(ctx, "GET", "/"+in.Topic+"/json", url.Values{"poll": {"1"}, "since": {since}}, nil, nil)
	if e != nil {
		return nil, e
	}
	if h.Get("X-Messages-Truncated") == "1" {
		return nil, errors.New("ntfy dropped older cached messages; narrow the initial time window or increase the server replay limit")
	}
	ct, _, e := mime.ParseMediaType(h.Get("Content-Type"))
	if e != nil || (ct != "application/x-ndjson" && ct != "application/json" && ct != "application/ndjson") {
		return nil, errors.New("expected ntfy JSON notification stream")
	}
	out := &PollOut{Messages: []schema.Message{}, NextCursor: in.Cursor}
	dec := json.NewDecoder(bytes.NewReader(b))
	received := map[string]bool{}
	pending := []schema.Message{}
	for {
		var m schema.Message
		e = dec.Decode(&m)
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, errors.New("ntfy returned an invalid JSON notification stream")
		}
		if m.Event == "open" || m.Event == "keepalive" {
			continue
		}
		if e = validMessage(m, in.Topic); e != nil {
			return nil, e
		}
		if m.Time > 9999999999 {
			return nil, errors.New("ntfy returned an unsupported notification timestamp")
		}
		if received[m.ID] {
			return nil, errors.New("ntfy repeated an ID within one response")
		}
		received[m.ID] = true
		if m.Time < current.Time || (m.Time == current.Time && known[m.ID]) {
			continue
		}
		pending = append(pending, m)
	}
	// Whole-second timestamps are not unique. Preserve IDs already delivered in the
	// boundary second instead of relying on ntfy's intentionally replaying ID marker.
	sort.SliceStable(pending, func(i, j int) bool {
		if pending[i].Time == pending[j].Time {
			return pending[i].ID < pending[j].ID
		}
		return pending[i].Time < pending[j].Time
	})
	if len(pending) > limit {
		out.HasMore = true
		pending = pending[:limit]
	}
	for _, m := range pending {
		if m.Time > current.Time {
			current.Time = m.Time
			current.Seen = []string{}
		}
		current.Seen = append(current.Seen, m.ID)
	}
	if len(current.Seen) > 4096 {
		return nil, errors.New("more than 4096 messages share a cursor second; use a durable queue or partition this topic")
	}
	if len(pending) > 0 {
		sort.Strings(current.Seen)
		out.NextCursor = encodeCursor(current)
	}
	out.Messages = pending
	return out, nil
}
