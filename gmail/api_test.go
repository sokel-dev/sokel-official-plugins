package main

import (
	"net/http"
	"strings"
	"testing"
)

// max_results 的兜底与封顶：不封顶的话，一次列 500 封再逐封拉详情
// 就是把当天配额烧光（messages.get 按封计费）。
func TestListQueryCapsMaxResults(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "20"},   // 不给 → 兜底 20
		{"0", "20"},  // 0 视作没给
		{"-5", "20"}, // 负数同理
		{"50", "50"},
		{"9999", "100"}, // 封顶
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

// 标签是逗号分隔的多值参数，且要容忍空格与空项
// （"INBOX, ,UNREAD" 里那个空项若原样发出去，Gmail 直接 400）。
func TestListQueryLabels(t *testing.T) {
	q := listQuery("is:unread", "INBOX, ,UNREAD,", 10)
	labels := q["labelIds"]
	if len(labels) != 2 || labels[0] != "INBOX" || labels[1] != "UNREAD" {
		t.Errorf("标签解析: %v", labels)
	}
	if q.Get("q") != "is:unread" {
		t.Errorf("搜索条件没带上: %q", q.Get("q"))
	}
	// 空条件不该出现在查询串里（Gmail 对空 q 的行为与不传不同）
	if got := listQuery("", "", 10); got.Has("q") {
		t.Errorf("空搜索条件不该发出去: %v", got)
	}
}

// 401 在这条链路上几乎只有一个含义，错误文案要直接说出来——
// 否则用户看到「Gmail 返回 401」会去查作用域或网络，而实际是授权过期
// （External + 测试中，refresh_token 7 天就过期，这是常态）。
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
	// 未预料的状态码也要带上原文，别吞掉
	if got := (&apiError{Status: 500, Body: "boom"}).Error(); !strings.Contains(got, "boom") {
		t.Errorf("未知错误要保留原文: %q", got)
	}
}

// 没有 token 时**不发请求**，直接给一句能指导操作的话。
// 发出去只会拿回 401，反而看起来像是授权坏了。
func TestGmailGetWithoutTokenFailsFast(t *testing.T) {
	err := gmailGet(t.Context(), "", "/messages", nil, nil)
	if err == nil {
		t.Fatal("无 token 应直接失败")
	}
	if !strings.Contains(err.Error(), "授权") {
		t.Errorf("要提示去凭证页授权, got %v", err)
	}
}

// fmtSscan：小助手，避免为了测试引 strconv 到用例里绕。
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
