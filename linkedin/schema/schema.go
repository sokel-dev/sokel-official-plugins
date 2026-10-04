// Package schema declares the operation and credential contracts for the linkedin plugin.
//
// Uses the same publish contract as the three P0 publishers. LinkedIn has four quirks:
//
//  1. **This posts a "personal update", not a company page.** A personal profile goes through the
//     self-serve "Share on LinkedIn" product + w_member_social, **with no approval needed**; a
//     company page requires w_organization_social + the partner program, with an approval cycle
//     measured in weeks to months. So this plugin only handles personal profiles — cramming both
//     into one plugin would turn "why can't I post to my company page" into a question that can
//     never be explained well.
//
//  2. **The body accepts neither Markdown nor HTML** — it's plain text + line breaks. Links are
//     auto-detected by LinkedIn and turned into cards (no need to build them yourself), but
//     **@mentions require a URN**, not an @name — so this plugin doesn't support mentions.
//
//  3. **Images must be registered before uploading** (initializeUpload → PUT the binary → get a
//     URN back), not the same shape as other platforms' "upload, get an id back".
//
//  4. **The access token expires in 60 days, and most self-serve apps can't get a refresh_token.**
//     Expiry requires a human to reauthorize — the credential's health check marks it invalid when
//     it fails, and the "credential invalid" alert fires, so it doesn't fail silently (see
//     dev-playbook §4.4).
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/auth"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// PostCreate posts a single update.
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

// PostDelete deletes a single update.
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

// HealthCheck checks whether the credential still works (the platform-mandated operation id).
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

// Credential is the credential contract.
type Credential struct{}

// AuthMeta: LinkedIn OAuth.
//
// **Only requests what's needed to post a personal update**: openid/profile fetch the account
// URN (which a post's author must be), and w_member_social is the actual posting permission.
// w_organization_social isn't requested — that needs partner approval, and mixing it in here
// would make the whole authorization screen fail.
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
