// Package schema declares the operation and credential contracts for the xueqiu plugin.
//
// **Xueqiu has no official publish API** (its "open platform" only covers market data,
// unrelated to posting — see docs/social-publishing-plugins.md §4). This plugin calls the
// private endpoints the web frontend uses itself — plain HTTP, no browser — and that
// brings three disciplines that differ from other plugins:
//
//  1. **The credential is a cookie, and it expires.** So health_check hits a
//     side-effect-free login-state endpoint, feeding the platform's "expired → alert →
//     human re-login → write back by id" chain (dev-playbook §4.4). Don't use "post a
//     status" to check whether the credential is still alive.
//
//  2. **The text is Xueqiu's own HTML**, not markdown or plain text: paragraphs are
//     <p>, images are the fixed shape <div class="img-single-upload"><img
//     class="ke_img">, and an image must first be uploaded to Xueqiu's own image host to
//     get its address. The plugin handles these conversions.
//
//  3. **AI disclosure is a first-class input.** Xueqiu's posting params include
//     ai_disclose — it should be set to 1 whenever content has been rewritten by an LLM.
//     Making it an explicit toggle instead of hardcoding 0 is a compliance requirement
//     (GEO plan §6), not optional.
//
// **Two things remain unverified** (also noted in the usage doc): whether the
// risk-control param md5__1038 attached to requests is actually required, and where
// session_token comes from. The plugin leaves both open: fetch automatically when
// possible, otherwise let the user paste one into the credential.
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// PostCreate sends one Xueqiu post.
type PostCreate struct{}

func (PostCreate) Meta() contract.Meta {
	return contract.Meta{ID: "xq_post_create", Label: "发帖",
		Desc: "发一条雪球帖子，可带图。正文给纯文本或 markdown 皆可，插件转成雪球的 HTML", TimeoutSec: 180}
}

func (PostCreate) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("text").Label("正文").
			Desc("纯文本/简单 markdown（**加粗**、空行分段）。已经是 HTML 的话原样透传"),
		field.Files("images").Label("图片").
			Desc("先传到雪球图床再嵌进正文，按顺序附在文末").Optional(),
		field.Bool("ai_disclose").Label("声明为 AI 生成").
			Desc("内容经 LLM 改写/生成过就该打开——这是雪球的合规字段，不是可选项").Default(false),
		field.Bool("allow_reward").Label("允许打赏").Default(false),
	}
}

func (PostCreate) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("id").Label("帖子 id"),
		field.String("url").Label("链接").Desc("拼不出时为空，不是错误"),
	}
}

// ArticleDraft writes a long-form article to the draft box (the mp.xueqiu.com API).
//
// **The output is a draft, not a published post**: the endpoint's own semantics are to
// save a draft, leaving the final "publish" step to a human clicking it on the website.
// This also happens to be the safer shape from a compliance standpoint — automated
// drafting, human publishing. Long-form is where research content really belongs; the
// short-post path suits a one-line take.
type ArticleDraft struct{}

func (ArticleDraft) Meta() contract.Meta {
	return contract.Meta{ID: "xq_article_draft", Label: "写长文（存草稿）",
		Desc: "把一篇长文写进雪球草稿箱，可带图；**不会直接发布**，最后一步在网页上点", TimeoutSec: 180}
}

func (ArticleDraft) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("title").Label("标题"),
		field.Text("content").Label("正文").
			Desc("纯文本/简单 markdown（**加粗**、空行分段）。已经是 HTML 的话原样透传"),
		field.Files("images").Label("图片").Desc("传到雪球图床后按顺序附在文末").Optional(),
		field.String("cover_pic").Label("封面图地址").Desc("已经在雪球图床上的地址；留空则没有封面").Optional(),
		field.Bool("private").Label("仅自己可见").Default(false),
	}
}

func (ArticleDraft) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("id").Label("草稿 id").Optional(),
		field.String("edit_url").Label("继续编辑/发布的地址").Desc("打开它检查排版，确认无误再点发布"),
	}
}

// HealthCheck checks whether the credential still works (the platform's standard operation id).
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "检查凭证",
		Desc:       "读一次写作页判断登录态（无副作用），顺带带出 uid 与昵称。ok=false 表示 cookie 过期了",
		TimeoutSec: 30}
}

func (HealthCheck) Inputs() []contract.FieldSpec { return nil }

func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("凭证可用"),
		field.String("message").Label("说明"),
		field.String("uid").Label("用户 id").Optional(),
		field.String("name").Label("昵称").Optional(),
	}
}

// Credential is the credential contract.
//
// **The cookie is the credential**: whoever has it can post as this account, so it must
// be handled like a secret. It expires — this works together with the "check credential +
// expiry alert + human re-login + write back by id" chain.
type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Secret("cookie").Label("Cookie").
			Desc("浏览器里登录雪球后，F12 → Network → 任意请求 → 复制整条 Cookie 值粘进来。" +
				"至少要含 xq_a_token / xqat / xq_id_token / u / device_id"),
		field.Secret("session_token").Label("会话令牌（session_token）").
			Desc("发帖接口要的防重放串。**留空则插件自己去页面上取**；取不到时会在错误里让你手工粘一次" +
				"（F12 → Network → 发一条帖子 → 请求体里的 session_token）").Optional(),
		field.Text("risk_param").Label("风控参数（md5__1038）").
			Desc("网页请求 URL 上挂的那串。**先留空试**——多数情况下不带也能发；" +
				"若报风控/403，把浏览器里那条完整值粘进来").Optional(),
		field.Text("user_agent").Label("User-Agent").
			Desc("留空用一个常见的桌面 Chrome UA。与你取 cookie 的浏览器保持一致更稳").Optional(),
		field.Text("proxy").Label("出站代理").Desc("部署环境直连不了 xueqiu.com 时填").Optional(),
	}
}
