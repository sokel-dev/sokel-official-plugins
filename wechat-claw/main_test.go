package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/importcjj/wechat-clawbot-client-go/store"
)

// session 序列化往返：凭证行 fields.session（对象 JSON）↔ store.Credentials。
// 平台 auth_flow 对 poll 带出的 session 做 json.Marshal 后原样保存——必须是对象形态（防双重编码）。
func TestSessionRoundtrip(t *testing.T) {
	in := store.Credentials{Token: "tk", BaseURL: "https://x", UserID: "u1", SavedAt: time.Now().Truncate(time.Second)}
	j := sessionToJSON(in)
	// 模拟平台侧：json.Marshal(json.RawMessage(j)) 应保持对象形态不再包引号。
	b, _ := json.Marshal(json.RawMessage(j))
	out, ok := sessionFromJSON(string(b))
	if !ok || out.Token != "tk" || out.UserID != "u1" {
		t.Errorf("往返失败: %+v ok=%v", out, ok)
	}
	if _, ok := sessionFromJSON(""); ok {
		t.Error("空 session 应判未登录")
	}
	if _, ok := sessionFromJSON(`"quoted-string"`); ok {
		t.Error("双重编码（字符串形态）应判失败，不得静默通过")
	}
}

// platformStore：Save 触发回写钩子；Load 读回；SyncBuf/ContextToken 内存可用。
func TestPlatformStore(t *testing.T) {
	var saved string
	ps := newPlatformStore("", func(s string) error { saved = s; return nil })
	if _, err := ps.LoadCredentials(context.Background(), "c"); err == nil {
		t.Error("无凭证应报错")
	}
	if err := ps.SaveCredentials(context.Background(), "c", store.Credentials{Token: "tk"}); err != nil {
		t.Fatal(err)
	}
	if saved == "" {
		t.Error("Save 应触发平台回写钩子")
	}
	if c, err := ps.LoadCredentials(context.Background(), "c"); err != nil || c.Token != "tk" {
		t.Errorf("Load 应读回: %+v %v", c, err)
	}
	_ = ps.SaveContextToken(context.Background(), "c", "u1", "ctk")
	if tk, _ := ps.LoadContextToken(context.Background(), "c", "u1"); tk != "ctk" {
		t.Errorf("context token 应可读回: %q", tk)
	}
	// 预载 session 的 store（源实例路径）。
	ps2 := newPlatformStore(sessionToJSON(store.Credentials{Token: "tk2"}), nil)
	if c, err := ps2.LoadCredentials(context.Background(), "c"); err != nil || c.Token != "tk2" {
		t.Errorf("预载 session 应可读: %+v %v", c, err)
	}
}

// 发送注册表：未登录/源未运行的清晰报错（用户可行动的提示，而非空指针）。
func TestRunningClientFor(t *testing.T) {
	if _, err := runningClientFor(map[string]string{}); err == nil {
		t.Error("无 session 应报「请扫码」")
	}
	if _, err := runningClientFor(map[string]string{"session": sessionToJSON(store.Credentials{Token: "ghost"})}); err == nil {
		t.Error("源未运行应报「未在本实例运行」")
	}
}

// auth 会话状态机：终态不被回调覆盖（confirmed 后再来 expired 不得回退）。
func TestAuthSessionTerminal(t *testing.T) {
	as := &authSession{status: "pending"}
	as.set("scanned", "")
	as.set("confirmed", `{"token":"tk"}`)
	as.set("expired", "")
	st, sess := as.snapshot()
	if st != "confirmed" || sess == "" {
		t.Errorf("终态不应被覆盖: %s %q", st, sess)
	}
}
