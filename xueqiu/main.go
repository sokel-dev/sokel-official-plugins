// xueqiu —— 雪球发帖插件（网页私有接口，非官方 API）。
//
// 雪球没有官方发布接口，这里走的是它网页端自己在调的那几个——**纯 HTTP，不跑浏览器**。
// 代价见 README：无文档、会变、有风控，所以形态定死为「低频 + 失败即报警 + 只发自有真实内容」。
//
// 运行：SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./xueqiu
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
