// 事件源（per-credential，P0 supervisor 每凭证起一份，源函数可重入——一切状态从 SourceCtx 走）：
//   - 凭证无 session → ReportStatus(auth_required) 等待（面板扫码写入 session 后，心跳发现字段变更
//     → supervisor 自动重启本实例，无需插件自己热加载）；
//   - 有 session → clawbot client Start 长连接：OnMessage → ctx.Trigger("message")；
//     OnSessionExpired → auth_required；token 运行中刷新 → platformStore 经 UpdateCredential 回写平台。
//   - 运行中 client 进「发送注册表」（按 session token 键），send_* 操作复用它（context token 在其 store 里，
//     新建 client 发不了话）——因此微信组建议单副本部署（多副本时 op 可能落到不持有该 bot 的副本）。
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

// running：发送注册表——session token → 运行中的 client（源注册，send_* 操作查取）。
var running sync.Map

// runningClientFor：按调用凭证的 session 定位运行中的 client（send_* 操作用）。
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

// uploadAttachments：把 clawbot 消息附件上传成平台文件引用（单个失败只丢该附件并记日志，不丢整条事件）。
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
		// 未登录：亮「待登录」等平台写入 session（字段变更 → supervisor 重启本实例）。
		ctx.ReportStatus("auth_required", "该凭证尚无微信会话：请在凭证管理点「登录/授权」扫码")
		<-ctx.Done()
		return nil
	}

	ps := newPlatformStore(sessJSON, func(s string) error {
		// 运行中 token 刷新 → 回写平台（唯一凭证存储方）。注意：字段变更会触发 supervisor 重启本实例，
		// 重启后以新 session 起 client——语义正确（新会话生效），代价是一次重连。
		return ctx.UpdateCredential(map[string]string{"session": s})
	})
	// clawbot 内部日志（getUpdates 错误/会话暂停等）走 slog Debug 级全开——排查「收不到消息」必须能看到轮询细节。
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
	// Start 阻塞长轮询；ctx 取消（凭证移除/分片迁移/字段变更重启）随之退出。
	return client.Start(ctx)
}
