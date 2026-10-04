// bluesky — a first-party Sokel publisher plugin: post / thread / delete.
//
// **The first template for the unified publish contract**
// (docs/social-publishing-plugins.md): publish operations always return id + url, the
// plugin owns splitting long content, and media travels along with the publish call.
//
// See the top of schema/schema.go for four design decisions: facets are computed by byte
// offset, link cards are built by default, images are sent with the publish call, and the
// session (accessJwt only lives a few minutes) is self-managed by the plugin.
//
// Run: SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./bluesky
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
	OnHealthCheck(p, opHealthCheck) // platform convention: called by both "Test" on the credential page and "Check credential" in workflows

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
