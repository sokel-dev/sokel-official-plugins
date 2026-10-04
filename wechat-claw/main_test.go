package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/importcjj/wechat-clawbot-client-go/store"
)

// Session serialization round trip: the credential row's fields.session (object JSON) ↔
// store.Credentials. The platform's auth_flow json.Marshals the session carried out by poll and
// saves it as-is — it must stay in object form (prevents double encoding).
func TestSessionRoundtrip(t *testing.T) {
	in := store.Credentials{Token: "tk", BaseURL: "https://x", UserID: "u1", SavedAt: time.Now().Truncate(time.Second)}
	j := sessionToJSON(in)
	// Simulates the platform side: json.Marshal(json.RawMessage(j)) should keep the object form and
	// not wrap it in quotes again.
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

// platformStore: Save triggers the write-back hook; Load reads it back; SyncBuf/ContextToken work
// from memory.
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
	// A store preloaded with a session (the source-instance path).
	ps2 := newPlatformStore(sessionToJSON(store.Credentials{Token: "tk2"}), nil)
	if c, err := ps2.LoadCredentials(context.Background(), "c"); err != nil || c.Token != "tk2" {
		t.Errorf("预载 session 应可读: %+v %v", c, err)
	}
}

// Send registry: clear errors for "not logged in" / "source not running" (an actionable message
// for the user, not a nil pointer).
func TestRunningClientFor(t *testing.T) {
	if _, err := runningClientFor(map[string]string{}); err == nil {
		t.Error("无 session 应报「请扫码」")
	}
	if _, err := runningClientFor(map[string]string{"session": sessionToJSON(store.Credentials{Token: "ghost"})}); err == nil {
		t.Error("源未运行应报「未在本实例运行」")
	}
}

// auth session state machine: a terminal state isn't overwritten by a callback (once confirmed, a
// later expired must not roll it back).
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
