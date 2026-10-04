// Package schema declares the operation and credential contracts for the bluesky plugin.
//
// This is **the first publisher plugin**, the template for the unified publish contract
// (docs/social-publishing-plugins.md §3): publish operations always return `id` + `url`,
// the plugin owns splitting long content, and media travels along with the publish call.
//
// Four design decisions:
//
//  1. **Rich text has to be computed inside the plugin.** AT Protocol doesn't
//     auto-detect links/hashtags/mentions in the text — facets must be given explicitly,
//     and a facet's index is a **UTF-8 byte offset**. Posting text that mixes CJK
//     characters with a link and indexing by character count would misplace the link or
//     even fail to post. Every user would hit this, so the plugin handles it.
//
//  2. **Link cards are built by default.** Eight times out of ten, financial content is
//     "one sentence + one link", and a cardless link is just a bare URL on the timeline.
//     So whenever a link is given, the plugin fetches its OG info once and builds an
//     external embed from it; if that fails, it falls back to a plain text link —
//     **that failure must not fail the publish**.
//
//  3. **Images are sent along with the publish call**, not as a separate upload
//     operation. Bluesky caps images at 2MB with up to 4 per post, which one call can
//     handle in full; a blob reference is also an opaque structure that would leave
//     people confused what to do with it on the canvas (X's video needs chunking +
//     transcoding, which is what would justify a standalone operation).
//
//  4. **The session is self-managed inside the plugin.** The accessJwt from
//     createSession only lives a few minutes, while refreshJwt is the long-lived
//     credential. Having the user store a token in the credential would mean manually
//     swapping it out every few minutes — so the credential instead stores "identifier +
//     app password", and the short-lived session is cached and renewed by the plugin.
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// PostCreate sends one post.
type PostCreate struct{}

func (PostCreate) Meta() contract.Meta {
	return contract.Meta{ID: "bsky_post_create", Label: "发帖",
		Desc: "发一条 Bluesky 帖子：自动识别链接/话题/提及，可带图与链接卡片", TimeoutSec: 120}
}

func (PostCreate) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("text").Label("正文").
			Desc("上限 300 个字（按字形算，中文一个字算一个）。链接、#话题、@用户名会自动变成可点的"),
		field.String("reply_to_uri").Label("回复给（帖子链接或 at:// 地址）").
			Desc("填了就是回复。可直接粘 https://bsky.app/profile/xxx/post/xxx").Optional(),
		field.Files("images").Label("图片").
			Desc("最多 4 张，每张 ≤2MB（Bluesky 的限制）").Optional(),
		field.Strings("image_alts").Label("图片替代文本").
			Desc("按顺序与图片对应，给读屏用户看；少给几条不影响发布").Optional(),
		field.String("link_card_url").Label("链接卡片").
			Desc("留空则用正文里的第一个链接自动生成卡片；填 -（减号）表示不要卡片").Optional(),
		field.String("langs").Label("语言").
			Desc("逗号分隔的 BCP-47 码，如 zh、en。影响别人的语言过滤能否看到你").Default("zh"),
	}
}

func (PostCreate) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("uri").Label("帖子地址").Desc("at:// 形式，回复/删除用它"),
		field.String("cid").Label("内容哈希").Desc("回复时要连同 uri 一起给"),
		field.String("url").Label("网页链接").Desc("https://bsky.app/... 可直接点开或发进通知"),
	}
}

// PostThread sends a thread of posts.
type PostThread struct{}

func (PostThread) Meta() contract.Meta {
	return contract.Meta{ID: "bsky_post_thread", Label: "发帖串",
		Desc: "把多段文本发成一串首尾相连的帖子；第一段可带图", TimeoutSec: 300}
}

func (PostThread) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Strings("texts").Label("每条正文").
			Desc("按顺序逐条发出，后一条自动回复前一条。**一条一段**，本操作不替你断句"),
		field.String("reply_to_uri").Label("接在谁后面").Desc("填了则整串挂在那条帖子下").Optional(),
		field.Files("images").Label("首条的图片").Desc("只挂在第一条上，最多 4 张").Optional(),
		field.Int("interval_ms").Label("每条之间停顿（毫秒）").Default(500),
		field.String("langs").Label("语言").Default("zh"),
	}
}

func (PostThread) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Strings("uris").Label("各条地址").Desc("按发出顺序；中途失败时这里是**已经发出去的那几条**"),
		field.String("root_uri").Label("首条地址"),
		field.String("root_url").Label("首条网页链接"),
		field.Int("count").Label("发出条数"),
	}
}

// PostDelete deletes one post.
type PostDelete struct{}

func (PostDelete) Meta() contract.Meta {
	return contract.Meta{ID: "bsky_post_delete", Label: "删帖", Desc: "只能删授权账号自己发的", TimeoutSec: 30}
}

func (PostDelete) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("uri").Label("帖子地址").Desc("at:// 地址或 bsky.app 链接"),
	}
}

func (PostDelete) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("deleted").Label("已删除")}
}

// HealthCheck checks whether the credential still works (the platform's standard operation id).
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "检查凭证",
		Desc: "用账号密码换一次会话。ok=false 表示密码被撤销或改了", TimeoutSec: 30}
}

func (HealthCheck) Inputs() []contract.FieldSpec { return nil }

func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("凭证可用"),
		field.String("message").Label("说明"),
		field.String("handle").Label("账号").Optional(),
	}
}

// Credential is the credential contract.
//
// **Stores a password, not a token**: AT Protocol's accessJwt only lives a few minutes,
// and storing it in the credential would mean manually swapping it out every few minutes.
// An app password is a revocable long-lived credential, and the short-lived session is
// self-managed by the plugin.
type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("identifier").Label("账号").
			Desc("完整 handle（如 acme.bsky.social）或邮箱"),
		field.Secret("app_password").Label("应用专用密码").
			Desc("Bluesky 设置 → Privacy and Security → App Passwords 里生成，形如 xxxx-xxxx-xxxx-xxxx。" +
				"**别用登录密码**：应用密码可单独撤销，且拿不到改密码/删账号这类权限"),
		field.Text("pds_url").Label("服务地址").
			Desc("留空用 https://bsky.social；自建 PDS 才需要改").Optional(),
		field.Text("proxy").Label("出站代理").
			Desc("如 http://127.0.0.1:7897；部署环境直连不了 bsky.social 时必填").Optional(),
	}
}
