// feishu —— Sokel 第一方插件：飞书自建应用的全能力侧。
//
// 消息/卡片/查人/群管理/云文档/多维表格/网盘 + 长连接事件源（收到消息 / 卡片按钮 /
// bot 进群 → 起工作流）。群 webhook 机器人是另一个插件（feishu-webhook）。
//
// 运行：SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./feishu
package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen

import (
	"log"
	"net/url"
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
		Name:     "feishu",
	})
	RegisterCredential(p)
	p.SetDoc(usageDoc, "")

	// —— 消息 ——
	OnSendText(p, opSendText)
	OnSendMarkdown(p, opSendMarkdown)
	OnSendCard(p, opSendCard)
	OnSendImage(p, opSendImage)
	OnSendFile(p, opSendFile)
	OnReplyMessage(p, opReplyMessage)
	OnRecallMessage(p, opRecallMessage)
	OnUploadImage(p, opUploadImage)
	OnUploadFile(p, opUploadFile)

	// —— 通讯录 / 群 ——
	OnGetUser(p, opGetUser)
	OnListChats(p, opListChats)
	OnCreateChat(p, opCreateChat)
	OnAddChatMembers(p, opAddChatMembers)

	// —— 云文档 / 多维表格 / 网盘 ——
	OnDocxCreate(p, opDocxCreate)
	OnDocxAppend(p, opDocxAppend)
	OnBitableListRecords(p, opBitableListRecords)
	OnBitableCreateRecord(p, opBitableCreateRecord)
	OnBitableUpdateRecord(p, opBitableUpdateRecord)
	OnBitableDeleteRecord(p, opBitableDeleteRecord)
	OnDriveUpload(p, opDriveUpload)

	// —— 保底 / 体检 ——
	OnCall(p, opCall)
	OnHealthCheck(p, opHealthCheck)

	// —— 事件（长连接；per-credential，一条凭证=一个应用=一条连接）——
	DeclareEvents(p)
	sokel.RegisterSource(p, sokel.Source{ID: "events", Label: "飞书长连接事件"}, runEvents)

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

// urlQuery query 参数转义（misc/content 共用）。
func urlQuery(s string) string { return url.QueryEscape(s) }
