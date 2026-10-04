// Package schema declares the contracts for the feishu-webhook plugin.
//
// Scope: a Feishu **group custom bot** — the shortest path to sending a message to "that one
// group". No app to build, no admin to ask: Group settings → Group bots → Add a custom bot, and
// you have a webhook URL in 30 seconds.
//
// Deliberately split into a separate plugin from the feishu main plugin (self-built app): a group
// webhook's authorization scope (can only post to one group) and an enterprise app (can post to
// the whole company) shouldn't be mixed into one credential pool — the credential type is a
// security boundary. The discord plugin is a structurally identical precedent.
//
// The capability boundary is stated as-is: can only send, only to this one group, can't receive
// messages, rate-limited to 100 messages/minute.
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// Send posts a message to the group.
type Send struct{}

func (Send) Meta() contract.Meta {
	return contract.Meta{ID: "webhook_send", Label: "发到群",
		Desc: "通过群自定义机器人发消息（文本 / Markdown / 卡片三选一，按填了哪个决定）"}
}

func (Send) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("text").Label("文本").Desc("纯文本；填了它就按文本发").Optional(),
		field.String("title").Label("标题").Desc("Markdown 卡片的头；只对 markdown 生效").Optional(),
		field.Text("markdown").Label("Markdown").
			Desc("以卡片渲染：**加粗**、[链接](url)、列表、代码块。告警/报告用它").Optional(),
		field.Object("card", "飞书交互卡片 JSON，形状由飞书卡片文档定义；填了它优先于 text/markdown").
			Label("卡片 JSON").Optional(),
		field.Bool("at_all").Label("@所有人").Desc("只对文本生效").Default(false),
	}
}

func (Send) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("成功")}
}

// HealthCheck is the platform-mandated credential health check.
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "检查凭证", TimeoutSec: 30,
		Desc: "向 webhook 发一次空校验请求判断 URL 是否有效（不会在群里发出消息）"}
}

func (HealthCheck) Inputs() []contract.FieldSpec { return nil }

func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("可用"),
		field.String("message").Label("说明"),
	}
}

// Credential is a group bot credential: one credential = one bot in one group.
type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Secret("webhook_url").Label("Webhook 地址").
			Desc("群设置 → 群机器人 → 添加「自定义机器人」后复制（open.feishu.cn/open-apis/bot/v2/hook/…）。它本身就是钥匙，按密钥保管"),
		field.Secret("secret").Label("签名密钥").
			Desc("机器人安全设置若开了「签名校验」，把密钥粘这里；没开留空").Optional(),
	}
}
