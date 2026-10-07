// Package schema declares the reddit plugin's contract.
//
// Reddit closed self-service API apps on 2025-11-11 (the "Responsible Builder Policy": every app now needs an
// approval that takes weeks and is rarely given), so this plugin uses nothing that needs a key: Reddit's public
// Atom feeds (`.rss` on a search, a subreddit, a user, a post). They need no account and cost nothing, and they
// carry what a watcher needs: who posted what, where, when, with a link to reply on the site. They cannot post or
// reply, and they do not show an account's inbox, so "someone replied to my comment on somebody else's post" is
// out of reach; new comments on the user's own posts (and on any post listed) are not.
//
// The feeds allow ONE request per minute per IP without login (measured 2026-10-07: x-ratelimit-remaining 0 after
// one request, reset ~60 s). Every call here goes through a pacer that waits for the window, so an operation can
// take up to a minute, and a watch round with several posts takes several minutes. That is why the poll interval's
// floor is high.
//
// Field names follow Reddit's (fullname t3_… / t1_…, subreddit, permalink); mapping onto a shape shared with other
// platforms is left to the workflow.
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// Item is a post (t3) or comment (t1) as a feed shows it.
type Item struct {
	Name      string `sokel:"name" label:"全名" desc:"t3_…（帖子）或 t1_…（评论）"`
	Kind      string `sokel:"kind" label:"类型" desc:"post / comment"`
	Subreddit string `sokel:"subreddit" label:"版块"`
	Author    string `sokel:"author" label:"作者" desc:"不带 u/"`
	AuthorURL string `sokel:"author_url" label:"作者主页"`
	Title     string `sokel:"title" label:"标题" desc:"帖子的标题；评论是「作者 on 帖子标题」"`
	Text      string `sokel:"text" label:"正文（纯文本）" desc:"由 text_html 去掉标签、还原转义得到；链接保留完整地址"`
	TextHTML  string `sokel:"text_html" label:"正文（HTML）" desc:"Reddit 渲染好的 HTML，链接帖是摘要卡片"`
	URL       string `sokel:"url" label:"地址" desc:"https://www.reddit.com/r/…，在那里查看与回复"`
	PostID    string `sokel:"post_id" label:"所属帖子" desc:"帖子 id（不带 t3_）"`
	CreatedAt string `sokel:"created_at" label:"发布时间" desc:"RFC3339，UTC"`
}

// Search finds posts by keyword.
type Search struct{}

func (Search) Meta() contract.Meta {
	return contract.Meta{ID: "reddit_search", Label: "搜索帖子",
		Desc: "全站或指定版块按关键词搜帖子。Reddit 不登录一分钟只让取一次，操作可能等最多一分钟", TimeoutSec: 150}
}

func (Search) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("query").Label("关键词").Desc("Reddit 搜索语法，如 \"sokel\" 或 title:sokel"),
		field.String("subreddits").Label("只在这些版块里找").Desc("用 + 连接，如 golang+selfhosted；留空 = 全站").Optional(),
		field.Enum("sort", field.Opt("new", "最新"), field.Opt("relevance", "相关"), field.Opt("top", "最高分"), field.Opt("comments", "评论最多")).Label("排序").Default("new"),
		field.Enum("time", field.Opt("day", "一天内"), field.Opt("week", "一周内"), field.Opt("month", "一月内"), field.Opt("year", "一年内"), field.Opt("all", "全部")).Label("时间范围").Default("week"),
	}
}

func (Search) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("items", []Item{}).Label("帖子"),
		field.Int("count").Label("条数"),
	}
}

// SubredditNew lists a subreddit's newest posts.
type SubredditNew struct{}

func (SubredditNew) Meta() contract.Meta {
	return contract.Meta{ID: "reddit_subreddit_new", Label: "版块最新帖子", Desc: "一个或多个版块的最新帖子（最多 25 条）", TimeoutSec: 150}
}

func (SubredditNew) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("subreddits").Label("版块").Desc("不带 r/，多个用 + 连接，如 golang+selfhosted"),
	}
}

func (SubredditNew) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("items", []Item{}).Label("帖子"),
		field.Int("count").Label("条数"),
	}
}

// PostComments reads a post's comments.
type PostComments struct{}

func (PostComments) Meta() contract.Meta {
	return contract.Meta{ID: "reddit_post_comments", Label: "取帖子评论",
		Desc: "一个帖子的评论（Reddit 的 feed 给最多 25 条，顺序是它的默认排序）", TimeoutSec: 150}
}

func (PostComments) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("post").Label("帖子").Desc("帖子地址或 id（如 1abc2de / t3_1abc2de）"),
	}
}

func (PostComments) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Json("post", Item{}).Label("帖子"),
		field.Array("items", []Item{}).Label("评论"),
		field.Int("count").Label("评论条数"),
	}
}

// UserPosts lists a user's posts.
type UserPosts struct{}

func (UserPosts) Meta() contract.Meta {
	return contract.Meta{ID: "reddit_user_posts", Label: "用户的帖子", Desc: "某个用户最近发的帖子（最多 25 条）", TimeoutSec: 150}
}

func (UserPosts) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("username").Label("用户名").Desc("不带 u/"),
	}
}

func (UserPosts) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("items", []Item{}).Label("帖子"),
		field.Int("count").Label("条数"),
	}
}

// HealthCheck is the platform's "test credential" operation.
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "检查配置", Desc: "连一次 Reddit，并核对监听的用户名是否存在", TimeoutSec: 150}
}

func (HealthCheck) Inputs() []contract.FieldSpec { return nil }

func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("可用"),
		field.String("message").Label("说明"),
	}
}

// —— Events ——

// CommentReceived: a new comment on one of the user's recent posts, or on a listed post.
type CommentReceived struct{}

func (CommentReceived) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "comment_received", Label: "有新评论"}
}

func (CommentReceived) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("comment_id").Label("评论全名").Desc("t1_…"),
		field.String("text").Label("正文（纯文本）"),
		field.String("text_html").Label("正文（HTML）"),
		field.String("author").Label("评论人").Desc("不带 u/"),
		field.String("author_url").Label("评论人主页"),
		field.String("url").Label("评论地址").Desc("在那里回复"),
		field.String("subreddit").Label("版块"),
		field.String("post_id").Label("所属帖子 id"),
		field.String("post_title").Label("所属帖子标题"),
		field.String("post_url").Label("所属帖子地址"),
		field.String("created_at").Label("时间"),
	}
}

// KeywordMatched: a new post matching the watched keyword.
type KeywordMatched struct{}

func (KeywordMatched) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "keyword_matched", Label: "关键词命中"}
}

func (KeywordMatched) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("name").Label("帖子全名").Desc("t3_…"),
		field.String("title").Label("标题"),
		field.String("text").Label("正文（纯文本）"),
		field.String("text_html").Label("正文（HTML）"),
		field.String("author").Label("作者"),
		field.String("author_url").Label("作者主页"),
		field.String("url").Label("地址").Desc("在那里查看与回复"),
		field.String("subreddit").Label("版块"),
		field.String("created_at").Label("时间"),
		field.String("matched_query").Label("命中的关键词"),
	}
}

type Events struct{}

func (Events) CommonFields() []string {
	return []string{"text", "text_html", "author", "author_url", "url", "subreddit", "created_at"}
}

// —— Credential ——

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("watch_user").Label("监听的 Reddit 用户名").
			Desc("你的 Reddit 账号，不带 u/。它最近发的帖子下有新评论时触发「有新评论」；留空 = 不监听").Optional(),
		field.Text("watch_items").Label("额外监听的帖子").
			Desc("帖子地址或 id，多个用逗号分隔。别人替你发的帖子、你想跟进的讨论放这里").Optional(),
		field.Text("watch_query").Label("监听的关键词").
			Desc("出现这个词的新帖子触发「关键词命中」，如项目名或 github.com/你的/仓库；留空 = 不监听").Optional(),
		field.Text("watch_subreddits").Label("关键词只在这些版块里找").
			Desc("用 + 连接，如 golang+selfhosted；留空 = 全站").Optional(),
		field.Text("watch_days").Label("跟踪多少天内的帖子").
			Desc("默认 14：你两周前发的帖子还会有人评论，再早的基本沉了。每轮最多盯 5 个帖子（最新的）").Default("14"),
		field.Text("poll_seconds").Label("轮询间隔（秒）").
			Desc("默认 600，最短 300。Reddit 不登录一分钟只让取一次，一轮要花「盯的帖子数 + 2」分钟，间隔从一轮结束算起").Default("600"),
		field.Text("proxy").Label("出站代理").
			Desc("如 http://127.0.0.1:7897；部署环境直连不了 reddit.com 时必填。注意一分钟一次的额度按出口 IP 算，几个副本共用一个代理就要分这一次").Optional(),
		field.Text("watch_cursor").Label("轮询游标").Desc("事件源自动维护，勿手填").Optional(),
	}
}
