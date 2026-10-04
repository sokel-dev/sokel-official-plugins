// wechat-claw —— 微信 iLink Bot（wechat-clawbot-client-go）插件：收发一体。
//
//   - 凭证 = 一个微信账号：session 由「凭证管理 → 登录/授权」扫码获得（P2 协作式认证），
//     插件不落地任何凭证（platformStore：读=平台下发，写=sokel.credential.update 回写）。
//   - 多账号单实例：P0 per-credential supervisor 天然覆盖——配几个凭证就跑几个账号。
//   - 事件：message（chat_id 公共字段顶层平铺）；操作：send_text / send_image / send_file。
//   - 发送依赖运行中 client 的 context token（随入站消息进 store）→ 微信组建议单副本部署。
//
// ⚠️ 合规提示：iLink bot API 属非官方通道，有封号风险——建议专号专用。
//
// 运行：SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx SOKEL_NATS_TOKEN=xxx ./wechat-claw
package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen

import (
	"log"
	"os"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Cred：微信账号凭证。session 不手填——扫码登录后由平台写入（协作式认证）。
// —— 发送操作 ——

func opSendText(ctx plugin.Ctx, in *SendTextIn) (*SendTextOut, error) {
	c, err := runningClientFor(ctx.Credential())
	if err != nil {
		return nil, err
	}
	if err := c.SendText(ctx, in.To, in.Text); err != nil {
		return nil, err
	}
	return &SendTextOut{OK: true}, nil
}

func opSendImage(ctx plugin.Ctx, in *SendImageIn) (*SendImageOut, error) {
	c, err := runningClientFor(ctx.Credential())
	if err != nil {
		return nil, err
	}
	data, err := in.Image.Blob(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.SendImage(ctx, in.To, data, in.Caption); err != nil {
		return nil, err
	}
	return &SendImageOut{OK: true}, nil
}

func opSendFile(ctx plugin.Ctx, in *SendFileIn) (*SendFileOut, error) {
	c, err := runningClientFor(ctx.Credential())
	if err != nil {
		return nil, err
	}
	data, err := in.File.Blob(ctx)
	if err != nil {
		return nil, err
	}
	name := in.Filename
	if name == "" {
		name = in.File.Name
	}
	if name == "" {
		name = "file.bin"
	}
	if err := c.SendFile(ctx, in.To, data, name, in.Caption); err != nil {
		return nil, err
	}
	return &SendFileOut{OK: true}, nil
}

func main() {
	token := sokel.Env("TOKEN")
	if token == "" && sokel.Env("DEPLOY_KEY") == "" {
		log.Fatal("请设置 SOKEL_TOKEN(接入组「接入命令」里复制);随部署托管的容器可改设 SOKEL_DEPLOY_KEY 自动注册")
	}
	p := sokel.New(sokel.Config{
		Endpoint: env("SOKEL_ENDPOINT", "http://localhost:8088"),
		Token:    token,
		Name:     "wechat-claw",
	})

	RegisterCredential(p)                    // 凭证契约（schema 声明生成；Cred 在 zz_credential.go）
	p.SetDoc(usageDoc, "")                   // 使用说明（docs/*.md）：凭证怎么拿、有什么坑，随握手上报给平台
	RegisterAuth(p, opAuthStart, opAuthPoll) // 扫码登录：参数表由 schema 声明的步骤决定

	// —— 操作契约 ——（send_* 上画布；auth_* 内部操作走凭证面板登录流，不上画布）
	OnSendText(p, opSendText)
	OnSendImage(p, opSendImage)
	OnSendFile(p, opSendFile)

	// —— 事件契约 ——
	DeclareEvents(p)

	// 常驻事件源：per-credential（每个已登录账号一份长连接）。
	sokel.RegisterSource(p, sokel.Source{ID: "wechat", Label: "微信长连接"}, runWechatSource)

	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
