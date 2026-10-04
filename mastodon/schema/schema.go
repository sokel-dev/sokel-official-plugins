// Package schema declares the operation and credential contracts for the mastodon plugin.
//
// Uses the same publish contract as bluesky (docs/social-publishing-plugins.md §3): publishing
// returns id + url, chunking long content is the plugin's job, media travels with publishing. The
// differences all come down to these four points:
//
//  1. **The character limit is whatever the instance says, not 500**. Mastodon is a federated
//     network, and every instance configures its own (mastodon.social uses 500, many
//     Chinese-language instances use 5000, some go up to 11000). Hardcoding 500 would reject a
//     long post the user could otherwise send, so the plugin asks the instance
//     (/api/v2/instance) and caches the answer.
//
//  2. **Publishing carries an idempotency key**. Workflows retry (network hiccups, node reruns),
//     and Mastodon conveniently provides Idempotency-Key (the same key within an hour lands only
//     one post). Without it, a single timeout-and-retry turns into two identical posts on the
//     timeline.
//
//  3. **A content warning (CW) is a first-class citizen**. The convention across the fediverse is
//     to fold sensitive/long content behind a CW, which is especially common for financial
//     commentary. Without this field, the plugin could only post "bare", which gets read as not
//     knowing the etiquette.
//
//  4. **Visibility must be selectable, and replies inherit it by default**. Four levels: public /
//     unlisted / followers-only / direct; later entries in a thread follow the first one by
//     default — without that, mixing public and unlisted in one thread leaves readers seeing only
//     a disjointed half.
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

func visibilityField() contract.FieldSpec {
	return field.Enum("visibility",
		field.Opt("public", "公开（进公共时间线）"),
		field.Opt("unlisted", "不列出（可见但不进公共时间线）"),
		field.Opt("private", "仅关注者"),
		field.Opt("direct", "私信（只有被 @ 的人可见）")).
		Label("可见性").Default("public")
}

// StatusCreate posts a single status.
type StatusCreate struct{}

func (StatusCreate) Meta() contract.Meta {
	return contract.Meta{ID: "masto_status_create", Label: "发嘟文",
		Desc: "发一条 Mastodon 嘟文，可带图、内容警告、可见性与投票", TimeoutSec: 120}
}

func (StatusCreate) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("text").Label("正文").
			Desc("上限由实例决定（多数 500，不少中文实例 5000+）。链接、#话题、@某人 由实例自动识别，不用插件加工"),
		field.String("reply_to_id").Label("回复给（嘟文 id）").Desc("填了就是回复").Optional(),
		field.Text("spoiler_text").Label("内容警告（CW）").
			Desc("填了则正文折叠，先显示这行提示。联邦圈的惯例：敏感/长内容都该折起来").Optional(),
		visibilityField(),
		field.Files("images").Label("图片/视频").Desc("最多 4 个（投票时不能带）").Optional(),
		field.Strings("image_alts").Label("媒体替代文本").Desc("按顺序对应").Optional(),
		field.Bool("sensitive").Label("标记为敏感内容").Desc("图片会先打码，点了才看").Default(false),
		field.Strings("poll_options").Label("投票选项").Desc("2-4 项；填了就不能带媒体").Optional(),
		field.Int("poll_hours").Label("投票时长（小时）").Default(24),
		field.String("language").Label("语言").Desc("ISO 639 码，如 zh、en").Default("zh"),
	}
}

func (StatusCreate) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("id").Label("嘟文 id"),
		field.String("url").Label("链接").Desc("可直接点开或发进通知"),
		field.String("visibility").Label("实际可见性"),
	}
}

// StatusThread posts a chain of statuses.
type StatusThread struct{}

func (StatusThread) Meta() contract.Meta {
	return contract.Meta{ID: "masto_status_thread", Label: "发嘟文串",
		Desc: "把多段文本发成一串首尾相连的嘟文；后续条目默认继承首条的可见性与 CW", TimeoutSec: 300}
}

func (StatusThread) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Strings("texts").Label("每条正文").Desc("按顺序逐条发出，后一条自动回复前一条。本操作不替你断句"),
		field.String("reply_to_id").Label("接在谁后面").Optional(),
		field.Text("spoiler_text").Label("内容警告（CW）").Desc("整串共用").Optional(),
		visibilityField(),
		field.Files("images").Label("首条的媒体").Optional(),
		field.Int("interval_ms").Label("每条之间停顿（毫秒）").Default(500),
		field.String("language").Label("语言").Default("zh"),
	}
}

func (StatusThread) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Strings("ids").Label("各条 id").Desc("按发出顺序；中途失败时是**已经发出去的那几条**"),
		field.String("root_id").Label("首条 id"),
		field.String("root_url").Label("首条链接"),
		field.Int("count").Label("发出条数"),
	}
}

// StatusDelete deletes a single status.
type StatusDelete struct{}

func (StatusDelete) Meta() contract.Meta {
	return contract.Meta{ID: "masto_status_delete", Label: "删嘟文",
		Desc: "只能删授权账号自己发的", TimeoutSec: 30}
}

func (StatusDelete) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("status_id").Label("嘟文 id").Desc("id 或嘟文链接都行")}
}

func (StatusDelete) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("deleted").Label("已删除")}
}

// HealthCheck checks whether the credential still works (the platform-mandated operation id).
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "检查凭证",
		Desc: "验一次访问令牌。ok=false 表示令牌被撤销或作用域不够", TimeoutSec: 30}
}

func (HealthCheck) Inputs() []contract.FieldSpec { return nil }

func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("凭证可用"),
		field.String("message").Label("说明"),
		field.String("account").Label("账号").Optional(),
	}
}

// Credential is the credential contract.
type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("instance_url").Label("实例地址").
			Desc("你的账号所在实例，如 https://mastodon.social 或 https://m.cmx.im"),
		field.Secret("access_token").Label("访问令牌").
			Desc("实例里「偏好设置 → 开发 → 新建应用」，作用域勾 write:statuses、write:media、read:accounts，" +
				"建完在应用详情页复制「你的访问令牌」"),
		field.Text("proxy").Label("出站代理").
			Desc("如 http://127.0.0.1:7897；部署环境直连不了该实例时必填").Optional(),
	}
}
