// linkedin — a Sokel first-party publisher plugin: posting personal updates.
//
// Uses the same publish contract as the three P0 publishers. The four quirks are covered at the
// top of schema/schema.go: personal profiles only (a company page needs partner approval), plain
// text body, a three-step image flow, and a 60-day token.
//
// Run: SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./linkedin
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
	RegisterAuth(p) // LinkedIn OAuth: openid + profile + w_member_social

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
