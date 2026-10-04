// feed —— Sokel 第一方 Feed 订阅插件：一个操作，多个来源，产出统一的 JSON 条目。
//
// 借鉴 RSSHub 的两条判断（统一条目形状、一源一适配器），但**不依赖它的服务**，
// 也**不产出 XML**——详见 schema/schema.go 顶部。
//
// 运行：SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./feed
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
		Name:     "feed",
	})

	RegisterCredential(p)
	p.SetDoc(usageDoc, "")

	OnFeedFetch(p, opFetch)
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
