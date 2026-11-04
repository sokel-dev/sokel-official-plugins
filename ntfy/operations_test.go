package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-official-plugins/ntfy/schema"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// Payload from the official subscription documentation; not a live capture.
const documentedMessage = `{"id":"hwQ2YpKdmg","time":1635528741,"event":"message","topic":"mytopic","message":"Disk full"}`

func TestCapturedLocalResponses(t *testing.T) {
	for _, name := range []string{"health", "publish", "poll"} {
		ext := ".json"
		ct := "application/json"
		if name == "poll" {
			ext = ".ndjson"
			ct = "application/x-ndjson"
		}
		raw, e := os.ReadFile("testdata/live-" + name + ext)
		if e != nil {
			t.Fatal(e)
		}
		c, _ := fake(t, string(raw), ct, 200, nil)
		switch name {
		case "health":
			o, e := c.health(context.Background(), &HealthIn{})
			if e != nil || !o.Healthy {
				t.Fatal(o, e)
			}
		case "publish":
			o, e := c.publish(context.Background(), &PublishIn{Topic: "sokel-wave6-synthetic", Message: "Synthetic local-only acceptance message"})
			if e != nil || o.Message.ID != "7zhR3zXX1y80" {
				t.Fatal(o, e)
			}
		case "poll":
			o, e := c.poll(context.Background(), &PollIn{Topic: "sokel-wave6-synthetic"})
			if e != nil || len(o.Messages) != 1 || o.Messages[0].ID != "7zhR3zXX1y80" {
				t.Fatal(o, e)
			}
		}
	}
}

func TestPublishGeneratedDispatch(t *testing.T) {
	c, _ := fake(t, documentedMessage, "application/json", 200, func(r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/prefix/" || r.Header.Get("Authorization") != "Bearer synthetic-test-token" || r.Header.Get("Firebase") != "no" || r.Header.Get("Cache") != "no" || r.GetBody != nil {
			t.Fatal("publish request", r.URL.Path, r.Header)
		}
		var b struct {
			Topic, Message, Title string
			Priority              int
			Tags                  []string
		}
		if json.NewDecoder(r.Body).Decode(&b) != nil || b.Message != "Disk full" || b.Title != "中文通知" || b.Priority != 1 || b.Topic != "mytopic" {
			t.Fatal(b)
		}
	})
	s := invoke(t, c, "publish", `{"topic":"mytopic","message":"Disk full","options":{"title":"中文通知","priority":1,"cache":false,"tags":["warning"]}}`, map[string]string{"access_token": "synthetic-test-token"})
	raw, _ := json.Marshal(s.vars)
	if !strings.Contains(string(raw), `"id":"hwQ2YpKdmg"`) {
		t.Fatal(string(raw))
	}
	if strings.Contains(string(raw), "expires") {
		t.Fatal("omitted fields must stay absent")
	}
}
func TestPollPagingAndControlFrames(t *testing.T) {
	second := strings.ReplaceAll(documentedMessage, "hwQ2YpKdmg", "otherID12345")
	c, _ := fake(t, `{"event":"open"}`+"\n"+documentedMessage+"\n"+second, "application/x-ndjson", 200, func(r *http.Request) {
		if r.URL.Path != "/prefix/mytopic/json" || r.URL.Query().Get("poll") != "1" || r.URL.Query().Get("since") != "all" {
			t.Fatal(r.URL)
		}
	})
	s := invoke(t, c, "poll", `{"topic":"mytopic","limit":1}`, nil)
	if s.vars["has_more"] != true || s.vars["next_cursor"] == "" {
		t.Fatal(s.vars)
	}
	for _, body := range []string{"", documentedMessage, `{"event":"keepalive"}`} {
		c, _ = fake(t, body, "application/json", 200, nil)
		out, e := c.poll(context.Background(), &PollIn{Topic: "mytopic", Since: "10m"})
		if e != nil || out.HasMore {
			t.Fatal(out, e)
		}
	}
	for _, body := range []string{documentedMessage + "\n" + documentedMessage, `{`, strings.ReplaceAll(documentedMessage, "mytopic", "wrong"), `null`, `{"event":"message"}`} {
		c, _ = fake(t, body, "application/x-ndjson", 200, nil)
		if _, e := c.poll(context.Background(), &PollIn{Topic: "mytopic"}); e == nil {
			t.Fatal("bad stream accepted", body)
		}
	}
	c, _ = fake(t, documentedMessage, "application/x-ndjson", 200, nil)
	if _, e := c.poll(context.Background(), &PollIn{Topic: "mytopic", Since: "hwQ2YpKdmg"}); e == nil {
		t.Fatal("repeated input cursor")
	}
	c, _ = fake(t, documentedMessage, "text/html", 200, nil)
	if _, e := c.poll(context.Background(), &PollIn{Topic: "mytopic"}); e == nil {
		t.Fatal("html accepted")
	}
	c.http = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/x-ndjson"}, "X-Messages-Truncated": {"1"}}, Body: io.NopCloser(strings.NewReader(documentedMessage))}, nil
	})}
	if _, e := c.poll(context.Background(), &PollIn{Topic: "mytopic"}); e == nil {
		t.Fatal("truncated cache accepted")
	}
}

func TestSameSecondReplayDeduplicatesAndPreservesNewMessages(t *testing.T) {
	second := strings.ReplaceAll(documentedMessage, "hwQ2YpKdmg", "otherID12345")
	body := documentedMessage + "\n" + second
	c, _ := fake(t, body, "application/x-ndjson", 200, nil)
	first, e := c.poll(context.Background(), &PollIn{Topic: "mytopic", Limit: 1})
	if e != nil || !first.HasMore {
		t.Fatal(first, e)
	}
	c, _ = fake(t, body, "application/x-ndjson", 200, func(r *http.Request) {
		if r.URL.Query().Get("since") != "1635528740" {
			t.Fatal("continuation did not use boundary time", r.URL)
		}
	})
	next, e := c.poll(context.Background(), &PollIn{Topic: "mytopic", Cursor: first.NextCursor, Limit: 1})
	if e != nil || len(next.Messages) != 1 || next.Messages[0].ID != "otherID12345" || next.HasMore {
		t.Fatal(next, e)
	}
	end, e := c.poll(context.Background(), &PollIn{Topic: "mytopic", Cursor: next.NextCursor})
	if e != nil || len(end.Messages) != 0 || end.NextCursor != next.NextCursor {
		t.Fatal(end, e)
	}
	third := strings.ReplaceAll(documentedMessage, "hwQ2YpKdmg", "aNewID12345")
	c, _ = fake(t, body+"\n"+third, "application/x-ndjson", 200, nil)
	added, e := c.poll(context.Background(), &PollIn{Topic: "mytopic", Cursor: end.NextCursor})
	if e != nil || len(added.Messages) != 1 || added.Messages[0].ID != "aNewID12345" {
		t.Fatal(added, e)
	}
	later := strings.ReplaceAll(strings.ReplaceAll(documentedMessage, "hwQ2YpKdmg", "laterID1234"), "1635528741", "1635528742")
	c, _ = fake(t, body+"\n"+third+"\n"+later, "application/x-ndjson", 200, nil)
	advanced, e := c.poll(context.Background(), &PollIn{Topic: "mytopic", Cursor: added.NextCursor})
	if e != nil || len(advanced.Messages) != 1 {
		t.Fatal(advanced, e)
	}
	state, e := parseCursor(advanced.NextCursor, "mytopic")
	if e != nil || len(state.Seen) != 1 || state.Seen[0] != "laterID1234" {
		t.Fatal(state, e)
	}
	for _, in := range []PollIn{{Topic: "other-topic", Cursor: end.NextCursor}, {Topic: "mytopic", Cursor: "malformed"}, {Topic: "mytopic", Cursor: end.NextCursor, Since: "all"}, {Topic: "mytopic", Cursor: strings.Repeat("x", 200001)}} {
		if _, e := c.poll(context.Background(), &in); e == nil {
			t.Fatal("invalid cursor accepted")
		}
	}
	for _, state := range []pollCursor{{Version: 2, Topic: "mytopic", Time: 1, Seen: []string{"hwQ2YpKdmg"}}, {Version: 1, Topic: "mytopic", Time: 1, Seen: []string{"bad"}}, {Version: 1, Topic: "mytopic", Time: 1, Seen: []string{"hwQ2YpKdmg", "hwQ2YpKdmg"}}} {
		if _, e := parseCursor(encodeCursor(state), "mytopic"); e == nil {
			t.Fatal("bad cursor state")
		}
	}
}
func TestValidationAndHealth(t *testing.T) {
	c, n := fake(t, documentedMessage, "application/json", 200, nil)
	badPriority := 0
	long := strings.Repeat("x", 513)
	badClick := "file:///private"
	yes := true
	for _, in := range []PublishIn{{Topic: "../x", Message: "hi"}, {Topic: "topic", Message: ""}, {Topic: "topic", Message: strings.Repeat("x", 4097)}, {Topic: "topic", Message: "hi", Options: schema.PublishOptions{Priority: &badPriority}}, {Topic: "topic", Message: "hi", Options: schema.PublishOptions{Title: &long}}, {Topic: "topic", Message: "hi", Options: schema.PublishOptions{Click: &badClick}}, {Topic: "topic", Message: "hi", Options: schema.PublishOptions{Tags: []string{"bad,tag"}}}} {
		if _, e := c.publish(context.Background(), &in); e == nil {
			t.Fatal("bad publish input")
		}
	}
	for _, in := range []PollIn{{Topic: "bad/topic"}, {Topic: "topic", Since: "-1"}, {Topic: "topic", Since: "invalid"}, {Topic: "topic", Limit: -1}, {Topic: "topic", Limit: 1001}} {
		if _, e := c.poll(context.Background(), &in); e == nil {
			t.Fatal("bad poll input")
		}
	}
	if *n != 0 {
		t.Fatal("invalid input sent")
	}
	for _, since := range []string{"all", "1635528741", "10m"} {
		if !validSince(since) {
			t.Fatal(since)
		}
	}
	c, _ = fake(t, documentedMessage, "application/json", 200, func(r *http.Request) {
		if r.Header.Get("Cache") != "yes" {
			t.Fatal("cache true")
		}
	})
	if _, e := c.publish(context.Background(), &PublishIn{Topic: "mytopic", Message: "hi", Options: schema.PublishOptions{Cache: &yes}}); e != nil {
		t.Fatal(e)
	}
	c, _ = fake(t, `{"healthy":true}`, "application/json", 200, nil)
	s := invoke(t, c, "health", `{}`, nil)
	if s.vars["healthy"] != true {
		t.Fatal(s.vars)
	}
	c, _ = fake(t, `{}`, "application/json", 200, nil)
	if _, e := c.health(context.Background(), &HealthIn{}); e == nil {
		t.Fatal("missing health")
	}
	c.cred.AccessToken = "bad\nsecret"
	if _, e := c.health(context.Background(), &HealthIn{}); e == nil {
		t.Fatal("bad token")
	}
}
func TestLocalServiceAcceptance(t *testing.T) {
	base := os.Getenv("SOKEL_TEST_NTFY_URL")
	if base == "" {
		t.Skip("set SOKEL_TEST_NTFY_URL to a disposable local ntfy service")
	}
	if !strings.HasPrefix(base, "http://127.0.0.1:") {
		t.Fatal("local acceptance requires an explicit loopback service")
	}
	c := newClient(Cred{})
	c.base = base
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	h, e := c.health(ctx, &HealthIn{})
	if e != nil || !h.Healthy {
		t.Fatal(h, e)
	}
	topic := "sokel-test-" + time.Now().Format("150405000000000")
	first, e := c.publish(ctx, &PublishIn{Topic: topic, Message: "Sokel local fixture one"})
	if e != nil {
		t.Fatal(e)
	}
	second, e := c.publish(ctx, &PublishIn{Topic: topic, Message: "Sokel local fixture two"})
	if e != nil {
		t.Fatal(e)
	}
	// ntfy acknowledges publication before its batched cache write is visible.
	// Wait for visibility without publishing again or treating acceptance as delivery.
	deadline := time.Now().Add(5 * time.Second)
	for {
		visible, err := c.poll(ctx, &PollIn{Topic: topic})
		if err != nil {
			t.Fatal(err)
		}
		if len(visible.Messages) == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("published messages did not become visible in the cache")
		}
		time.Sleep(100 * time.Millisecond)
	}
	a, e := c.poll(ctx, &PollIn{Topic: topic, Limit: 1})
	if e != nil || !a.HasMore || len(a.Messages) != 1 {
		t.Fatal(a, e)
	}
	b, e := c.poll(ctx, &PollIn{Topic: topic, Cursor: a.NextCursor})
	if e != nil || len(b.Messages) != 1 || b.Messages[0].ID == a.Messages[0].ID || b.HasMore {
		t.Fatal(b, e)
	}
	end, e := c.poll(ctx, &PollIn{Topic: topic, Cursor: b.NextCursor})
	if e != nil || len(end.Messages) != 0 || end.NextCursor != b.NextCursor {
		t.Fatal(end, e)
	}
	if first.Message.ID == second.Message.ID {
		t.Fatal("publish IDs are not unique")
	}
	t.Log("real ntfy health, publish, same-second deduplication and cursor continuation passed")
}
