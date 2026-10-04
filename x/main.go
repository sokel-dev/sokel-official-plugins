// x —— Sokel 第一方插件：X（推特）的读写与事件触发。
//
// 五条设计判断（详见 schema/schema.go 顶部）：读按平台的**增量游标**形状而不是照抄 X 的
// next_token、**转推/回复一眼分掉**（kind 是插件推出来的）、**推串是一个操作**、
// **媒体上传独立成操作**、**写操作一律回 id 与链接**。
//
// 认证只有一条路：平台侧 OAuth 2.0 授权（provider=x，PKCE 与 refresh 轮转都在平台，
// 见 server/internal/credential/oauth.go）。X 从 2026-02 起按次计费，
// 每个操作的开销写在说明书里。
//
// 运行：SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./x
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
		Name:     "x",
	})

	RegisterCredential(p)  // 凭证契约（schema 声明生成；Cred 类型在 zz_credential.go）
	p.SetDoc(usageDoc, "") // 使用说明（docs/x.md）：app 怎么建、作用域怎么勾、钱怎么花
	RegisterAuth(p)        // 认证方式：X 的 OAuth 2.0（作用域在 schema 里声明，一次要齐）

	// 发布
	OnXPostCreate(p, opPostCreate)
	OnXPostThread(p, opPostThread)
	OnXPostDelete(p, opPostDelete)
	// 媒体
	OnXMediaUpload(p, opMediaUpload)
	// 互动
	OnXLike(p, opLike)
	OnXUnlike(p, opUnlike)
	OnXRepost(p, opRepost)
	OnXUnrepost(p, opUnrepost)
	OnXBookmark(p, opBookmark)
	OnXUnbookmark(p, opUnbookmark)
	OnXFollow(p, opFollow)
	OnXUnfollow(p, opUnfollow)
	// 读
	OnXSearch(p, opSearch)
	OnXUserTimeline(p, opUserTimeline)
	OnXMentions(p, opMentions)
	OnXListPosts(p, opListPosts)
	OnXPostGet(p, opPostGet)
	OnXUserGet(p, opUserGet)
	// 健康检查（平台约定的操作 id：凭证页「测试」与工作流「检查凭证」都调它）
	OnHealthCheck(p, opHealthCheck)
	// 私信
	OnXDmSend(p, opDMSend)
	OnXDmEvents(p, opDMEvents)
	// 列表
	OnXListMemberAdd(p, opListMemberAdd)
	OnXListMemberRemove(p, opListMemberRemove)

	DeclareEvents(p)
	// 常驻事件源：轮询提及与关键词（X 的实时推送只在 Enterprise 档，见 watch.go）。
	sokel.RegisterSource(p, sokel.Source{ID: "watch", Label: "提及/关键词轮询"}, runWatchSource)

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
