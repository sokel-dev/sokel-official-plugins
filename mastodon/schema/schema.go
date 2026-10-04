// Package schema 声明 mastodon 插件的操作与凭证契约。
//
// 与 bluesky 同一套 publish 契约（docs/social-publishing-plugins.md §3）：
// 发布回 id + url、长内容分段归插件、媒体随发布走。差异全在这四条上：
//
//  1. **字数上限由实例说了算，不是 500**。Mastodon 是联邦网络，每个实例自己配
//     （mastodon.social 是 500，不少中文实例是 5000，有的到 11000）。写死 500 会把
//     一条本可以发的长文拦下来，所以插件去问实例（/api/v2/instance）并缓存。
//
//  2. **发布带幂等键**。工作流会重试（网络抖动、节点重跑），而 Mastodon 恰好提供了
//     Idempotency-Key（一小时内同键只落一条）。不带的话，一次超时重试就是时间线上两条一样的嘟文。
//
//  3. **内容警告（CW）是一等公民**。联邦圈的惯例是敏感/长内容折在 CW 后面，
//     财经观点尤其常见。不给这个字段，插件就只能发「裸嘟」，会被当成不懂规矩。
//
//  4. **可见性要能选，且回复默认继承**。公开/不列出/仅关注者/私信四档；
//     帖串的后续条目默认跟随首条——不跟随的话，一串里混进公开与不列出，
//     读者只能看到断断续续的半串。
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

// StatusCreate 发一条嘟文。
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

// StatusThread 发一串嘟文。
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

// StatusDelete 删一条嘟文。
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

// HealthCheck 凭证还能用吗（平台约定的操作 id）。
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

// Credential 凭证契约。
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
