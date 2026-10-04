// Package schema declares the hackernews plugin's contract.
//
// Hacker News has no write API: the official API (Firebase) and the search API HN itself links to (Algolia) are
// both read-only, and automated submissions break the site's rules. So this plugin reads and watches; replying
// happens on the site, through the comment's link.
//
// What it is for is not missing a conversation: when someone comments on a story you submitted, or replies to one
// of your comments anywhere, the "new comment" event fires within one polling interval.
//
// Two sources, chosen for what each does well:
//
//   - Algolia (hn.algolia.com/api/v1) for anything "new since": search_by_date filters by author / story and by
//     time in one request, which the official API cannot do (it only has per-item lookups).
//   - Firebase (hacker-news.firebaseio.com/v0) for the live front-page lists and single items: it is the source of
//     truth, and Algolia lags it by up to a minute.
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// Item is one story or comment, the same shape whichever API produced it.
type Item struct {
	ID         string `sokel:"id" label:"条目 id"`
	Type       string `sokel:"type" label:"类型" desc:"story / comment / job / poll"`
	Author     string `sokel:"author" label:"作者"`
	Title      string `sokel:"title,optional" label:"标题" desc:"帖子才有；评论为空"`
	Text       string `sokel:"text,optional" label:"正文（纯文本）" desc:"由 text_html 去掉标签、还原转义得到；链接帖通常为空"`
	TextHTML   string `sokel:"text_html,optional" label:"正文（HN 原文 HTML）" desc:"HN 返回的原样内容"`
	URL        string `sokel:"url,optional" label:"外链" desc:"链接帖指向的地址；讨论帖与评论为空"`
	Permalink  string `sokel:"permalink" label:"HN 上的地址" desc:"https://news.ycombinator.com/item?id=…，回复评论就点这里"`
	Score      int    `sokel:"score" label:"得分" desc:"帖子的分数；评论没有公开分数，为 0"`
	Comments   int    `sokel:"comments" label:"评论数" desc:"帖子的评论总数；评论为 0"`
	StoryID    string `sokel:"story_id" label:"所属帖子 id" desc:"评论所在的帖子；帖子本身为自己的 id"`
	StoryTitle string `sokel:"story_title,optional" label:"所属帖子标题"`
	ParentID   string `sokel:"parent_id,optional" label:"上一级 id" desc:"评论回复的是哪一条（帖子或另一条评论）；帖子为空"`
	CreatedAt  string `sokel:"created_at" label:"发布时间" desc:"RFC3339，UTC"`
	Depth      int    `sokel:"depth" label:"层级" desc:"只在「取评论」里有意义：1 = 直接回复帖子，2 = 回复评论，以此类推"`
}

// ItemGet reads one story or comment.
type ItemGet struct{}

func (ItemGet) Meta() contract.Meta {
	return contract.Meta{ID: "hn_item_get", Label: "取条目",
		Desc: "按 id 读一条帖子或评论（得分、评论数、正文、作者）", TimeoutSec: 30}
}

func (ItemGet) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("id").Label("条目 id").Desc("HN 地址里 item?id= 后面那串数字，也可以直接粘整个地址"),
	}
}

func (ItemGet) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("item", Item{}).Label("条目")}
}

// List reads one of the front-page lists.
type List struct{}

func (List) Meta() contract.Meta {
	return contract.Meta{ID: "hn_list", Label: "取榜单",
		Desc: "首页、最新、Ask HN、Show HN 等榜单的前若干条", TimeoutSec: 60}
}

func (List) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Enum("list",
			field.Opt("top", "首页（热门）"),
			field.Opt("new", "最新"),
			field.Opt("best", "最佳"),
			field.Opt("ask", "Ask HN"),
			field.Opt("show", "Show HN"),
			field.Opt("job", "招聘")).
			Label("榜单").Default("top"),
		field.Int("limit").Label("条数").Desc("默认 30，最多 100").Default(30),
	}
}

func (List) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("items", []Item{}).Label("条目").Desc("按榜单顺序"),
		field.Int("count").Label("条数"),
	}
}

// Search finds stories / comments by keyword, newest first.
type Search struct{}

func (Search) Meta() contract.Meta {
	return contract.Meta{ID: "hn_search", Label: "搜索",
		Desc: "按关键词搜帖子和评论，最新的在前。看谁在讨论你的项目就用它", TimeoutSec: 30}
}

func (Search) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("query").Label("关键词").Desc("项目名、域名、仓库地址都行；多个词之间是「且」"),
		field.Enum("kind",
			field.Opt("all", "帖子和评论"),
			field.Opt("story", "只看帖子"),
			field.Opt("comment", "只看评论")).
			Label("范围").Default("all"),
		field.Int("since_hours").Label("最近多少小时").Desc("0 = 不限").Default(0),
		field.Int("limit").Label("条数").Desc("默认 20，最多 100").Default(20),
	}
}

func (Search) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("items", []Item{}).Label("结果").Desc("按时间倒序"),
		field.Int("count").Label("条数"),
	}
}

// ThreadComments reads every comment under a story.
type ThreadComments struct{}

func (ThreadComments) Meta() contract.Meta {
	return contract.Meta{ID: "hn_thread_comments", Label: "取评论",
		Desc: "取一条帖子下的全部评论，按讨论顺序平铺（带层级）。可只取某个时刻之后的新评论", TimeoutSec: 60}
}

func (ThreadComments) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("id").Label("帖子 id").Desc("也可以直接粘 HN 地址"),
		field.String("since").Label("只要这之后的").Desc("RFC3339 时间；留空 = 全部").Optional(),
	}
}

func (ThreadComments) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("items", []Item{}).Label("评论").Desc("讨论顺序（先父后子），depth 是层级"),
		field.Int("count").Label("条数"),
		field.String("title").Label("帖子标题"),
	}
}

// HealthCheck is the platform's "test credential" operation.
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "检查连接",
		Desc: "HN 不需要凭证；这里检查能不能连上 HN 的两个接口，并核对监听的用户名是否存在", TimeoutSec: 30}
}

func (HealthCheck) Inputs() []contract.FieldSpec { return nil }

func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("可用"),
		field.String("message").Label("说明"),
	}
}

// —— Events ——
//
// Field names follow HN's own terms (story / comment / parent), and nothing HN gives is dropped: the comment's HTML
// is passed through as text_html next to the plain-text rendering. Mapping onto a shape shared with other platforms is
// left to the workflow, where it is visible, rather than done here where it would quietly bend one platform's meaning
// into another's.

func commentEventFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("comment_id").Label("评论 id"),
		field.String("text").Label("正文（纯文本）").Desc("由 text_html 去掉标签、还原转义得到；链接保留完整地址"),
		field.String("text_html").Label("正文（HN 原文 HTML）").Desc("HN 返回的原样内容"),
		field.String("author").Label("评论人"),
		field.String("author_url").Label("评论人主页"),
		field.String("url").Label("评论地址").Desc("news.ycombinator.com/item?id=…，在那里回复"),
		field.String("story_id").Label("所属帖子 id"),
		field.String("story_title").Label("所属帖子标题"),
		field.String("story_url").Label("所属帖子在 HN 上的地址"),
		field.String("parent_id").Label("回复的是哪一条").Desc("帖子 id 或上一条评论的 id；与 story_id 相同表示直接回复帖子"),
		field.Bool("is_reply_to_me").Label("回复的是我").Desc("parent_id 是监听账号写的帖子或评论"),
		field.String("created_at").Label("发布时间").Desc("RFC3339，UTC"),
	}
}

// CommentReceived: a new comment on a watched story, or a reply to the watched user anywhere.
type CommentReceived struct{}

func (CommentReceived) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "comment_received", Label: "有新评论"}
}
func (CommentReceived) Fields() []contract.FieldSpec { return commentEventFields() }

// KeywordMatched: a new story or comment containing the watched keywords.
type KeywordMatched struct{}

func (KeywordMatched) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "keyword_matched", Label: "关键词命中"}
}
func (KeywordMatched) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("kind").Label("类型").Desc("story（新帖子）/ comment（新评论）"),
		field.String("item_id").Label("条目 id").Desc("命中的帖子或评论"),
		field.String("title").Label("标题").Desc("帖子才有"),
		field.String("text").Label("正文（纯文本）").Desc("讨论帖与评论有；链接帖为空"),
		field.String("text_html").Label("正文（HN 原文 HTML）"),
		field.String("link").Label("外链").Desc("链接帖指向的地址"),
		field.String("author").Label("作者"),
		field.String("author_url").Label("作者主页"),
		field.String("url").Label("在 HN 上的地址"),
		field.String("story_id").Label("所属帖子 id").Desc("帖子本身为自己的 id"),
		field.String("story_title").Label("所属帖子标题"),
		field.Int("points").Label("得分").Desc("帖子才有"),
		field.String("created_at").Label("发布时间").Desc("RFC3339，UTC"),
		field.String("matched_query").Label("命中的关键词"),
	}
}

// Events lists the fields every event carries; the platform flattens them to the trigger's top level.
type Events struct{}

func (Events) CommonFields() []string {
	return []string{"text", "text_html", "author", "url", "story_id", "story_title", "created_at"}
}

// —— Credential ——
//
// HN needs no login to read. The credential is where the event source's settings live: a source is a long-running
// process with no node configuration of its own, so what to watch can only come from the credential.
type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("watch_user").Label("监听的 HN 用户名").
			Desc("你的 HN 账号。它最近发的帖子下有新评论、或有人回复它的评论时触发「有新评论」；留空 = 不监听").Optional(),
		field.Text("watch_items").Label("额外监听的帖子").
			Desc("帖子 id 或地址，多个用逗号分隔。别人替你发的帖子、或你想跟进的讨论放这里").Optional(),
		field.Text("watch_query").Label("监听的关键词").
			Desc("出现这个词的新帖子或新评论触发「关键词命中」，如项目名或 github.com/你的/仓库；留空 = 不监听").Optional(),
		field.Text("watch_days").Label("跟踪多少天内的帖子").
			Desc("默认 14：你两周前发的帖子还会有人评论，再早的基本沉了").Default("14"),
		field.Text("poll_seconds").Label("轮询间隔（秒）").
			Desc("默认 120，最短 60。HN 的接口免费，但别把它当实时推送用").Default("120"),
		field.Text("proxy").Label("出站代理").
			Desc("如 http://127.0.0.1:7897；部署环境直连不了 news.ycombinator.com 时必填").Optional(),
		field.Text("watch_cursor").Label("轮询游标").Desc("事件源自动维护，勿手填").Optional(),
	}
}
