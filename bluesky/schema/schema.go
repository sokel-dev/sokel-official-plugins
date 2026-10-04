// Package schema 声明 bluesky 插件的操作与凭证契约。
//
// 这是**第一个发布器插件**，统一 publish 契约的样板（docs/social-publishing-plugins.md §3）：
// 发布类操作一律回 `id` + `url`，长内容的分段归插件，媒体随发布一起走。
//
// 四条判断：
//
//  1. **富文本要在插件里算**。AT Protocol 的链接/话题/提及不是从正文里自动认的，
//     要显式给 facets——而 facets 的下标是 **UTF-8 字节偏移**。发一条带中文和链接的推，
//     按字符数算下标会让链接错位甚至发不出去。这件事每个用户都会踩，所以插件包掉。
//
//  2. **链接卡片默认要做**。财经内容十条有八条是「一句话 + 一个链接」，没有卡片的链接
//     在时间线上就是一串裸 URL。所以给了链接就顺手抓一次 OG 信息拼成 external 嵌入；
//     抓不到就退回纯文本链接，**不让它成为发布失败的理由**。
//
//  3. **图片随发布一起传**，不单列上传操作。Bluesky 的图片上限 2MB、一条最多 4 张，
//     一次调用传得完；而 blob 引用是个不透明结构，摆到画布上只会让人不知道拿它干嘛
//     （X 的视频要分片+转码，那才值得独立成操作）。
//
//  4. **会话在插件内自管**。createSession 拿到的 accessJwt 只活几分钟，
//     refreshJwt 才是长期凭据。让用户在凭证里存 token 等于让他每隔几分钟手动换一次——
//     所以凭证里存的是「账号 + 应用专用密码」，短期会话由插件缓存与续期。
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// PostCreate 发一条帖子。
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

// PostThread 发一串帖子。
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

// PostDelete 删一条帖子。
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

// HealthCheck 凭证还能用吗（平台约定的操作 id）。
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

// Credential 凭证契约。
//
// **不存 token 存密码**：AT Protocol 的 accessJwt 只活几分钟，存进凭证等于让人
// 每隔几分钟手动换一次。应用专用密码是可随时撤销的长期凭据，短期会话由插件自管。
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
