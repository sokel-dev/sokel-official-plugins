package main

// httptest fakes SUBMAIL to exercise form shapes and error translation. Real SMS sends cost money, so integration testing goes through operation:test.

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

type fakeCtx struct {
	context.Context
	cred map[string]string
}

func newFake(cred map[string]string) *fakeCtx {
	return &fakeCtx{Context: context.Background(), cred: cred}
}

func (f *fakeCtx) Credential() map[string]string { return f.cred }
func (f *fakeCtx) Upload(string, string, []byte) (*plugin.File, error) {
	return &plugin.File{ID: "f"}, nil
}
func (f *fakeCtx) UploadReader(string, string, io.Reader) (*plugin.File, error) {
	return &plugin.File{ID: "f"}, nil
}
func (f *fakeCtx) Fetch(*plugin.File) ([]byte, error) { return nil, nil }

func bothApps() map[string]string {
	return map[string]string{"sms_appid": "10001", "sms_appkey": "key-cn", "intl_appid": "20002", "intl_appkey": "key-intl"}
}

// Domestic send: the path / form keys / plaintext signature are all pinned; international send must use the international key pair.
func TestSendWireShape(t *testing.T) {
	var path string
	var form map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		path, form = r.URL.Path, r.PostForm
		_, _ = io.WriteString(w, `{"status":"success","send_id":"abc123","fee":2}`)
	}))
	defer srv.Close()
	old := apiBase
	apiBase = srv.URL
	defer func() { apiBase = old }()

	out, err := opSmsSend(newFake(bothApps()), &SmsSendIn{To: "13800138000", Content: "【测试】你好"})
	if err != nil {
		t.Fatal(err)
	}
	if out.SendID != "abc123" || out.Fee != 2 {
		t.Errorf("out=%+v", out)
	}
	if path != "/sms/send" {
		t.Errorf("path=%s", path)
	}
	if form["appid"][0] != "10001" || form["signature"][0] != "key-cn" {
		t.Errorf("国内要用国内那对钥匙: %v", form)
	}

	if _, err := opIntlSend(newFake(bothApps()), &IntlSendIn{To: "+818012345678", Content: "hello"}); err != nil {
		t.Fatal(err)
	}
	if path != "/internationalsms/send" || form["appid"][0] != "20002" || form["signature"][0] != "key-intl" {
		t.Errorf("国际要用国际那对钥匙与路径: path=%s form=%v", path, form)
	}
}

// Template variables must be serialized to a JSON string and put in the vars form key.
func TestXsendVars(t *testing.T) {
	var form map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		form = r.PostForm
		_, _ = io.WriteString(w, `{"status":"success","send_id":"x","fee":1}`)
	}))
	defer srv.Close()
	old := apiBase
	apiBase = srv.URL
	defer func() { apiBase = old }()

	_, err := opSmsXsend(newFake(bothApps()), &SmsXsendIn{To: "13800138000", Project: "abc12",
		Vars: map[string]any{"code": "123456"}})
	if err != nil {
		t.Fatal(err)
	}
	if form["project"][0] != "abc12" || !strings.Contains(form["vars"][0], `"code":"123456"`) {
		t.Errorf("form=%v", form)
	}
}

// Two kinds of errors rejected up front: a missing domestic 【signature】 and a missing
// international country code -- neither should be discovered only after hitting SUBMAIL.
func TestLocalValidation(t *testing.T) {
	ctx := newFake(bothApps())
	if _, err := opSmsSend(ctx, &SmsSendIn{To: "13800138000", Content: "没有签名的内容"}); err == nil || !strings.Contains(err.Error(), "签名") {
		t.Errorf("缺【签名】要提前拦, got %v", err)
	}
	if _, err := opIntlSend(ctx, &IntlSendIn{To: "13800138000", Content: "hi"}); err == nil || !strings.Contains(err.Error(), "国家码") {
		t.Errorf("国际缺 + 国家码要提前拦, got %v", err)
	}
}

// When only one app group is configured, using the other side's operation must point the way, not just return error 101.
func TestMissingAppGroup(t *testing.T) {
	cnOnly := map[string]string{"sms_appid": "10001", "sms_appkey": "k"}
	if _, err := opIntlSend(newFake(cnOnly), &IntlSendIn{To: "+8613800138000", Content: "hi"}); err == nil || !strings.Contains(err.Error(), "国际短信应用") {
		t.Errorf("缺国际组要指路, got %v", err)
	}
}

// Error code translation.
func TestErrTranslation(t *testing.T) {
	for code, want := range map[int]string{101: "两个应用", 152: "充值", 406: "审核", 254: "已报备签名"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, fmt.Sprintf(`{"status":"error","code":%d,"msg":"x"}`, code))
		}))
		old := apiBase
		apiBase = srv.URL
		_, err := opSmsSend(newFake(bothApps()), &SmsSendIn{To: "13800138000", Content: "【测】hi"})
		apiBase = old
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("code %d 要说「%s」, got %v", code, want, err)
		}
	}
}

// SUBMAIL gives balance numbers as strings ("balance":"12345"), which must be parseable.
func TestBalanceStringNumber(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"status":"success","balance":"12345","transactional_balance":"678"}`)
	}))
	defer srv.Close()
	old := apiBase
	apiBase = srv.URL
	defer func() { apiBase = old }()
	out, err := opBalance(newFake(bothApps()), &BalanceIn{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Balance != 12345 || out.Transactional != 678 {
		t.Errorf("out=%+v", out)
	}
}

// Balance: domestic and international each hit their own endpoint with their own key --
// hitting /balance/sms with the international key gets misjudged as a bad credential (a fixed
// bug, pinned here). International is by amount (decimal), domestic is by message count.
func TestBalanceBothEndpoints(t *testing.T) {
	var hits []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		hits = append(hits, r.URL.Path+"|"+r.PostForm.Get("appid"))
		if strings.Contains(r.URL.Path, "international") {
			_, _ = io.WriteString(w, `{"status":"success","balance":"12.58"}`)
			return
		}
		_, _ = io.WriteString(w, `{"status":"success","balance":"300","transactional_balance":"40"}`)
	}))
	defer srv.Close()
	old := apiBase
	apiBase = srv.URL
	defer func() { apiBase = old }()

	out, err := opBalance(newFake(bothApps()), &BalanceIn{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Balance != 300 || out.Transactional != 40 || out.IntlBalance != 12.58 {
		t.Errorf("out=%+v", out)
	}
	want := []string{"/balance/sms|10001", "/balance/internationalsms|20002"}
	if len(hits) != 2 || hits[0] != want[0] || hits[1] != want[1] {
		t.Errorf("hits=%v want %v", hits, want)
	}

	// With only international configured: don't hit the domestic endpoint, and don't error.
	hits = nil
	intlOnly := map[string]string{"intl_appid": "20002", "intl_appkey": "k"}
	out, err = opBalance(newFake(intlOnly), &BalanceIn{})
	if err != nil || out.IntlBalance != 12.58 || len(hits) != 1 {
		t.Errorf("只配国际: out=%+v err=%v hits=%v", out, err, hits)
	}
}

// sms_log goes through the v4 gateway (the old gateway returns "Unknown method" in practice); at least one of send_id/to is required.
func TestSmsLogV4Gateway(t *testing.T) {
	var path string
	var form map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		path, form = r.URL.Path, r.PostForm
		_, _ = io.WriteString(w, `{"status":"success","results":[{"send_id":"abc","to":"138","status":"dropped","report":"DELIVRD_FAIL:BLACKLIST"}]}`)
	}))
	defer srv.Close()
	old := apiBaseV4
	apiBaseV4 = srv.URL
	defer func() { apiBaseV4 = old }()

	out, err := opSmsLog(newFake(bothApps()), &SmsLogIn{SendID: "abc"})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/sms/log" || form["send_id"][0] != "abc" || form["start_date"] == nil {
		t.Errorf("path=%s form=%v", path, form)
	}
	if out.Count != 1 {
		t.Errorf("out=%+v", out)
	}
	row, _ := out.Logs[0].(map[string]any)
	if row["status"] != "dropped" {
		t.Errorf("status 要原样透传: %+v", row)
	}
	if _, err := opSmsLog(newFake(bothApps()), &SmsLogIn{}); err == nil || !strings.Contains(err.Error(), "至少填一个") {
		t.Errorf("两个都空要拒绝, got %v", err)
	}
}
