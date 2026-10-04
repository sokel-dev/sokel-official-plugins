// Package schema declares the operation and credential contracts for the discord plugin.
//
// Same publish contract as bluesky / mastodon, but Discord serves a different purpose:
// **it's for community distribution, not a public discovery channel**. What's posted is visible
// only to channel members, so within this batch of publishers it serves as a "push outlet for
// team/client channels" — pushing a note to the research team's channel once a report is out,
// rather than public exposure in the GEO sense.
//
// Three design decisions:
//
//  1. **Default to webhook, not a bot**. A webhook is just a URL; a channel admin can create one
//     with a few clicks. A bot requires creating an application, configuring intents, inviting it
//     into the server, and managing permissions. For sending messages, a webhook can do everything
//     a bot can, at an order of magnitude lower setup cost.
//
//  2. **The embed card is the primary form**. Financial pushes are "title + summary + link + a few
//     fields" — plain text would flood the channel and be unreadable. So the embed is made a
//     first-class input instead of leaving people to hand-assemble JSON.
//
//  3. **The receipt must include the message id**. Discord's webhook doesn't return a message body
//     by default (204); adding ?wait=true makes it do so. The plugin always sends it — without the
//     id, downstream can't edit or delete this message.
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// MessageSend sends a message.
type MessageSend struct{}

func (MessageSend) Meta() contract.Meta {
	return contract.Meta{ID: "discord_message_send", Label: "发消息",
		Desc: "往频道发一条消息，可带嵌入卡片与附件", TimeoutSec: 60}
}

func (MessageSend) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("content").Label("正文").
			Desc("上限 2000 字符。支持 Markdown；@everyone 之类要频道允许才生效").Optional(),
		field.String("title").Label("卡片标题").Desc("填了就带一张嵌入卡片").Optional(),
		field.Text("description").Label("卡片正文").Desc("上限 4096 字符").Optional(),
		field.String("url").Label("卡片链接").Desc("点标题跳转的地址").Optional(),
		field.String("color").Label("卡片色条").Desc("十六进制，如 #2b6cb0；留空用 Discord 默认").Optional(),
		field.String("image_url").Label("卡片大图地址").Optional(),
		field.Json("fields", map[string]string{}).Label("卡片字段").
			Desc("键值对，如 {\"标的\":\"600519\",\"评级\":\"买入\"}；在卡片里排成两列").Optional(),
		field.String("footer").Label("卡片页脚").Desc("常放来源与时间").Optional(),
		field.Files("files").Label("附件").Desc("随消息上传的文件（图片会直接显示）").Optional(),
		field.String("username").Label("发送者昵称").Desc("覆盖 Webhook 自带的名字（仅 Webhook 模式）").Optional(),
		field.String("thread_id").Label("发进某个话题").Desc("论坛频道/话题的 id；留空发到频道本身").Optional(),
	}
}

func (MessageSend) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("id").Label("消息 id").Desc("改/删这条消息要用它"),
		field.String("channel_id").Label("频道 id"),
		field.String("url").Label("消息链接").Desc("https://discord.com/channels/... 可直接点开"),
	}
}

// MessageDelete deletes a message.
type MessageDelete struct{}

func (MessageDelete) Meta() contract.Meta {
	return contract.Meta{ID: "discord_message_delete", Label: "删消息",
		Desc: "删掉本 Webhook（或本 Bot）自己发的消息", TimeoutSec: 30}
}

func (MessageDelete) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("message_id").Label("消息 id"),
		field.String("thread_id").Label("所在话题").Desc("消息发在话题里时要给").Optional(),
	}
}

func (MessageDelete) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("deleted").Label("已删除")}
}

// HealthCheck checks whether the credential still works (the platform's conventional operation id).
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "检查凭证",
		Desc: "查一次 Webhook 是否还在。ok=false 表示它被删了或地址不对", TimeoutSec: 30}
}

func (HealthCheck) Inputs() []contract.FieldSpec { return nil }

func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("凭证可用"),
		field.String("message").Label("说明"),
		field.String("channel").Label("频道 id").Optional(),
	}
}

// Credential is the credential contract.
type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Secret("webhook_url").Label("Webhook 地址").
			Desc("频道设置 → 整合 → Webhook → 新建，复制那条 https://discord.com/api/webhooks/... 的地址。" +
				"**它本身就是凭据**（拿到就能发消息），所以按密钥保管"),
		field.Text("proxy").Label("出站代理").
			Desc("如 http://127.0.0.1:7897；部署环境直连不了 discord.com 时必填").Optional(),
	}
}
