// Collaborative login (implemented on the plugin side, declared in the schema with auth.QR()):
// Start kicks off a clawbot QR-code login → returns a QR code data-uri challenge; Poll polls the
// status, and once confirmed, carries out the session (store.Credentials JSON) — the platform
// writes it into the credential row, and this plugin never persists any credential itself. The
// login session lives in process memory (polled while valid; discarded when the panel closes).
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	clawbot "github.com/importcjj/wechat-clawbot-client-go"
	qrcode "github.com/skip2/go-qrcode"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

// The panel's polling window: how long a login session stays in memory.
const authTTL = 5 * time.Minute

// The server-side QR code's real lifetime is about 94 seconds; once it's up, get_qrcode_status
// returns expired immediately, and scanning the stale code again produces no further status change.
// A bit of margin is reported to the panel so it re-kicks-off auth_start for a new code before the
// old one actually expires — AuthState carries no QRImage, so poll has no way to send a new code
// back to the frontend.
const qrTTL = 80 * time.Second

type authSession struct {
	mu      sync.Mutex
	status  string // pending | scanned | confirmed | expired
	session string // store.Credentials JSON, once confirmed
	qrURI   string // data:image/png;base64,…
	cancel  context.CancelFunc
}

var authSessions sync.Map // auth_id → *authSession

func (a *authSession) set(status, session string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	// A terminal state (confirmed/expired) is never overwritten by a later callback (e.g. OnQRExpired
	// firing after Wait has already returned).
	if a.status == "confirmed" || a.status == "expired" {
		return
	}
	a.status = status
	if session != "" {
		a.session = session
	}
}

func (a *authSession) snapshot() (string, string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.status, a.session
}

// —— Kicking off a QR-code login ——

func opAuthStart(_ sokel.Ctx) (*sokel.AuthChallenge, error) {
	authID := fmt.Sprintf("auth_%d", time.Now().UnixNano())
	as := &authSession{status: "pending"}

	// Login uses its own in-memory store: when login completes, clawbot writes the credentials into
	// it (onLoginComplete → SaveCredentials), and we intercept that through the save hook,
	// serializing it as the session for auth_poll to pick up and hand to the platform to persist.
	ps := newPlatformStore("", func(sessionJSON string) error {
		as.set("confirmed", sessionJSON)
		return nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), authTTL)
	as.cancel = cancel
	client := clawbot.NewDefault(authID, ps, clawbot.WithDefaultEventHooks(clawbot.DefaultEventHooks{
		OnQRScanned: func(string) { as.set("scanned", "") },
		// When the code expires, clawbot swaps in a new one and keeps polling on its own, but the
		// panel is still showing the PNG rendered at the moment auth_start ran — swapping it
		// underneath does nothing visible, so the user is forever scanning a dead code. So this just
		// ends the session here, letting the panel see expired and re-run auth_start from scratch.
		OnQRExpired: func(string, int) {
			as.set("expired", "")
			cancel()
		},
	}))
	ls, err := client.Login(ctx)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("发起扫码登录失败: %w", err)
	}
	png, err := qrcode.Encode(ls.QRCodeURL(), qrcode.Medium, 240)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("生成二维码失败: %w", err)
	}
	as.qrURI = "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	authSessions.Store(authID, as)
	go func() {
		defer cancel()
		if werr := ls.Wait(ctx); werr != nil {
			as.set("expired", "")
			log.Printf("[wechat-claw] 登录会话 %s 结束: %v", authID, werr)
		}
		// Wait succeeding → onLoginComplete already called SaveCredentials → the save hook already
		// set confirmed.
		time.AfterFunc(authTTL, func() { authSessions.Delete(authID) }) // clean up after the polling window
	}()
	return &sokel.AuthChallenge{
		AuthID:    authID, // provided by us: clawbot's login session is indexed by exactly this
		QRImage:   as.qrURI,
		Prompt:    "用微信扫码并确认登录",
		ExpiresIn: int(qrTTL.Seconds()),
	}, nil
}

// —— Polling status ——

func opAuthPoll(_ sokel.Ctx, authID string) (*sokel.AuthState, error) {
	v, ok := authSessions.Load(authID)
	if !ok {
		return &sokel.AuthState{Status: sokel.AuthExpired}, nil
	}
	status, session := v.(*authSession).snapshot()
	out := sokel.AuthState{Status: status}
	if status == sokel.AuthConfirmed && session != "" {
		// Carried out as a RawMessage (an object): the platform json.Marshals session and stores it
		// as fields.session — giving it as a string would wrap it in an extra layer of quotes
		// (double encoding), and the source instance's sessionFromJSON would no longer read it back.
		out.Session = json.RawMessage(session)
	}
	return &out, nil
}
