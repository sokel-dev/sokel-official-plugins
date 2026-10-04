// discord —— Sokel 第一方发布器插件：往频道发消息。
//
// 与 bluesky / mastodon 同一套 publish 契约，但定位不同：**社群分发，不是公开发现渠道**。
// 研报出来推一条到投研群用它；要公开曝光用前两个。
//
// 三条判断见 schema/schema.go 顶部：走 Webhook 不做 Bot、嵌入卡片是主形态、回执要给消息 id。
//
// 运行：SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./discord
package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen

import (
	"log"
	"os"

	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

func main() {
	token := sokel.Env("TOKEN")
	if token == "" && sokel.Env("DEPLOY_KEY") == "" {
		log.Fatal("请设置 SOKEL_TOKEN(接入组「接入命令」里复制);随部署托管的容器可改设 SOKEL_DEPLOY_KEY 自动注册")
	}
	p := sokel.New(sokel.Config{
		Endpoint: env("SOKEL_ENDPOINT", "http://localhost:8088"),
		Token:    token,
		Name:     "discord",
	})

	RegisterCredential(p)
	p.SetDoc(usageDoc, "")

	OnDiscordMessageSend(p, opMessageSend)
	OnDiscordMessageDelete(p, opMessageDelete)
	OnHealthCheck(p, opHealthCheck)

	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
