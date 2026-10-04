// redis —— Sokel 第一方插件：Redis 读写与消息（自建 / 云托管通吃）。
//
// 22 个操作覆盖 字符串/哈希/列表/集合/有序集合/Stream 六类结构 + call 保底，
// 外加事件源（Pub/Sub 频道、Stream 消费）。设计判断见 schema/schema.go 顶注。
//
// 运行：SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./redis
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

	// 事件源：Pub/Sub 订阅 + Stream 消费（凭证填了 watch_channels / watch_streams 才启动）。
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
