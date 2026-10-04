// wechat-claw — a WeChat iLink Bot (wechat-clawbot-client-go) plugin: send and receive in one.
//
//   - Credential = one WeChat account: the session is obtained via a QR-code scan in
//     "Credential management → Login/Authorize" (P2 collaborative authentication); the plugin
//     never persists any credential itself (platformStore: reads = issued by the platform, writes
//     = written back via sokel.credential.update).
//   - Multiple accounts in a single instance: naturally covered by the P0 per-credential
//     supervisor — configure as many credentials as accounts you want to run.
//   - Events: message (chat_id flattened to the top level as a shared field); operations:
//     send_text / send_image / send_file.
//   - Sending depends on the running client's context token (rebuilt from inbound messages into
//     the store) → the WeChat group recommends a single-replica deployment.
//
// Compliance warning: the iLink bot API is an unofficial channel and carries a ban risk — a
// dedicated account is recommended.
//
// Run: SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx SOKEL_NATS_TOKEN=xxx ./wechat-claw
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

// Cred: the WeChat account credential. session isn't filled in by hand — the platform writes it
// after a QR-code login (collaborative authentication).
// —— Send operations ——

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

	RegisterCredential(p)                    // credential contract (generated from the schema declaration; Cred lives in zz_credential.go)
	p.SetDoc(usageDoc, "")                   // usage doc (docs/*.md): how to get credentials, what the gotchas are; reported to the platform with the handshake
	RegisterAuth(p, opAuthStart, opAuthPoll) // QR-code login: the parameter list is decided by the steps declared in the schema

	// —— Operation contracts —— (send_* appear on the canvas; auth_* are internal operations that go through the credential panel's login flow, not the canvas)
	OnSendText(p, opSendText)
	OnSendImage(p, opSendImage)
	OnSendFile(p, opSendFile)

	// —— Event contracts ——
	DeclareEvents(p)

	// Standing event source: per-credential (one long connection per logged-in account).
	sokel.RegisterSource(p, sokel.Source{ID: "wechat", Label: "微信长连接"}, runWechatSource)

	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
