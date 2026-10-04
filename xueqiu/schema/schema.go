// Package schema 声明 xueqiu 插件的操作与凭证契约。
//
// **雪球没有官方发布 API**（它的「开放平台」只有行情，与发帖无关，见
// docs/social-publishing-plugins.md §4）。这个插件走的是网页端自己在调的私有接口——
// 纯 HTTP，不跑浏览器，但也因此有三条与别家不同的纪律：
//
//  1. **凭证是 cookie，会过期**。所以 health_check 打的是一个无副作用的登录态接口，
//     配合平台的「失效 → 告警 → 人工重登 → 按 id 写回」那条链路（dev-playbook §4.4）。
//     别拿「发一条帖子」来检查凭证还活着。
//
//  2. **正文是雪球那套 HTML**，不是 markdown 也不是纯文本：段落 <p>、图片是
//     <div class="img-single-upload"><img class="ke_img"> 的固定形状，而图片得先传到
//     它自己的图床拿到地址。这些转换插件包掉。
//
//  3. **AI 披露是一等入参**。雪球的发帖参数里有 ai_disclose——内容经 LLM 改写过就该置 1。
//     把它做成显式开关而不是写死 0：这条压在合规线上（GEO 方案 §6），不是可选项。
//
// **两个未验证的地方**（说明书里也写了）：请求上挂的风控参数 md5__1038 是否必需、
// session_token 从哪来。插件对两者都留了口：能自动取就自动取，取不到就让用户在凭证里粘。
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// PostCreate 发一条雪球帖子。
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

// ArticleDraft 发一篇长文到草稿箱（mp.xueqiu.com 那套接口）。
//
// **产出是草稿不是已发布**：接口本身的语义就是存草稿，最后一步「发布」留给人在网页上点。
// 这也正好是合规上更稳的形态——自动写、人工发。长文是投研内容真正该去的地方，
// 短帖那条路适合的是一句话观点。
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

// HealthCheck 凭证还能用吗（平台约定的操作 id）。
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

// Credential 凭证契约。
//
// **cookie 就是凭据**：拿到它就能以这个账号发帖，所以按密钥保管。
// 它会过期——配合「检查凭证 + 失效告警 + 人工重登 + 按 id 写回」那条链路用。
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
