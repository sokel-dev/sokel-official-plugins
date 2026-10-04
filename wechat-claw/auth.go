// 协作式登录（插件侧实现，schema 里用 auth.QR() 声明）：Start 发起 clawbot 扫码登录 → 返回二维码
// data-uri 挑战；Poll 轮询状态，confirmed 时带出 session（store.Credentials JSON）——
// 由平台写入凭证行，本插件不落地任何凭证。登录会话存进程内存（有效期内轮询；面板关闭即弃）。
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

// 面板的轮询窗口：登录会话在内存里留这么久。
const authTTL = 5 * time.Minute

// 服务端二维码的真实寿命约 94 秒，到点 get_qrcode_status 立刻返回 expired，
// 旧码再被扫也不会有任何状态变化。留一点余量报给面板，让它在码失效前就重新
// 发起 auth_start 换一张——AuthState 里没有 QRImage，poll 没法把新码送回前端。
const qrTTL = 80 * time.Second

type authSession struct {
	mu      sync.Mutex
	status  string // pending | scanned | confirmed | expired
	session string // confirmed 时的 store.Credentials JSON
	qrURI   string // data:image/png;base64,…
	cancel  context.CancelFunc
}

var authSessions sync.Map // auth_id → *authSession

func (a *authSession) set(status, session string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	// 终态（confirmed/expired）不被后续回调覆盖（如 Wait 返回后又来 OnQRExpired）。
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

// —— 发起扫码 ——

func opAuthStart(_ sokel.Ctx) (*sokel.AuthChallenge, error) {
	authID := fmt.Sprintf("auth_%d", time.Now().UnixNano())
	as := &authSession{status: "pending"}

	// 登录用独立内存 store：登录完成时 clawbot 把 credentials 写进去（onLoginComplete → SaveCredentials），
	// 我们从 save 钩子截获序列化为 session，等 auth_poll 取走交平台落库。
	ps := newPlatformStore("", func(sessionJSON string) error {
		as.set("confirmed", sessionJSON)
		return nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), authTTL)
	as.cancel = cancel
	client := clawbot.NewDefault(authID, ps, clawbot.WithDefaultEventHooks(clawbot.DefaultEventHooks{
		OnQRScanned: func(string) { as.set("scanned", "") },
		// 码过期时 clawbot 会自己换一张新码接着轮询，但面板上挂的是 auth_start
		// 那一刻渲染的 PNG，换了也看不见——用户扫的永远是死码。所以这里直接收场，
		// 让面板按 expired 重新走 auth_start。
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
		// Wait 成功 → onLoginComplete 已 SaveCredentials → save 钩子已置 confirmed。
		time.AfterFunc(authTTL, func() { authSessions.Delete(authID) }) // 轮询窗口后清理
	}()
	return &sokel.AuthChallenge{
		AuthID:    authID, // 自带：clawbot 的登录会话就是按它索引的
		QRImage:   as.qrURI,
		Prompt:    "用微信扫码并确认登录",
		ExpiresIn: int(qrTTL.Seconds()),
	}, nil
}

// —— 轮询状态 ——

func opAuthPoll(_ sokel.Ctx, authID string) (*sokel.AuthState, error) {
	v, ok := authSessions.Load(authID)
	if !ok {
		return &sokel.AuthState{Status: sokel.AuthExpired}, nil
	}
	status, session := v.(*authSession).snapshot()
	out := sokel.AuthState{Status: status}
	if status == sokel.AuthConfirmed && session != "" {
		// 以 RawMessage（对象）形态带出：平台对 session 做 json.Marshal 后存 fields.session——
		// 若给字符串会被再包一层引号（双重编码），源实例 sessionFromJSON 就读不回了。
		out.Session = json.RawMessage(session)
	}
	return &out, nil
}
