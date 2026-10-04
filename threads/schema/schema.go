// Package schema declares the operation and credential contracts for the threads plugin.
//
// Uses the same publish contract as the other publishers. Threads has four quirks:
//
//  1. **Publishing is two steps**: first create a "media container" to get a creation_id, then
//     publish it. That intermediate step is Meta's own shape, not something we added — but the
//     plugin wraps the two steps into one operation, so the canvas doesn't need two nodes for it.
//
//  2. **Images/video are fetched by URL, not uploaded**. Threads downloads that address itself, so
//     it must be a **publicly reachable** link. The platform rewrites file references into signed
//     download URLs when handing them to the plugin, so just binding a file works fine; a public
//     link from an external image host works too.
//
//  3. **250 per 24 hours** (a rolling window). This is an account-level quota, and going over it
//     is rejected outright — so "check credentials" surfaces the remaining quota along the way, and
//     a workflow can decide whether to keep publishing based on it.
//
//  4. **A 60-day token** with no refresh_token (renewal means exchanging the long-lived token for
//     a new long-lived token). Expiry requires a human to reauthorize; health_check marks the
//     credential invalid and triggers an alert when it's expired.
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/auth"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// PostCreate posts a single Threads post.
type PostCreate struct{}

func (PostCreate) Meta() contract.Meta {
	return contract.Meta{ID: "th_post_create", Label: "发帖",
		Desc: "发一条 Threads（建容器 + 发布两步已包在里面），可带图/视频或回复别人", TimeoutSec: 180}
}

func (PostCreate) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("text").Label("正文").Desc("上限 500 字符"),
		field.Files("media").Label("图片/视频").
			Desc("最多 20 个（多个自动变成轮播）。Threads 会**自己去下载**这些文件，" +
				"所以它们必须公网可达——绑平台文件即可，平台会给带签名的地址").Optional(),
		field.Strings("media_urls").Label("媒体地址").
			Desc("已经在公网上的图片/视频地址；与上面的文件二选一或混用").Optional(),
		field.String("reply_to_id").Label("回复给（帖子 id）").Desc("填了就是回复").Optional(),
		field.Enum("reply_control",
			field.Opt("everyone", "所有人可回复"),
			field.Opt("accounts_you_follow", "仅我关注的人"),
			field.Opt("mentioned_only", "仅被提及的人")).
			Label("谁能回复").Default("everyone"),
	}
}

func (PostCreate) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("id").Label("帖子 id"),
		field.String("url").Label("链接").Desc("可直接点开或发进通知"),
	}
}

// PostThread posts a chain of Threads posts.
type PostThread struct{}

func (PostThread) Meta() contract.Meta {
	return contract.Meta{ID: "th_post_thread", Label: "发帖串",
		Desc: "把多段文本发成一串首尾相连的帖子；第一条可带媒体", TimeoutSec: 600}
}

func (PostThread) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Strings("texts").Label("每条正文").Desc("按顺序逐条发出，后一条自动回复前一条。本操作不替你断句"),
		field.String("reply_to_id").Label("接在谁后面").Optional(),
		field.Files("media").Label("首条的媒体").Optional(),
		field.Int("interval_ms").Label("每条之间停顿（毫秒）").
			Desc("默认 1000。Threads 的发布是异步的，间隔太短容易撞上「上一条还没就绪」").Default(1000),
	}
}

func (PostThread) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Strings("ids").Label("各条 id").Desc("按发出顺序；中途失败时是**已经发出去的那几条**"),
		field.String("root_id").Label("首条 id"),
		field.String("root_url").Label("首条链接"),
		field.Int("count").Label("发出条数"),
	}
}

// HealthCheck checks whether the credential still works (the platform-mandated operation id), and
// surfaces the remaining publishing quota along the way.
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "检查凭证",
		Desc:       "查一次账号与**剩余发布额度**（每 24 小时 250 条）。ok=false 多半是令牌 60 天到期了",
		TimeoutSec: 30}
}

func (HealthCheck) Inputs() []contract.FieldSpec { return nil }

func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("凭证可用"),
		field.String("message").Label("说明"),
		field.String("username").Label("账号").Optional(),
		field.Int("quota_used").Label("24 小时内已发").Optional(),
		field.Int("quota_total").Label("24 小时配额").Optional(),
	}
}

// Credential is the credential contract.
type Credential struct{}

// AuthMeta: Threads's OAuth.
//
// threads_basic is the foundation for fetching account info (without it, you can't even look up
// who you are); threads_content_publish is the actual publishing permission.
func (Credential) AuthMeta() contract.AuthMeta {
	return auth.OAuth("threads", "threads_basic", "threads_content_publish")
}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Secret("access_token").Label("访问令牌（授权注入）").
			Desc("点「授权」后由平台写入，勿手填。**60 天到期**且 Threads 没有刷新令牌——" +
				"到期后重新点一次「授权」即可").Optional(),
		field.Text("proxy").Label("出站代理").
			Desc("如 http://127.0.0.1:7897；部署环境直连不了 graph.threads.net 时必填").Optional(),
	}
}
