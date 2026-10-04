// linkedin —— Sokel 第一方发布器插件：发个人动态。
//
// 与三个 P0 发布器同一套 publish 契约。四件特殊事见 schema/schema.go 顶部：
// 只做个人号（公司页要合作伙伴审批）、正文是纯文本、图片三步走、令牌 60 天到期。
//
// 运行：SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./linkedin
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
		Name:     "linkedin",
	})

	RegisterCredential(p)
	p.SetDoc(usageDoc, "")
	RegisterAuth(p) // LinkedIn OAuth：openid + profile + w_member_social

	OnLiPostCreate(p, opPostCreate)
	OnLiPostDelete(p, opPostDelete)
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
