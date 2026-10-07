// reddit: first-party Reddit plugin on Reddit's public feeds (no API key: Reddit no longer hands those out).
// Search posts, list a subreddit's or a user's posts, read a post's comments; watch the user's posts for new
// comments and a keyword for new posts, as events. See schema/schema.go for the design.
//
// Run: SOKEL_ENDPOINT=https://<platform> SOKEL_TOKEN=skp_xxx ./reddit
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
		Name:     "reddit",
	})

	RegisterCredential(p)
	p.SetDoc(usageDoc, "")

	OnRedditSearch(p, opSearch)
	OnRedditSubredditNew(p, opSubredditNew)
	OnRedditPostComments(p, opPostComments)
	OnRedditUserPosts(p, opUserPosts)
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
