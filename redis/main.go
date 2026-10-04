// redis — a first-party Sokel plugin: Redis reads/writes and messaging (works with both self-hosted and
// cloud-hosted instances).
//
// 22 operations cover six data structures (string/hash/list/set/sorted set/Stream) plus a call fallback,
// alongside event sources (Pub/Sub channels, Stream consumption). See the top comment in
// schema/schema.go for the design rationale.
//
// Run: SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./redis
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
		Name:     "redis",
	})
	RegisterCredential(p)
	p.SetDoc(usageDoc, "")

	OnGet(p, opGet)
	OnSet(p, opSet)
	OnDel(p, opDel)
	OnExists(p, opExists)
	OnExpire(p, opExpire)
	OnTTL(p, opTTL)
	OnIncr(p, opIncr)
	OnScan(p, opScan)
	OnHashGetAll(p, opHashGetAll)
	OnHashSet(p, opHashSet)
	OnHashDel(p, opHashDel)
	OnListPush(p, opListPush)
	OnListPop(p, opListPop)
	OnListRange(p, opListRange)
	OnSetAdd(p, opSetAdd)
	OnSetMembers(p, opSetMembers)
	OnSetRemove(p, opSetRemove)
	OnZsetAdd(p, opZsetAdd)
	OnZsetRange(p, opZsetRange)
	OnPublish(p, opPublish)
	OnStreamAdd(p, opStreamAdd)
	OnCall(p, opCall)
	OnHealthCheck(p, opHealthCheck)

	// Event sources: Pub/Sub subscription + Stream consumption (only starts when the credential's
	// watch_channels / watch_streams is set).
	DeclareEvents(p)
	sokel.RegisterSource(p, sokel.Source{ID: "consume", Label: "Redis 消息消费"}, runEvents)

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
