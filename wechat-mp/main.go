// wechat-mp —— Sokel 第一方发布器插件：微信公众号（建草稿 / 发布 / 上传图片）。
//
// 与 bluesky / mastodon / discord 同一套 publish 契约。公众号的四件特殊事见
// schema/schema.go 顶部：两步发布、封面图必填、正文图必须是微信域名的、IP 要在白名单。
//
// 运行：SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./wechat-mp
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
		Name:     "wechat-mp",
	})

	RegisterCredential(p)
	p.SetDoc(usageDoc, "")

	OnMpDraftAdd(p, opDraftAdd)
	OnMpPublish(p, opPublish)
	OnMpImageUpload(p, opImageUpload)
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
