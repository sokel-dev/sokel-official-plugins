// xueqiu — a Xueqiu posting plugin (the website's private endpoints, not an official API).
//
// Xueqiu has no official publish API, so this calls the handful of endpoints its own web
// frontend uses — **plain HTTP, no browser involved**. The tradeoffs are in the README:
// undocumented, subject to change, and risk-controlled, so the shape is fixed as "low
// frequency + alert on failure + only post genuine, self-authored content".
//
// Run: SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./xueqiu
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
		Name:     "xueqiu",
	})

	RegisterCredential(p)
	p.SetDoc(usageDoc, "")

	OnXqPostCreate(p, opPostCreate)
	OnXqArticleDraft(p, opArticleDraft)
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
