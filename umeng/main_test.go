package main

import (
	"context"
	"encoding/json"
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
	return map[string]string{
		"android_app_key": "andk", "android_master_secret": "andsec",
		"ios_app_key": "iosk", "ios_master_secret": "iossec",
	}
}

// Signature: MD5("POST"+full URL+body+secret), checked against an independent
// implementation; the URL does not include ?sign itself.
func TestSign(t *testing.T) {
	got := sign("https://msgapi.umeng.com/api/send", `{"a":1}`, "sec")
	// python: md5("POST" + url + body + "sec")
	if got != "6fa5dbccea0287bdd8627437ce27ccfa" {
		t.Errorf("sign = %q", got)
	}
}

// Send: cast type is auto-determined; signature is verified by recomputing it with the
// key; checks the Android payload shape.
func TestPushWireShape(t *testing.T) {
	var gotSign, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSign = r.URL.Query().Get("sign")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = io.WriteString(w, `{"ret":"SUCCESS","data":{"task_id":"tk1"}}`)
	}))
	defer srv.Close()
	old := apiBase
	apiBase = srv.URL
	defer func() { apiBase = old }()

	out, err := opPush(newFake(bothApps()), &PushIn{Platform: "android",
		DeviceTokens: "t1,t2", Title: "标题", Body: "内容", Production: true})
	if err != nil {
		t.Fatal(err)
	}
	if out.TaskID != "tk1" {
		t.Errorf("out=%+v", out)
	}
	if gotSign != sign(srv.URL+"/api/send", gotBody, "andsec") {
		t.Error("签名与密钥重算的不一致")
	}
	var req map[string]any
	_ = json.Unmarshal([]byte(gotBody), &req)
	if req["type"] != "listcast" || req["device_tokens"] != "t1,t2" {
		t.Errorf("两个 token 该是 listcast: %v", req)
	}
	pl := req["payload"].(map[string]any)
	if pl["display_type"] != "notification" {
		t.Errorf("Android payload 形状: %v", pl)
	}

	// iOS: payload is an aps structure, and signed with the iOS key pair
	_, err = opPush(newFake(bothApps()), &PushIn{Platform: "ios", DeviceTokens: "tok", Title: "t", Body: "b", Production: true})
	if err != nil {
		t.Fatal(err)
	}
	if gotSign != sign(srv.URL+"/api/send", gotBody, "iossec") {
		t.Error("iOS 要用 iOS 的 master secret 签名")
	}
	_ = json.Unmarshal([]byte(gotBody), &req)
	aps := req["payload"].(map[string]any)["aps"].(map[string]any)
	if aps["alert"].(map[string]any)["title"] != "t" {
		t.Errorf("iOS payload 要是 aps 结构: %v", req["payload"])
	}
	if req["type"] != "unicast" {
		t.Errorf("单 token 该是 unicast: %v", req["type"])
	}
}

// Over 500 tokens is blocked locally; a missing platform key produces a pointer.
func TestPushValidation(t *testing.T) {
	many := make([]string, 501)
	for i := range many {
		many[i] = fmt.Sprintf("t%d", i)
	}
	_, err := opPush(newFake(bothApps()), &PushIn{DeviceTokens: strings.Join(many, ","), Title: "t", Body: "b"})
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("超 500 要拦, got %v", err)
	}
	_, err = opPush(newFake(map[string]string{"android_app_key": "k", "android_master_secret": "s"}),
		&PushIn{Platform: "ios", Title: "t", Body: "b"})
	if err == nil || !strings.Contains(err.Error(), "iOS") {
		t.Errorf("缺 iOS 组要指路, got %v", err)
	}
}

// health_check: "task not found" = valid credentials (pass); 1003 = wrong credentials
// (fail).
func TestHealthCheckSemantics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"ret":"FAIL","data":{"error_code":"2000","error_msg":"task not found"}}`)
	}))
	old := apiBase
	apiBase = srv.URL
	out, _ := opHealthCheck(newFake(bothApps()), &HealthCheckIn{})
	apiBase = old
	srv.Close()
	if !out.OK {
		t.Errorf("2000 任务不存在应判定钥匙有效: %+v", out)
	}

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"ret":"FAIL","data":{"error_code":"1003","error_msg":"sign error"}}`)
	}))
	apiBase = srv2.URL
	out, _ = opHealthCheck(newFake(bothApps()), &HealthCheckIn{})
	apiBase = old
	srv2.Close()
	if out.OK || !strings.Contains(out.Message, "appkey") {
		t.Errorf("1003 要判失败并指路: %+v", out)
	}

	// **2000 is a catch-all code**: a fake appKey also returns 2000 (msg "该应用已被禁用",
	// observed in practice) — judging by code alone would let a fake key through, so it
	// must be judged by msg.
	srv3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"ret":"FAIL","data":{"error_code":"2000","error_msg":"该应用已被禁用"}}`)
	}))
	apiBase = srv3.URL
	out, _ = opHealthCheck(newFake(bothApps()), &HealthCheckIn{})
	apiBase = old
	srv3.Close()
	if out.OK {
		t.Errorf("2000+应用被禁用 要判失败（假钥匙就是这个形状）: %+v", out)
	}
}
