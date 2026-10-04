// The event source (per-credential: the P0 supervisor starts one per credential, and the source
// function is reentrant — all state flows through SourceCtx):
//   - No session on the credential → ReportStatus(auth_required) and wait (once the panel's QR scan
//     writes a session, the heartbeat detects the field change → the supervisor auto-restarts this
//     instance, no hot-reload needed from the plugin itself);
//   - A session present → the clawbot client's Start opens a long connection: OnMessage →
//     ctx.Trigger("message"); OnSessionExpired → auth_required; a token refresh while running →
//     platformStore writes it back to the platform via UpdateCredential.
//   - A running client goes into the "send registry" (keyed by session token), and send_*
//     operations reuse it (the context token lives in its store — a freshly created client can't
//     send messages) — this is why the WeChat group recommends a single-replica deployment (with
//     multiple replicas, an operation might land on a replica that doesn't hold this bot).
package main

import (
	"fmt"
	"log"
	"log/slog"
	"os"
	"sync"

	clawbot "github.com/importcjj/wechat-clawbot-client-go"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

// running is the send registry — session token → the running client (registered by the source,
// looked up by send_* operations).
var running sync.Map

// runningClientFor locates the running client by the session of the credential being used for the
// call (used by send_* operations).
func runningClientFor(cred map[string]string) (*clawbot.DefaultClient, error) {
	c, ok := sessionFromJSON(cred["session"])
	if !ok {
		return nil, fmt.Errorf("该凭证尚未登录（无会话）：请在凭证管理点「登录/授权」扫码")
	}
	v, ok := running.Load(c.Token)
	if !ok {
		return nil, fmt.Errorf("该账号的事件源未在本实例运行（登录后约 20s 内自动启动；多副本部署时请保持微信组单副本）")
	}
	return v.(*clawbot.DefaultClient), nil
}

// uploadAttachments uploads a clawbot message's attachments as platform file references (a single
// failure only drops that attachment and logs it, not the whole event).
func uploadAttachments(ctx plugin.SourceCtx, m *clawbot.Message) (imgs, files []*sokel.File, voice *sokel.File) {
	up := func(name, mime string, data []byte) *sokel.File {
		if len(data) == 0 {
			return nil
		}
		f, err := ctx.Upload(name, mime, data)
		if err != nil {
			log.Printf("[wechat-claw] 附件上传失败（%s）: %v", name, err)
			return nil
		}
		return f
	}
	for i, a := range m.Images {
		name := a.Filename
		if name == "" {
			name = fmt.Sprintf("image_%d_%d.png", m.MessageID, i)
		}
		if f := up(name, a.ContentType, a.Data); f != nil {
			imgs = append(imgs, f)
		}
	}
	for i, a := range m.Files {
		name := a.Filename
		if name == "" {
			name = fmt.Sprintf("file_%d_%d.bin", m.MessageID, i)
		}
		if f := up(name, a.ContentType, a.Data); f != nil {
			files = append(files, f)
		}
	}
	if m.Voice != nil {
		voice = up(fmt.Sprintf("voice_%d.silk", m.MessageID), "audio/silk", m.Voice.Data)
	}
	return
}

func runWechatSource(ctx plugin.SourceCtx) error {
	sessJSON := ctx.Credential()["session"]
	creds, ok := sessionFromJSON(sessJSON)
	if !ok {
		// Not logged in: surface "pending login" and wait for the platform to write a session (a
		// field change → the supervisor restarts this instance).
		ctx.ReportStatus("auth_required", "该凭证尚无微信会话：请在凭证管理点「登录/授权」扫码")
		<-ctx.Done()
		return nil
	}

	ps := newPlatformStore(sessJSON, func(s string) error {
		// A token refresh while running → write it back to the platform (the sole credential store).
		// Note: the field change triggers the supervisor to restart this instance, and after the
		// restart the client starts with the new session — semantically correct (the new session
		// takes effect), at the cost of one reconnect.
		return ctx.UpdateCredential(map[string]string{"session": s})
	})
	// clawbot's internal logging (getUpdates errors, session pauses, etc.) is turned on fully at
	// slog Debug level — troubleshooting "not receiving messages" requires seeing the polling detail.
	slogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	client := clawbot.NewDefault("wc-"+creds.UserID, ps,
		clawbot.WithLogger[struct{}](slogger),
		clawbot.WithDefaultEventHooks(clawbot.DefaultEventHooks{
			OnConnected: func(string) { log.Printf("[wechat-claw] 长轮询已连接（user=%s）", creds.UserID) },
			OnDisconnected: func(_ string, err error) {
				log.Printf("[wechat-claw] 长轮询断开（user=%s）: %v", creds.UserID, err)
			},
			OnMessage: func(_ string, m *clawbot.Message) {
				log.Printf("[wechat-claw] 收到消息 from=%s to=%s id=%d text=%q imgs=%d files=%d", m.From, m.To, m.MessageID, m.Text, len(m.Images), len(m.Files))
				imgs, files, voice := uploadAttachments(ctx, m)
				ev := MessageEvent{
					ChatID: m.From, To: m.To, Text: m.Text, MessageID: int(m.MessageID),
					Images: imgs, Files: files, Voice: voice, Raw: m.Raw,
				}
				if err := ctx.Trigger("message", fmt.Sprintf("%d", m.MessageID), ev); err != nil {
					log.Printf("[wechat-claw] 推送事件失败: %v", err)
				} else {
					log.Printf("[wechat-claw] 事件已推送（message_id=%d）", m.MessageID)
				}
			},
			OnSessionExpired: func(string) {
				log.Printf("[wechat-claw] 会话已失效（user=%s）", creds.UserID)
				ctx.ReportStatus("auth_required", "微信会话已失效：请重新扫码登录")
			},
			OnError: func(_ string, err error) { log.Printf("[wechat-claw] 运行错误: %v", err) },
		}))

	running.Store(creds.Token, client)
	defer running.Delete(creds.Token)
	// Start blocks on the long poll; it exits once ctx is canceled (credential removed/shard
	// migration/restart from a field change).
	return client.Start(ctx)
}
