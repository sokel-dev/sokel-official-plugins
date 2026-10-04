// Package schema 声明 linkedin 插件的操作与凭证契约。
//
// 与三个 P0 发布器同一套 publish 契约。LinkedIn 的四件特殊事：
//
//  1. **发的是「个人动态」，不是公司页**。个人号走自助产品「Share on LinkedIn」+
//     w_member_social，**没有审批**；公司页要 w_organization_social + 合作伙伴计划，
//     审批周期以周到月计。所以这个插件只做个人号——把两者塞进一个插件，
//     会让「为什么我发不了公司页」变成一个永远解释不清的问题。
//
//  2. **正文不认 Markdown 也不认 HTML**，是纯文本 + 换行。链接会被 LinkedIn 自动识别并
//     生成卡片（不用自己拼），但**@提及要用 URN**，不是 @名字——所以本插件不做提及。
//
//  3. **图片要先注册再上传**（initializeUpload → PUT 二进制 → 拿 URN），
//     和别家「传完拿 id」不是一个形状。
//
//  4. **访问令牌 60 天到期，且多数自助应用拿不到 refresh_token**。
//     到期就得人再授权一次——凭证的健康检查会在它失效时把状态标成 invalid，
//     「凭证失效」告警会响，不至于悄悄失灵（见 dev-playbook §4.4）。
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/auth"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// PostCreate 发一条动态。
type PostCreate struct{}

func (PostCreate) Meta() contract.Meta {
	return contract.Meta{ID: "li_post_create", Label: "发动态",
		Desc: "以授权账号的个人身份发一条 LinkedIn 动态，可带图", TimeoutSec: 120}
}

func (PostCreate) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("text").Label("正文").
			Desc("纯文本 + 换行（**不认 Markdown 也不认 HTML**）；上限 3000 字符。" +
				"正文里的链接由 LinkedIn 自动识别并生成卡片，不用自己拼"),
		field.Files("images").Label("图片").Desc("最多 9 张；每张 ≤10MB").Optional(),
		field.Strings("image_alts").Label("图片替代文本").Desc("按顺序对应").Optional(),
		field.Enum("visibility",
			field.Opt("PUBLIC", "所有人可见"),
			field.Opt("CONNECTIONS", "仅一度人脉")).
			Label("可见性").Default("PUBLIC"),
	}
}

func (PostCreate) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("id").Label("动态 URN").Desc("形如 urn:li:share:7xxxxxx，删除要用它"),
		field.String("url").Label("链接").Desc("https://www.linkedin.com/feed/update/... 可直接点开"),
	}
}

// PostDelete 删一条动态。
type PostDelete struct{}

func (PostDelete) Meta() contract.Meta {
	return contract.Meta{ID: "li_post_delete", Label: "删动态",
		Desc: "只能删授权账号自己发的", TimeoutSec: 30}
}

func (PostDelete) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("post_id").Label("动态 URN").Desc("来自「发动态」的 id，或动态链接"),
	}
}

func (PostDelete) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("deleted").Label("已删除")}
}

// HealthCheck 凭证还能用吗（平台约定的操作 id）。
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "检查凭证",
		Desc: "查一次授权账号。ok=false 多半是令牌到期了（LinkedIn 的令牌 60 天）", TimeoutSec: 30}
}

func (HealthCheck) Inputs() []contract.FieldSpec { return nil }

func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("凭证可用"),
		field.String("message").Label("说明"),
		field.String("name").Label("账号姓名").Optional(),
	}
}

// Credential 凭证契约。
type Credential struct{}

// AuthMeta：LinkedIn OAuth。
//
// **只申请发个人动态要的那几个**：openid/profile 用来拿账号 URN（发帖的 author 必须是它），
// w_member_social 才是发帖权限。不申请 w_organization_social——那个要合作伙伴审批，
// 混在这儿会让整个授权页过不了。
func (Credential) AuthMeta() contract.AuthMeta {
	return auth.OAuth("linkedin", "openid", "profile", "w_member_social")
}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Secret("access_token").Label("访问令牌（授权注入）").
			Desc("点「授权」后由平台写入，勿手填。**LinkedIn 的令牌 60 天到期**，" +
				"且多数自助应用没有刷新令牌——到期后重新点一次「授权」即可").Optional(),
		field.Secret("refresh_token").Label("刷新令牌（授权注入）").
			Desc("只有申请过 Marketing Developer Platform 的应用才会有；没有是正常的").Optional(),
		field.Text("proxy").Label("出站代理").
			Desc("如 http://127.0.0.1:7897；部署环境直连不了 linkedin.com 时必填").Optional(),
	}
}
