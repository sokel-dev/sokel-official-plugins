// Package schema declares the producthunt plugin's contract.
//
// Product Hunt's API (v2, GraphQL at api.producthunt.com/v2/api/graphql) is read-only for posts and comments: its
// only mutations are goals and follows. So this plugin reads products, their comments and the day's leaderboard, and
// watches the products you care about for new comments. Launching and replying happen on the site.
//
// Field names follow Product Hunt's schema (post / comment / votesCount as votes_count …). The official schema is kept
// in testdata/schema.graphql (from github.com/producthunt/producthunt-api) and the queries in queries.go are checked
// against it by a test, since without a token the API cannot even be introspected.
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// Post is one product launch.
type Post struct {
	ID            string `sokel:"id" label:"产品 id"`
	Slug          string `sokel:"slug" label:"slug" desc:"产品页地址 producthunt.com/posts/ 后面那段"`
	Name          string `sokel:"name" label:"名称"`
	Tagline       string `sokel:"tagline" label:"一句话介绍"`
	Description   string `sokel:"description,optional" label:"介绍"`
	URL           string `sokel:"url" label:"产品页"`
	Website       string `sokel:"website,optional" label:"官网"`
	VotesCount    int    `sokel:"votes_count" label:"得票"`
	CommentsCount int    `sokel:"comments_count" label:"评论数"`
	CreatedAt     string `sokel:"created_at" label:"发布时间" desc:"RFC3339"`
	FeaturedAt    string `sokel:"featured_at,optional" label:"上首页时间" desc:"没上首页为空"`
	Makers        string `sokel:"makers,optional" label:"制作者" desc:"用户名，逗号分隔"`
}

// Comment is one comment or reply.
type Comment struct {
	ID         string `sokel:"id" label:"评论 id"`
	Body       string `sokel:"body" label:"正文"`
	URL        string `sokel:"url" label:"评论地址" desc:"在那里回复"`
	VotesCount int    `sokel:"votes_count" label:"得票"`
	ParentID   string `sokel:"parent_id,optional" label:"回复的是哪条评论" desc:"顶层评论为空"`
	IsMine     bool   `sokel:"is_mine" label:"是我写的" desc:"token 所属账号写的"`
	Username   string `sokel:"username,optional" label:"评论人" desc:"Product Hunt 对接口隐藏评论人身份，通常为空；点 url 到网页上看"`
	CreatedAt  string `sokel:"created_at" label:"时间" desc:"RFC3339"`
}

// PostGet reads one product.
type PostGet struct{}

func (PostGet) Meta() contract.Meta {
	return contract.Meta{ID: "ph_post_get", Label: "取产品", Desc: "得票、评论数、上首页时间等", TimeoutSec: 30}
}

func (PostGet) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("post").Label("产品").Desc("slug 或产品页地址（producthunt.com/posts/…）"),
	}
}

func (PostGet) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("post", Post{}).Label("产品")}
}

// Comments reads a product's comments with their replies.
type Comments struct{}

func (Comments) Meta() contract.Meta {
	return contract.Meta{ID: "ph_comments", Label: "取评论",
		Desc: "一个产品的评论和回复，最新在前（每条评论带最近 5 条回复）", TimeoutSec: 60}
}

func (Comments) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("post").Label("产品").Desc("slug 或产品页地址"),
		field.Int("limit").Label("顶层评论条数").Desc("默认 20，最多 50（Product Hunt 每页 20 条，多的分页取）").Default(20),
	}
}

func (Comments) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("items", []Comment{}).Label("评论").Desc("顶层评论在前、它的回复紧跟其后"),
		field.Int("count").Label("条数"),
		field.Int("total").Label("评论总数"),
	}
}

// Leaderboard reads the day's ranking.
type Leaderboard struct{}

func (Leaderboard) Meta() contract.Meta {
	return contract.Meta{ID: "ph_leaderboard", Label: "取榜单",
		Desc: "某一天（太平洋时间）发布的产品，按排名", TimeoutSec: 60}
}

func (Leaderboard) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("date").Label("日期").Desc("YYYY-MM-DD，按太平洋时间算一天（PH 的日榜就是这么切的）；留空 = 今天").Optional(),
		field.Enum("order", field.Opt("RANKING", "排名"), field.Opt("VOTES", "得票"), field.Opt("NEWEST", "最新")).Label("排序").Default("RANKING"),
		field.Int("limit").Label("条数").Desc("默认 20，最多 50（每页 20 条，多的分页取）").Default(20),
	}
}

func (Leaderboard) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("items", []Post{}).Label("产品"),
		field.Int("count").Label("条数"),
	}
}

// HealthCheck is the platform's "test credential" operation.
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "检查凭证", Desc: "用 token 读一次当前用户", TimeoutSec: 30}
}

func (HealthCheck) Inputs() []contract.FieldSpec { return nil }

func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("可用"),
		field.String("message").Label("说明"),
		field.String("username").Label("账号"),
	}
}

// —— Events ——

// CommentReceived: a new comment or reply on a watched product.
type CommentReceived struct{}

func (CommentReceived) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "comment_received", Label: "有新评论"}
}

func (CommentReceived) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("comment_id").Label("评论 id"),
		field.String("body").Label("正文"),
		field.String("url").Label("评论地址").Desc("在那里回复（PH 的接口不能回复）"),
		field.Int("votes_count").Label("得票"),
		field.String("parent_id").Label("回复的是哪条评论").Desc("顶层评论为空"),
		field.Bool("is_reply_to_me").Label("回复的是我").Desc("它回复的那条评论是 token 所属账号写的"),
		field.String("username").Label("评论人").Desc("Product Hunt 对接口隐藏评论人身份，通常为空；点 url 到网页上看是谁"),
		field.String("post_id").Label("产品 id"),
		field.String("post_name").Label("产品名称"),
		field.String("post_url").Label("产品页"),
		field.String("created_at").Label("时间"),
	}
}

type Events struct{}

func (Events) CommonFields() []string {
	return []string{"comment_id", "body", "url", "post_name", "created_at"}
}

// —— Credential ——

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Secret("developer_token").Label("Developer token").
			Desc("api.producthunt.com/v2/oauth/applications 建一个应用，页面下方的 Developer Token（不会过期，代表你自己的账号）").Required(),
		field.Text("proxy").Label("出站代理").Desc("如 http://127.0.0.1:7897；部署环境连不上 producthunt.com 时必填").Optional(),
		field.Text("watch_posts").Label("监听的产品").
			Desc("slug 或产品页地址，多个用逗号分隔：这些产品下有新评论或新回复时触发「有新评论」").Optional(),
		field.Select("watch_my_posts", "on", "off").Label("监听我做的产品").
			Desc("on = token 所属账号作为制作者的最近 10 个产品也一起监听").Default("on"),
		field.Text("poll_seconds").Label("轮询间隔（秒）").
			Desc("默认 180，最短 60（PH 按查询复杂度限流，15 分钟一个额度）").Default("180"),
		field.Text("watch_cursor").Label("轮询游标").Desc("事件源自动维护，勿手填").Optional(),
	}
}
