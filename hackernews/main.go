// hackernews: first-party Hacker News plugin. Read lists, items, threads and search; watch for new comments on your
// stories and replies to your comments (the "new comment" event), and for keywords.
//
// HN has no write API, so there is no post or reply operation: see schema/schema.go.
//
// Run: SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./hackernews
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
		Name:     "hackernews",
	})

	RegisterCredential(p)
	p.SetDoc(usageDoc, "")

	OnHnItemGet(p, opItemGet)
	OnHnList(p, opList)
	OnHnSearch(p, opSearch)
	OnHnThreadComments(p, opThreadComments)
	OnHealthCheck(p, opHealthCheck)

	DeclareEvents(p)
	sokel.RegisterSource(p, sokel.Source{ID: "watch", Label: "评论/关键词轮询"}, runWatchSource)

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
