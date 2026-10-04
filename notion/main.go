// notion —— Sokel 第一方插件：读写 Notion + 数据源变动触发。
//
// 三条设计判断（详见 schema/schema.go 顶部）：按**数据源**建模（2025-09-03 之后
// database 只是容器）、正文走 **markdown**（块树只作兜底）、属性**两份都给**
// （props 归一化 + properties_raw 原样）。
//
// 认证两种并存：内部集成密钥（凭证里填 ntn_ 开头的 token，自部署最省事）
// 或 OAuth 授权（平台侧 notion provider 代答，见 server/internal/credential/oauth.go）。
//
// 运行：SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./notion
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
		Name:     "notion",
	})

	RegisterCredential(p)  // 凭证契约（schema 声明生成；Cred 类型在 zz_credential.go）
	p.SetDoc(usageDoc, "") // 使用说明（docs/notion.md）：集成怎么建、页面怎么 share、实时事件怎么接
	RegisterAuth(p)        // 认证方式：Notion OAuth（无作用域——权限是用户在同意页上勾页面）

	// 读
	OnNotionSearch(p, opSearch)
	OnNotionPageGet(p, opPageGet)
	OnNotionDBQuery(p, opDBQuery)
	OnNotionDBSchema(p, opDBSchema)
	OnNotionDBList(p, opDBList)
	OnNotionBlockChildren(p, opBlockChildren)
	OnNotionUserList(p, opUserList)
	OnNotionUserGet(p, opUserGet)
	OnNotionCommentList(p, opCommentList)
	OnHealthCheck(p, opHealthCheck) // 凭证页「检查」按钮调它（id 必须是 health_check）
	// 写
	OnNotionPageCreate(p, opPageCreate)
	OnNotionPageUpdate(p, opPageUpdate)
	OnNotionPageContent(p, opPageContent)
	OnNotionPageTrash(p, opPageTrash)
	OnNotionBlockAppend(p, opBlockAppend)
	OnNotionCommentCreate(p, opCommentCreate)
	OnNotionDBCreate(p, opDBCreate)
	OnNotionDBUpdateSchema(p, opDBUpdateSchema)
	OnNotionFileUpload(p, opFileUpload)

	DeclareEvents(p)
	// 常驻事件源：轮询凭证里配的数据源（Notion 的 webhook 装不了自动化，理由见 watch.go）。
	sokel.RegisterSource(p, sokel.Source{ID: "watch", Label: "数据源变动轮询"}, runWatchSource)

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
