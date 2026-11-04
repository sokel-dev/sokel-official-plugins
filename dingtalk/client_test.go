package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func testClient(t *testing.T, send func(*http.Request) string) *client {
	t.Helper()
	return &client{base: "https://api.dingtalk.com", cred: Cred{AppKey: "app", AppSecret: "secret", RobotCode: "robot"}, http: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		body := send(r)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}}
}
func TestGroupMessageWire(t *testing.T) {
	calls := 0
	c := testClient(t, func(r *http.Request) string {
		calls++
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		if r.URL.Path == "/v1.0/oauth2/accessToken" {
			if b["appKey"] != "app" || b["appSecret"] != "secret" {
				t.Fatalf("bad credentials: %#v", b)
			}
			return `{"accessToken":"test-token","expireIn":7200}`
		}
		if r.URL.Path != "/v1.0/robot/groupMessages/send" || r.Method != "POST" || r.Header.Get("x-acs-dingtalk-access-token") != "test-token" {
			t.Fatalf("bad request: %s", r.URL.Path)
		}
		if b["openConversationId"] != "cid" || b["robotCode"] != "robot" || b["msgKey"] != "sampleMarkdown" {
			t.Fatalf("bad message: %#v", b)
		}
		var param map[string]string
		if err := json.Unmarshal([]byte(b["msgParam"].(string)), &param); err != nil || param["text"] != "hello" || param["title"] != "title" {
			t.Fatalf("bad msgParam %#v", b)
		}
		return `{"processQueryKey":"receipt"}`
	})
	out, err := c.GroupMessageSend(context.Background(), &GroupMessageSendIn{ConversationID: "cid", MessageType: "markdown", Title: "title", Text: "hello"})
	if err != nil || out.ProcessQueryKey != "receipt" || calls != 2 {
		t.Fatalf("out=%+v err=%v calls=%d", out, err, calls)
	}
}
func TestDirectPartialFailure(t *testing.T) {
	c := testClient(t, func(r *http.Request) string {
		if strings.Contains(r.URL.Path, "oauth2") {
			return `{"accessToken":"t","expireIn":7200}`
		}
		return `{"processQueryKey":"q","invalidStaffIdList":["bad"],"flowControlledStaffIdList":["slow"]}`
	})
	out, err := c.DirectMessageSend(context.Background(), &DirectMessageSendIn{UserIDs: []string{"ok", "bad", "slow"}, Text: "hello"})
	if err != nil || len(out.InvalidUserIDs) != 1 || out.InvalidUserIDs[0] != "bad" || len(out.RateLimitedUserIDs) != 1 {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

func TestHealthAttachmentAndCache(t *testing.T) {
	count := 0
	c := testClient(t, func(r *http.Request) string {
		if strings.Contains(r.URL.Path, "oauth2") {
			count++
			return `{"accessToken":"token","expireIn":7200}`
		}
		if r.URL.Path != "/v1.0/robot/messageFiles/download" {
			t.Fatal(r.URL.Path)
		}
		return `{"downloadUrl":"https://files.example.invalid/file?signature=temporary"}`
	})
	c.cache = &tokenCache{}
	out, err := c.HealthCheck(context.Background(), &HealthCheckIn{})
	if err != nil || !out.OK {
		t.Fatalf("health=%+v %v", out, err)
	}
	for i := 0; i < 2; i++ {
		out, err := c.MessageFileURL(context.Background(), &MessageFileURLIn{DownloadCode: "code"})
		if err != nil || out.DownloadURL == "" {
			t.Fatalf("attachment=%+v %v", out, err)
		}
	}
	if count != 1 {
		t.Fatalf("token calls %d", count)
	}
	c.cred.AppSecret = ""
	health, err := c.HealthCheck(context.Background(), &HealthCheckIn{})
	if err != nil || health.OK {
		t.Fatal("invalid credentials reported healthy")
	}
}
func TestErrorsAreRedactedAndWritesNotRetried(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{{429, `{"message":"secret token"}`}, {200, `{"code":"invalid","message":"secret token"}`}, {200, `{"errcode":400,"message":"secret token"}`}, {200, `not json`}, {200, `{}`}, {302, `{}`}} {
		t.Run(fmt.Sprintf("%d-%s", tc.status, tc.body), func(t *testing.T) {
			calls := 0
			c := testClient(t, func(r *http.Request) string { return `{}` })
			c.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if strings.Contains(r.URL.Path, "oauth2") {
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"accessToken":"token","expireIn":7200}`)), Header: make(http.Header)}, nil
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
			})
			_, err := c.GroupMessageSend(context.Background(), &GroupMessageSendIn{ConversationID: "cid", Text: "hello"})
			if err == nil || strings.Contains(err.Error(), "secret token") || calls != 2 {
				t.Fatalf("error=%v calls=%d", err, calls)
			}
		})
	}
	c := newClient("https://api.dingtalk.com", Cred{})
	if c.http.CheckRedirect(nil, nil) != http.ErrUseLastResponse {
		t.Fatal("redirects enabled")
	}
}
func TestValidationPreventsRequests(t *testing.T) {
	c := testClient(t, func(*http.Request) string { t.Fatal("unexpected request"); return "" })
	for _, in := range []*GroupMessageSendIn{{Text: "a"}, {ConversationID: "c"}, {ConversationID: "c", Text: "a", MessageType: "markdown"}, {ConversationID: "c", Text: "a", MessageType: "html"}, {ConversationID: "c", Text: strings.Repeat("a", 15001)}} {
		if _, err := c.GroupMessageSend(context.Background(), in); err == nil {
			t.Fatalf("accepted %+v", in)
		}
	}
	for _, ids := range [][]string{nil, {""}, {"a", "a"}, make([]string, 21)} {
		if _, err := c.DirectMessageSend(context.Background(), &DirectMessageSendIn{UserIDs: ids, Text: "hi"}); err == nil {
			t.Fatal("accepted invalid users")
		}
	}
	if _, err := c.MessageFileURL(context.Background(), &MessageFileURLIn{}); err == nil {
		t.Fatal("accepted empty code")
	}
}

func TestRejectPlaintextCredentialEndpoint(t *testing.T) {
	c := testClient(t, func(*http.Request) string { return `{"accessToken":"token","expireIn":7200}` })
	c.base = "http://api.example.invalid"
	out, err := c.HealthCheck(context.Background(), &HealthCheckIn{})
	if err != nil || out.OK {
		t.Fatalf("plaintext credential endpoint was accepted: %+v %v", out, err)
	}
}
