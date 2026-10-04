// bluesky —— Sokel 第一方发布器插件：发帖 / 帖串 / 删帖。
//
// **统一 publish 契约的第一个样板**（docs/social-publishing-plugins.md）：
// 发布类操作一律回 id + url，长内容的分段归插件，媒体随发布一起走。
//
// 四条判断见 schema/schema.go 顶部：facets 按字节偏移算、链接卡片默认做、
// 图片随发布传、会话（accessJwt 只活几分钟）由插件自管。
//
// 运行：SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./bluesky
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
		Name:     "bluesky",
	})

	RegisterCredential(p)
	p.SetDoc(usageDoc, "")

	OnBskyPostCreate(p, opPostCreate)
	OnBskyPostThread(p, opPostThread)
	OnBskyPostDelete(p, opPostDelete)
	OnHealthCheck(p, opHealthCheck) // 平台约定：凭证页「测试」与工作流「检查凭证」都调它

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
