package main

import (
	"net/http"
	"strings"
	"testing"
)

// Default and cap for max_results: without a cap, listing 500 messages and then fetching each
// one's detail would burn through the whole day's quota (messages.get is billed per message).
func TestListQueryCapsMaxResults(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "20"},   // not given -> default 20
		{"0", "20"},  // 0 is treated as not given
		{"-5", "20"}, // same for negative numbers
		{"50", "50"},
		{"9999", "100"}, // capped
	}
	for _, c := range cases {
		n := 0
		_, _ = fmtSscan(c.in, &n)
		q := listQuery("", "", n)
		if got := q.Get("maxResults"); got != c.want {
			t.Errorf("maxResults(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

// Labels are a comma-separated multi-value parameter, and must tolerate whitespace and empty
// entries (the empty entry in "INBOX, ,UNREAD" would get Gmail to respond with a flat 400 if
// sent as-is).
func TestListQueryLabels(t *testing.T) {
	q := listQuery("is:unread", "INBOX, ,UNREAD,", 10)
	labels := q["labelIds"]
	if len(labels) != 2 || labels[0] != "INBOX" || labels[1] != "UNREAD" {
		t.Errorf("标签解析: %v", labels)
	}
	if q.Get("q") != "is:unread" {
		t.Errorf("搜索条件没带上: %q", q.Get("q"))
	}
	// An empty condition shouldn't appear in the query string (Gmail treats an empty q
	// differently than not passing it at all)
	if got := listQuery("", "", 10); got.Has("q") {
		t.Errorf("空搜索条件不该发出去: %v", got)
	}
}

// On this path, 401 almost always means one specific thing, and the error text should say it
// directly — otherwise seeing "Gmail returned 401" sends users off checking scopes or the
// network, when the real cause is that authorization expired (common with External + Testing
// mode, where refresh_token expires after 7 days).
func TestAPIErrorMessagesAreActionable(t *testing.T) {
	cases := map[int]string{
		http.StatusUnauthorized:    "重新授权",
		http.StatusForbidden:       "Gmail API",
		http.StatusTooManyRequests: "轮询间隔",
	}
	for code, want := range cases {
		got := (&apiError{Status: code, Body: "{}"}).Error()
		if !strings.Contains(got, want) {
			t.Errorf("HTTP %d 的提示要能指导下一步，got %q（应含 %q）", code, got, want)
		}
	}
	// An unexpected status code should also carry the raw body, not swallow it
	if got := (&apiError{Status: 500, Body: "boom"}).Error(); !strings.Contains(got, "boom") {
		t.Errorf("未知错误要保留原文: %q", got)
	}
}

// With no token, **don't send the request** — give an actionable message right away. Sending it
// would only get a 401 back, which would look like authorization is broken instead.
func TestGmailGetWithoutTokenFailsFast(t *testing.T) {
	err := gmailGet(t.Context(), "", "/messages", nil, nil)
	if err == nil {
		t.Fatal("无 token 应直接失败")
	}
	if !strings.Contains(err.Error(), "授权") {
		t.Errorf("要提示去凭证页授权, got %v", err)
	}
}

// fmtSscan is a small helper that avoids pulling strconv into the test cases just for this.
func fmtSscan(s string, n *int) (int, error) {
	if s == "" {
		return 0, nil
	}
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	v := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, nil
		}
		v = v*10 + int(c-'0')
	}
	if neg {
		v = -v
	}
	*n = v
	return 1, nil
}
