// Package schema 声明 feishu-webhook 插件的契约。
//
// 定位：飞书**群自定义机器人**——发消息到「那一个群」的最短路径。不建应用、
// 不求管理员：群设置 → 群机器人 → 添加自定义机器人，30 秒拿到一条 webhook URL。
//
// 与 feishu 主插件（自建应用）刻意分成两个插件：一条群 webhook 的授权范围
// （只能发一个群）和一个企业应用（能发全公司）不该混在一个凭证池里——
// 凭证类型是安全边界。discord 插件是同构先例。
//
// 能力边界照实说：只能发、只发这一个群、收不了消息，频控 100 条/分钟。
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// Send 发消息到群。
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

// HealthCheck 平台约定的凭证体检。
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

// Credential 群机器人凭证：一条凭证 = 一个群的一个机器人。
type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Secret("webhook_url").Label("Webhook 地址").
			Desc("群设置 → 群机器人 → 添加「自定义机器人」后复制（open.feishu.cn/open-apis/bot/v2/hook/…）。它本身就是钥匙，按密钥保管"),
		field.Secret("secret").Label("签名密钥").
			Desc("机器人安全设置若开了「签名校验」，把密钥粘这里；没开留空").Optional(),
	}
}
