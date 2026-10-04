// feishu — Sokel's first-party plugin: the full-capability side of a Feishu custom app.
//
// Messages/cards/user lookup/chat management/docs/bitable/drive + a long-connection event
// source (message received / card button clicked / bot added to a chat -> starts a workflow).
// The group webhook bot is a separate plugin (feishu-webhook).
//
// Run: SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./feishu
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

	// —— Messages ——
	OnSendText(p, opSendText)
	OnSendMarkdown(p, opSendMarkdown)
	OnSendCard(p, opSendCard)
	OnSendImage(p, opSendImage)
	OnSendFile(p, opSendFile)
	OnReplyMessage(p, opReplyMessage)
	OnRecallMessage(p, opRecallMessage)
	OnUploadImage(p, opUploadImage)
	OnUploadFile(p, opUploadFile)

	// —— Contacts / chats ——
	OnGetUser(p, opGetUser)
	OnListChats(p, opListChats)
	OnCreateChat(p, opCreateChat)
	OnAddChatMembers(p, opAddChatMembers)

	// —— Docs / Bitable / Drive ——
	OnDocxCreate(p, opDocxCreate)
	OnDocxAppend(p, opDocxAppend)
	OnBitableListRecords(p, opBitableListRecords)
	OnBitableCreateRecord(p, opBitableCreateRecord)
	OnBitableUpdateRecord(p, opBitableUpdateRecord)
	OnBitableDeleteRecord(p, opBitableDeleteRecord)
	OnDriveUpload(p, opDriveUpload)

	// —— Fallback / health check ——
	OnCall(p, opCall)
	OnHealthCheck(p, opHealthCheck)

	// —— Events (long connection; per-credential, one credential = one app = one connection) ——
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

// urlQuery escapes a query parameter (shared by misc.go and content.go).
func urlQuery(s string) string { return url.QueryEscape(s) }
