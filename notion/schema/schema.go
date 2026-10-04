// Package schema declares the notion plugin's operations, events, and credential contract.
//
// Three decisions run through all of it:
//
//  1. **Model around data sources, not the old database_id shape.** Since 2025-09-03, Notion split
//     a database into a "container (database) + table (data_source)"; one database can hold multiple
//     data sources, and querying, row creation, and relations all use data_source_id. A contract
//     built on the old shape breaks across the board the moment a user hands it a multi-source
//     database, and fixing it later is a breaking migration (n8n has already been through this
//     once). So it's data sources from day one.
//
//  2. **Page content goes through markdown, the block tree is only a fallback.** Notion's markdown
//     read/write endpoint (GET/PATCH /v1/pages/{id}/markdown) turns "read a page for the model" /
//     "let the model edit a page" into a single call, whereas the block tree has to be fetched
//     recursively and assembled across 25 block types. The block-level API is still kept
//     (notion_block_children / notion_block_append) because things like database blocks and embeds
//     can't be expressed in markdown.
//
//  3. **Properties are given both ways** (see the top of types.go): `props` normalized,
//     `properties_raw` as-is.
//
// Two auth methods coexist: an internal integration secret (paste the `ntn_`-prefixed token into
// the credential) or OAuth authorization. If a token is set, use it; otherwise fall back to the
// access_token obtained via authorization — both are the same kind of credential (which pages this
// integration can see), so there's no need to split them into two separate credential fields.
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/auth"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// —— Read ——

// Search finds pages and data sources by title.
type Search struct{}

func (Search) Meta() contract.Meta {
	return contract.Meta{ID: "notion_search", Label: "搜索"}
}

func (Search) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		// Notion search **only matches titles**, it doesn't search content. Spelled out here,
		// otherwise "can't find it" gets reported as a bug.
		field.String("query").Label("关键词").
			Desc("只匹配标题，不搜正文；留空 = 列出集成能看到的全部").Optional(),
		field.Enum("type",
			field.Opt("all", "页面和数据源"), field.Opt("page", "只要页面"), field.Opt("data_source", "只要数据源")).
			Label("只要哪一类").Default("all"),
		field.Int("max_items").Label("最多几条").Desc("默认 50，上限 200；自动翻页").Default(50),
	}
}

func (Search) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("items", []SearchItem{}).Label("命中").
			Desc("要正文用「读页面」，要行用「查询数据源」"),
		field.Int("count").Label("条数"),
	}
}

// GetPage reads one page: properties + markdown content.
type GetPage struct{}

func (GetPage) Meta() contract.Meta {
	return contract.Meta{ID: "notion_page_get", Label: "读页面"}
}

func (GetPage) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("page_id").Label("页面 ID").Desc("贴完整 Notion 链接也行，会自己抠出 id"),
		// Content is a separate request, and a large page can run to tens of KB — when only
		// properties are needed, that round trip shouldn't be spent for nothing.
		field.Bool("with_content").Label("同时取正文").
			Desc("勾选后多一次请求，取整页 markdown").Default(true),
	}
}

func (GetPage) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("id").Label("页面 id"),
		field.String("url").Label("链接").Optional(),
		field.String("title").Label("标题").Optional(),
		field.Text("markdown").Label("正文（markdown）").Desc("with_content=false 时为空").Optional(),
		// Notion truncates very large pages. Without flagging this, downstream would feed a model
		// half a page thinking it's the whole thing.
		field.Bool("truncated").Label("正文被截断").
			Desc("true = 页面太大，markdown 只是一部分").Optional(),
		field.Object("props", "属性名与类型由所在数据源的表结构决定；值已归一化").Label("属性").Optional(),
		field.Object("properties_raw", "Notion 原样的属性 JSON；归一化会丢信息的类型（rollup/formula）在这里取").
			Label("属性（原样）").Optional(),
		field.String("parent_type").Label("父类别").Optional(),
		field.String("parent_id").Label("父 id").Optional(),
		field.String("created_time").Label("创建时间").Optional(),
		field.String("last_edited_time").Label("最后编辑时间").Optional(),
		field.Bool("in_trash").Label("在回收站").Optional(),
	}
}

// QueryDataSource queries a data source's rows (what used to be called "query database").
type QueryDataSource struct{}

func (QueryDataSource) Meta() contract.Meta {
	return contract.Meta{ID: "notion_db_query", Label: "查询数据源"}
}

func (QueryDataSource) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("data_source_id").Label("数据源 ID").
			Desc("用「列出数据源」从数据库 id 取；贴数据库链接时会当作单源库自动取第一个数据源"),
		// Filter/sort are **not normalized**: Notion's filter DSL is a nested and/or combinator,
		// and a normalized mini-language built on top of it would cover less than half the
		// possible forms, leaving no fallback for the rest.
		field.Any("filter", `Notion 的过滤 DSL，原样透传。例：{"property":"状态","status":{"equals":"进行中"}}`).
			Label("过滤").Optional(),
		field.Any("sorts", `Notion 的排序数组，原样透传。例：[{"property":"更新时间","direction":"descending"}]`).
			Label("排序").Optional(),
		field.Int("max_items").Label("最多几行").Desc("默认 100，上限 1000；自动翻页").Default(100),
		field.String("start_cursor").Label("起始游标").Desc("接着上次的 next_cursor 往下取").Optional(),
	}
}

func (QueryDataSource) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("pages", []PageItem{}).Label("行").Desc("每行的属性已归一化到 props"),
		field.Int("count").Label("行数"),
		field.Bool("has_more").Label("还有更多").Optional(),
		field.String("next_cursor").Label("下一页游标").Optional(),
	}
}

// GetSchema fetches a data source's schema.
type GetSchema struct{}

func (GetSchema) Meta() contract.Meta {
	return contract.Meta{ID: "notion_db_schema", Label: "取表结构"}
}

func (GetSchema) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("data_source_id").Label("数据源 ID"),
	}
}

func (GetSchema) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("data_source_id").Label("数据源 id"),
		field.String("database_id").Label("所属数据库 id").Optional(),
		field.String("title").Label("名称").Optional(),
		field.Array("properties", []PropSpec{}).Label("列"),
	}
}

// ListDataSources lists the data sources under a database.
type ListDataSources struct{}

func (ListDataSources) Meta() contract.Meta {
	return contract.Meta{ID: "notion_db_list", Label: "列出数据源"}
}

func (ListDataSources) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("database_id").Label("数据库 ID").Desc("贴完整 Notion 链接也行"),
	}
}

func (ListDataSources) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("database_id").Label("数据库 id"),
		field.String("title").Label("名称").Optional(),
		field.Array("data_sources", []DataSourceRef{}).Label("数据源").
			Desc("单源库只有一个；查询与建行用它的 id"),
		field.String("first_data_source_id").Label("首个数据源 id").
			Desc("单源库直接接这个往下走").Optional(),
	}
}

// ListBlocks reads child blocks (for things markdown can't express).
type ListBlocks struct{}

func (ListBlocks) Meta() contract.Meta {
	return contract.Meta{ID: "notion_block_children", Label: "读子块"}
}

func (ListBlocks) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("block_id").Label("块或页面 ID").Desc("页面本身就是块，贴页面 id 即读整页顶层块"),
		field.Int("max_items").Label("最多几块").Desc("默认 100，上限 500；不递归子块").Default(100),
	}
}

func (ListBlocks) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Any("blocks", "Notion 的块对象数组，形状由块类型决定（25 种，且随版本增补），原样透传").
			Label("块"),
		field.Int("count").Label("块数"),
		field.String("next_cursor").Label("下一页游标").Optional(),
	}
}

// ListUsers lists workspace members.
type ListUsers struct{}

func (ListUsers) Meta() contract.Meta {
	return contract.Meta{ID: "notion_user_list", Label: "列成员"}
}

func (ListUsers) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Int("max_items").Label("最多几人").Default(100),
	}
}

func (ListUsers) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("users", []User{}).Label("成员").Desc("写「负责人」这类 people 属性要用这里的 id"),
		field.Int("count").Label("人数"),
	}
}

// GetUser reads one member.
type GetUser struct{}

func (GetUser) Meta() contract.Meta {
	return contract.Meta{ID: "notion_user_get", Label: "读成员"}
}

func (GetUser) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("user_id").Label("用户 ID").Desc("留空 = 读本集成自己的机器人身份").Optional(),
	}
}

func (GetUser) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("id").Label("用户 id"),
		field.String("name").Label("名字").Optional(),
		field.String("type").Label("类别").Optional(),
		field.String("email").Label("邮箱").Optional(),
		field.String("avatar_url").Label("头像").Optional(),
	}
}

// ListComments lists comments on a page/block.
type ListComments struct{}

func (ListComments) Meta() contract.Meta {
	return contract.Meta{ID: "notion_comment_list", Label: "列评论"}
}

func (ListComments) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("block_id").Label("页面或块 ID"),
		field.Int("max_items").Label("最多几条").Default(100),
	}
}

func (ListComments) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("comments", []Comment{}).Label("评论"),
		field.Int("count").Label("条数"),
	}
}

// —— Write ——

// CreatePage creates a page: the parent can be another page, or a data source (= creating a row).
type CreatePage struct{}

func (CreatePage) Meta() contract.Meta {
	return contract.Meta{ID: "notion_page_create", Label: "建页面"}
}

func (CreatePage) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Enum("parent_type",
			field.Opt("page", "挂在某一页下"), field.Opt("data_source", "作为数据源的一行")).
			Label("父类别").Default("page"),
		field.String("parent_id").Label("父 ID").Desc("父页面 id 或数据源 id；贴链接也行"),
		field.String("title").Label("标题").
			Desc("数据源行的标题会写进它的标题列（不必再在 props 里给一遍）").Optional(),
		field.Object("props", "属性名与类型由目标数据源的表结构决定，运行时才知道；用「取表结构」看有哪些列").
			Label("属性").
			Desc(`归一化写法：{"状态":"进行中","标签":["A","B"],"截止":"2026-08-20"}；父是页面时忽略`).Optional(),
		field.Text("markdown").Label("正文（markdown）").Desc("留空 = 建一个空页面").Optional(),
		field.String("icon").Label("图标").Desc("一个 emoji，如 📌").Optional(),
	}
}

func (CreatePage) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("id").Label("页面 id"),
		field.String("url").Label("链接").Optional(),
		field.String("title").Label("标题").Optional(),
	}
}

// UpdatePage updates a page's properties (use "update content" for the body).
type UpdatePage struct{}

func (UpdatePage) Meta() contract.Meta {
	return contract.Meta{ID: "notion_page_update", Label: "改页面属性"}
}

func (UpdatePage) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("page_id").Label("页面 ID"),
		field.String("title").Label("标题").Desc("留空 = 不改").Optional(),
		field.Object("props", "属性名与类型由所在数据源的表结构决定，运行时才知道").Label("属性").
			Desc("只给要改的列；没提到的列不动。清空某列用 null").Optional(),
		field.String("icon").Label("图标").Desc("一个 emoji；留空 = 不改").Optional(),
	}
}

func (UpdatePage) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("id").Label("页面 id"),
		field.String("url").Label("链接").Optional(),
	}
}

// UpdateContent updates a page's content (three markdown modes).
type UpdateContent struct{}

func (UpdateContent) Meta() contract.Meta {
	return contract.Meta{ID: "notion_page_content", Label: "改页面正文"}
}

func (UpdateContent) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("page_id").Label("页面 ID"),
		// The three modes map to Notion's three write styles. edit is the one Notion officially
		// recommends for models — having a model rewrite the whole page is both expensive and
		// prone to deleting content it didn't mention.
		field.Enum("mode",
			field.Opt("append", "追加到末尾"),
			field.Opt("replace", "整页替换"),
			field.Opt("edit", "搜索替换（改一段）")).
			Label("怎么改").Default("append"),
		field.Text("markdown").Label("内容（markdown）").Desc("append / replace 用").Optional(),
		field.Text("old_str").Label("要替换的原文").Desc("edit 用；必须与页面里的文字一字不差").Optional(),
		field.Text("new_str").Label("替换成").Desc("edit 用；留空 = 删掉这段").Optional(),
		field.Bool("replace_all").Label("替换全部匹配").Desc("edit 用；默认只替换第一处").Optional(),
	}
}

func (UpdateContent) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("id").Label("页面 id"),
		field.Text("markdown").Label("改完后的正文").Desc("Notion 回的当前全文").Optional(),
		field.Bool("truncated").Label("正文被截断").Optional(),
	}
}

// TrashPage moves a page to trash, or restores it from trash.
type TrashPage struct{}

func (TrashPage) Meta() contract.Meta {
	return contract.Meta{ID: "notion_page_trash", Label: "删除/恢复页面"}
}

func (TrashPage) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("page_id").Label("页面 ID"),
		// Notion's API can only move to trash, not delete outright — spelled out here so nobody
		// thinks the data is gone.
		field.Bool("restore").Label("改为恢复").Desc("勾选 = 从回收站拿回来").Optional(),
	}
}

func (TrashPage) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("id").Label("页面 id"),
		field.Bool("in_trash").Label("在回收站").Desc("API 只能移进回收站，不能彻底删除"),
	}
}

// AppendBlocks appends raw blocks (for things markdown can't express).
type AppendBlocks struct{}

func (AppendBlocks) Meta() contract.Meta {
	return contract.Meta{ID: "notion_block_append", Label: "追加原始块"}
}

func (AppendBlocks) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("block_id").Label("页面或块 ID"),
		field.Any("blocks", "Notion 的块对象数组，原样透传。普通正文用「改页面正文」更省事，这里是给 markdown 表达不了的块（嵌入、看板、数据库块）留的口子").
			Label("块"),
	}
}

func (AppendBlocks) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Int("count").Label("写入块数"),
		field.Strings("block_ids").Label("块 id").Optional(),
	}
}

// CreateComment adds a comment.
type CreateComment struct{}

func (CreateComment) Meta() contract.Meta {
	return contract.Meta{ID: "notion_comment_create", Label: "加评论"}
}

func (CreateComment) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("page_id").Label("页面 ID").Desc("与讨论串 id 二选一").Optional(),
		field.String("discussion_id").Label("讨论串 ID").Desc("回某一串评论；来自「列评论」").Optional(),
		field.Text("text").Label("内容"),
	}
}

func (CreateComment) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("id").Label("评论 id"),
		field.String("discussion_id").Label("讨论串 id").Optional(),
	}
}

// CreateDatabase creates a database (including its first data source).
type CreateDatabase struct{}

func (CreateDatabase) Meta() contract.Meta {
	return contract.Meta{ID: "notion_db_create", Label: "建数据库"}
}

func (CreateDatabase) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("parent_page_id").Label("父页面 ID").Desc("数据库必须挂在某一页下"),
		field.String("title").Label("名称"),
		field.Object("properties", "列定义：Notion 的属性 schema，原样透传").Label("列").
			Desc(`例：{"名称":{"title":{}},"状态":{"select":{"options":[{"name":"待办"},{"name":"完成"}]}}}；` +
				`必须**恰好有一个** title 列，否则 Notion 拒绝`),
	}
}

func (CreateDatabase) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("database_id").Label("数据库 id"),
		field.String("data_source_id").Label("数据源 id").Desc("往里建行用这个").Optional(),
		field.String("url").Label("链接").Optional(),
	}
}

// UpdateSchema updates a data source's columns.
type UpdateSchema struct{}

func (UpdateSchema) Meta() contract.Meta {
	return contract.Meta{ID: "notion_db_update_schema", Label: "改表结构"}
}

func (UpdateSchema) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("data_source_id").Label("数据源 ID"),
		field.String("title").Label("改名").Desc("留空 = 不改").Optional(),
		field.Object("properties", "列定义：Notion 的属性 schema，原样透传").Label("列改动").
			Desc(`只给要改的列。加列 {"新列":{"number":{}}}；删列 {"旧列":null}；改名 {"旧列":{"name":"新名"}}`).Optional(),
	}
}

func (UpdateSchema) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("data_source_id").Label("数据源 id"),
		field.Array("properties", []PropSpec{}).Label("改完后的列"),
	}
}

// UploadFile sends a file from the platform's file layer into Notion.
type UploadFile struct{}

func (UploadFile) Meta() contract.Meta {
	return contract.Meta{ID: "notion_file_upload", Label: "上传文件"}
}

func (UploadFile) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.File("file").Label("文件"),
		field.String("filename").Label("文件名").Desc("留空 = 用文件自带的名字").Optional(),
	}
}

func (UploadFile) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		// The output is **a reference, not a link**: a Notion file must first be uploaded as a
		// file_upload, then referenced by a block or property.
		field.String("file_upload_id").Label("文件引用 id").
			Desc(`用法：追加块 {"type":"file","file":{"type":"file_upload","file_upload":{"id":"<这个 id>"}}}，` +
				`或写进 files 属性`),
		field.String("filename").Label("文件名").Optional(),
	}
}

// —— Credential ——

// Credential is this plugin's credential contract.
//
// Two auth methods coexist: set `token` (internal integration, simplest for self-hosting), or
// click "authorize" to go through Notion's consent screen (public integration, where the user
// checks which pages to grant). If neither is set, this credential isn't usable.
type Credential struct{}

// AuthMeta: Notion OAuth. **No scopes** — Notion's permissions are the pages the user checks on the
// consent screen, not something requested as a scope. The platform-side notion provider honors
// this (see server/internal/credential/oauth.go).
func (Credential) AuthMeta() contract.AuthMeta { return auth.OAuth("notion") }

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Secret("token").Label("内部集成密钥").
			Desc("notion.so/profile/integrations 建一个「内部」集成，「密钥」页复制 ntn_ 开头那串；" +
				"走 OAuth 授权则留空。**填完还要去 Notion 里把页面「⋯ → 连接」给这个集成**，否则什么都读不到").
			Optional(),
		field.Secret("access_token").Label("访问令牌（授权注入）").Desc("点「授权」后自动写入，勿手填").Optional(),
		field.Secret("refresh_token").Label("刷新令牌（授权注入）").Desc("勿手填").Optional(),
		// Notion is hosted abroad. Without this field, the plugin is dead on arrival for a
		// domestic deployment, and the only symptom is "timeout" — it doesn't show that the
		// network simply can't reach it (same lesson as the search plugin).
		field.Text("proxy").Label("出站代理").
			Desc("如 http://127.0.0.1:7897；部署环境直连不了 Notion 时必填").Optional(),
		// Event source configuration. A Notion webhook subscription can only be created by hand
		// on its integration settings page — the API can't create one — so "which tables to
		// watch" can only be decided by the credential.
		field.Text("watch_data_sources").Label("监听的数据源").
			Desc("逗号分隔的数据源 id 或链接（数据库链接直接贴即可）；留空 = 不产事件。" +
				"改完保存即生效，事件源会自动重启").Optional(),
		field.Text("poll_seconds").Label("轮询间隔（秒）").
			Desc("默认 60；Notion 限流 3 次/秒，盯的表越多别调得越短").Default("60"),
		field.Text("watch_cursor").Label("轮询游标").Desc("事件源自动维护，勿手填").Optional(),
	}
}

// —— Credential health check ——

// HealthCheck checks this credential: a single read of /users/me (the integration's own identity).
//
// The operation id must be health_check — the platform credential page's "check" button relies on
// this to decide whether this plugin can be verified as alive. When the token is invalid, this
// returns ok=false + message rather than an error: the platform treats an error as "this plugin
// can't run a health check" and ok=false as "the health check concluded unavailable", and it's the
// latter that needs to be said here.
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "检查凭证", TimeoutSec: 30,
		Desc: "读一次 Notion 的 /users/me，报令牌是否有效以及它代表哪个集成。" +
			"令牌有效不等于读得到某一页——页面还要在 Notion 里「⋯ → 连接」给这个集成"}
}

func (HealthCheck) Inputs() []contract.FieldSpec { return nil }

func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("可用"),
		// Name and workspace are there to help someone recognize which one this is: a person often
		// has several integrations (each connected to different pages), and "the token is valid"
		// alone doesn't tell them whether this credential is wired to the workspace they think it is.
		field.String("name").Label("集成名称").Optional(),
		field.String("workspace_name").Label("工作空间").Optional(),
		field.String("message").Label("说明"),
	}
}

// —— Events ——
//
// The two events share page_id / data_source_id (see Events.CommonFields): the platform flattens
// these into the top level of the trigger input, so both branches share the same variable — binding
// one when wiring up a "get page" step is enough, no need to fetch it separately per branch.
//
// The fields here are the layer that's "enough to decide whether to run, and what to run with".
// Content doesn't go into the event: a page can run to tens of KB, and the same page fans out to
// multiple workflows, so attaching a copy to each would multiply that content N times across the
// event payload and run records. Follow up with a "get page" step for content.

func pageEventFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("page_id").Label("页面 id"),
		field.String("data_source_id").Label("数据源 id").Desc("这一行属于哪张表"),
		field.String("url").Label("链接").Optional(),
		field.String("title").Label("标题").Optional(),
		field.String("created_time").Label("创建时间").Optional(),
		field.String("last_edited_time").Label("最后编辑时间").Optional(),
		field.Object("props", "属性名与类型由该数据源的表结构决定；值已归一化").Label("属性").Optional(),
	}
}

// PageCreated: a new row was added in a data source.
type PageCreated struct{}

func (PageCreated) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "page_created", Label: "新增行"}
}
func (PageCreated) Fields() []contract.FieldSpec { return pageEventFields() }

// PageUpdated: a row in a data source was changed.
type PageUpdated struct{}

func (PageUpdated) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "page_updated", Label: "行被修改"}
}
func (PageUpdated) Fields() []contract.FieldSpec { return pageEventFields() }

// Events declares the shared fields.
type Events struct{}

func (Events) CommonFields() []string { return []string{"page_id", "data_source_id"} }
