package main

// httptest 假 SUBMAIL 打穿表单形状与错误翻译。真发短信要花钱，联调走 operation:test。

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

// 国内发送：路径 / 表单键 / 明文签名 都钉住；国际发送要用国际那对钥匙。
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

// 模板变量要序列化成 JSON 串放进 vars 表单键。
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

// 提前拦的两类错：国内缺【签名】、国际缺国家码——都不该打到 SUBMAIL 才发现。
func TestLocalValidation(t *testing.T) {
	ctx := newFake(bothApps())
	if _, err := opSmsSend(ctx, &SmsSendIn{To: "13800138000", Content: "没有签名的内容"}); err == nil || !strings.Contains(err.Error(), "签名") {
		t.Errorf("缺【签名】要提前拦, got %v", err)
	}
	if _, err := opIntlSend(ctx, &IntlSendIn{To: "13800138000", Content: "hi"}); err == nil || !strings.Contains(err.Error(), "国家码") {
		t.Errorf("国际缺 + 国家码要提前拦, got %v", err)
	}
}

// 只配了一组应用时，用另一边的操作要指路，而不是 101。
func TestMissingAppGroup(t *testing.T) {
	cnOnly := map[string]string{"sms_appid": "10001", "sms_appkey": "k"}
	if _, err := opIntlSend(newFake(cnOnly), &IntlSendIn{To: "+8613800138000", Content: "hi"}); err == nil || !strings.Contains(err.Error(), "国际短信应用") {
		t.Errorf("缺国际组要指路, got %v", err)
	}
}

// 错误码翻译。
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

// 余额的数字 SUBMAIL 以字符串给（"balance":"12345"），要能解。
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

// 余额：国内与国际各打各的端点、各用各的钥匙——国际钥匙打 /balance/sms 会被
// 误判成坏凭证（修过的 bug，钉住）。国际按金额（小数），国内按条。
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

	// 只配国际时：不打国内端点，也不报错。
	hits = nil
	intlOnly := map[string]string{"intl_appid": "20002", "intl_appkey": "k"}
	out, err = opBalance(newFake(intlOnly), &BalanceIn{})
	if err != nil || out.IntlBalance != 12.58 || len(hits) != 1 {
		t.Errorf("只配国际: out=%+v err=%v hits=%v", out, err, hits)
	}
}

// sms_log 走 v4 网关（老网关 Unknown method——实测），send_id/to 至少一个。
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
