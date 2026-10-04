// Package schema declares the operation and credential contracts for the wechat-mp (WeChat
// Official Accounts) plugin.
//
// Uses the same publish contract as bluesky / mastodon / discord, but Official Accounts have four
// quirks none of the others do, and getting any one of them wrong means "it's configured but won't
// publish", with an error code that won't tell you why:
//
//  1. **Publishing is two steps: create a draft, then publish the draft.** A media_id sits between
//     them. This is built as two operations rather than one "post article" — because once a draft
//     is created, it can be eyeballed in the Official Account backend, which is exactly the kind of
//     checkpoint financial content should have (whether to wire up human review on the canvas is
//     up to you).
//
//  2. **A cover image is mandatory**, and it has to be uploaded as permanent material first to get
//     a thumb_media_id. Without a cover image, the draft endpoint rejects it outright.
//
//  3. **Images in the body must be on WeChat's own domain.** Hotlinked images never display in the
//     body (anti-hotlinking), so they need to go through "upload image" first to become an
//     mp.weixin.qq.com address before being spliced into the HTML.
//
//  4. **The calling server's public IP must be allowlisted**, or every endpoint returns 40164.
//     This is the easiest one to miss in a container deployment — the egress IP is often not the
//     one you think it is.
//
// Two more boundaries worth knowing: **as of 2025-07, personal accounts and unverified enterprise
// accounts have had publishing permission revoked** (a verified service/subscription account is
// required); and an article sent via freepublish **doesn't enter the message history feed** — it's
// a permanent link, suited to "content storage + driving traffic from elsewhere", not the same
// thing as a mass send.
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// DraftAdd creates a draft.
type DraftAdd struct{}

func (DraftAdd) Meta() contract.Meta {
	return contract.Meta{ID: "mp_draft_add", Label: "建草稿",
		Desc: "把一篇文章写进公众号草稿箱，产出 media_id。可在后台肉眼复核后再发布", TimeoutSec: 120}
}

func (DraftAdd) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("title").Label("标题").Desc("最多 64 字"),
		field.Text("content").Label("正文（HTML）").
			Desc("支持基础 HTML；**不支持 script/iframe/外部样式表**。" +
				"正文里的图片必须先过「上传图片」换成微信域名的地址，外链图一律显示不出来"),
		field.String("thumb_media_id").Label("封面图 media_id").
			Desc("**必填**，来自「上传图片」（用途选「封面」）。没有封面图微信直接拒收"),
		field.String("author").Label("作者").Optional(),
		field.Text("digest").Label("摘要").Desc("最多 120 字；留空则微信截正文前 54 字").Optional(),
		field.String("content_source_url").Label("原文链接").Desc("文末「阅读原文」跳转的地址").Optional(),
		field.Bool("open_comment").Label("开启留言").Default(false),
		field.Bool("only_fans_comment").Label("仅粉丝可留言").Default(false),
	}
}

func (DraftAdd) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("media_id").Label("草稿 media_id").Desc("拿它去「发布草稿」"),
	}
}

// Publish publishes a draft.
type Publish struct{}

func (Publish) Meta() contract.Meta {
	return contract.Meta{ID: "mp_publish", Label: "发布草稿",
		Desc: "把草稿发出去并等到发布完成，产出文章链接。**发出的文章不进历史消息**，是永久链接", TimeoutSec: 300}
}

func (Publish) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("media_id").Label("草稿 media_id").Desc("来自「建草稿」"),
		field.Bool("wait").Label("等到发布完成").
			Desc("关掉则只提交任务、立刻返回 publish_id（拿不到文章链接）。微信的发布是异步的，" +
				"提交成功不代表发布成功——还可能卡在原创声明或平台审核上").Default(true),
	}
}

func (Publish) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("publish_id").Label("发布任务 id"),
		field.String("article_url").Label("文章链接").Desc("等到完成才有；可直接点开或发进通知"),
		field.String("status").Label("发布状态").
			Desc("success 成功 / publishing 发布中 / original_failed 原创失败 / audit_failed 审核不通过 / …"),
		field.String("msg_data_id").Label("消息数据 id").Desc("查阅读量等数据接口要用它").Optional(),
	}
}

// ImageUpload uploads an image.
type ImageUpload struct{}

func (ImageUpload) Meta() contract.Meta {
	return contract.Meta{ID: "mp_image_upload", Label: "上传图片",
		Desc: "封面图 → media_id；正文配图 → 微信域名的图片地址（外链图在正文里显示不出来）", TimeoutSec: 120}
}

func (ImageUpload) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.File("file").Label("图片").Desc("JPG/PNG；封面图 ≤10MB，正文配图 ≤1MB（微信的限制）"),
		field.Enum("purpose",
			field.Opt("cover", "封面（永久素材，回 media_id）"),
			field.Opt("inline", "正文配图（回图片地址）")).
			Label("用途").Desc("两条路的产出不一样：封面要 media_id，正文要地址").Default("inline"),
	}
}

func (ImageUpload) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("media_id").Label("素材 media_id").Desc("用途选「封面」时有值，绑到「建草稿」的封面图上"),
		field.String("url").Label("图片地址").Desc("微信域名的地址，拼进正文 HTML 的 <img src>"),
	}
}

// HealthCheck checks whether the credential still works (the platform-mandated operation id).
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "检查凭证",
		Desc:       "换一次 access_token 并查草稿数——顺带验出「IP 不在白名单」与「账号没有发布权限」两种最常见的坑",
		TimeoutSec: 30}
}

func (HealthCheck) Inputs() []contract.FieldSpec { return nil }

func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("凭证可用"),
		field.String("message").Label("说明"),
		field.Int("draft_count").Label("草稿数").Optional(),
	}
}

// Credential is the credential contract.
type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("app_id").Label("AppID").Desc("公众号后台「设置与开发 → 基本配置」里的开发者 ID"),
		field.Secret("app_secret").Label("AppSecret").
			Desc("同一页的开发者密码。**只在生成时显示一次**，丢了只能重置（重置会让旧的立刻失效）"),
		field.Text("proxy").Label("出站代理").
			Desc("一般不用填（微信在境内）。填了要注意：**白名单认的是出口 IP**，走代理后出口是代理的 IP").Optional(),
	}
}
