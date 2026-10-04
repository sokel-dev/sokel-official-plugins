// Package schema 声明 notion 插件的操作、事件与凭证契约。
//
// 三条贯穿全篇的判断：
//
//  1. **按数据源建模，不留 database_id 的老形状**。2025-09-03 起 Notion 把 database 拆成
//     「容器（database）+ 表（data_source）」，一个库可以挂多个数据源；查询、建行、关联
//     用的都是 data_source_id。按旧形状做出来的契约，用户一给多源库就全线报错，
//     再改就是破坏性迁移（n8n 已经经历过一次）。所以从第一天就是数据源。
//
//  2. **正文走 markdown，块树只作兜底**。Notion 的 markdown 读写接口
//     （GET/PATCH /v1/pages/{id}/markdown）让「读一页给模型 / 让模型改一页」变成一次调用，
//     而块树要递归拉、要按 25 种块类型拼。块级接口仍然保留（notion_block_children /
//     notion_block_append），因为数据库块、嵌入这类东西 markdown 表达不了。
//
//  3. **属性给两份**（见 types.go 顶部）：`props` 归一化、`properties_raw` 原样。
//
// 认证两种并存：内部集成密钥（凭证里填 `ntn_` 开头的 token）或 OAuth 授权。
// 填了 token 就用 token，没填则用授权拿到的 access_token——两种都是「这个集成能看到
// 哪些页面」的凭据，没必要拆成两条凭证行。
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/auth"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// —— 读 ——

// Search 按标题搜页面与数据源。
type Search struct{}

func (Search) Meta() contract.Meta {
	return contract.Meta{ID: "notion_search", Label: "搜索"}
}

func (Search) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		// Notion 的搜索**只匹配标题**，不搜正文。写清楚，否则「搜不到」会被当成 bug。
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

// GetPage 读一页：属性 + markdown 正文。
type GetPage struct{}

func (GetPage) Meta() contract.Meta {
	return contract.Meta{ID: "notion_page_get", Label: "读页面"}
}

func (GetPage) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("page_id").Label("页面 ID").Desc("贴完整 Notion 链接也行，会自己抠出 id"),
		// 正文是另一次请求，且大页面可能几十 KB——只要属性时不该白花这一次往返。
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
		// 超大页面 Notion 会截断。不把这个字说出来，下游会把半页内容当成全文去喂模型。
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

// QueryDataSource 查数据源的行（= 老说法里的「查数据库」）。
type QueryDataSource struct{}

func (QueryDataSource) Meta() contract.Meta {
	return contract.Meta{ID: "notion_db_query", Label: "查询数据源"}
}

func (QueryDataSource) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("data_source_id").Label("数据源 ID").
			Desc("用「列出数据源」从数据库 id 取；贴数据库链接时会当作单源库自动取第一个数据源"),
		// 过滤/排序**不归一化**：Notion 的过滤 DSL 是嵌套的与或组合，
		// 归一化出来的小语言只会覆盖不到一半的写法，而剩下一半没有退路。
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

// GetSchema 取数据源的表结构。
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

// ListDataSources 列出一个数据库下的数据源。
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

// ListBlocks 读子块（markdown 表达不了的东西才用它）。
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

// ListUsers 列工作空间成员。
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

// GetUser 读一个成员。
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

// ListComments 列一页/一块上的评论。
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

// —— 写 ——

// CreatePage 建页面：父可以是另一页，也可以是数据源（= 建一行）。
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

// UpdatePage 改页面属性（正文用「改正文」）。
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

// UpdateContent 改页面正文（markdown 三态）。
type UpdateContent struct{}

func (UpdateContent) Meta() contract.Meta {
	return contract.Meta{ID: "notion_page_content", Label: "改页面正文"}
}

func (UpdateContent) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("page_id").Label("页面 ID"),
		// 三态对应 Notion 的三种写法。edit 是官方推荐给模型用的那种——
		// 让模型重写整页既贵又容易把没提到的内容删掉。
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

// TrashPage 把页面移进回收站，或从回收站恢复。
type TrashPage struct{}

func (TrashPage) Meta() contract.Meta {
	return contract.Meta{ID: "notion_page_trash", Label: "删除/恢复页面"}
}

func (TrashPage) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("page_id").Label("页面 ID"),
		// Notion 的 API 只能进回收站，删不掉——写清楚，免得有人以为数据没了。
		field.Bool("restore").Label("改为恢复").Desc("勾选 = 从回收站拿回来").Optional(),
	}
}

func (TrashPage) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("id").Label("页面 id"),
		field.Bool("in_trash").Label("在回收站").Desc("API 只能移进回收站，不能彻底删除"),
	}
}

// AppendBlocks 追加原始块（markdown 写不出来的东西用它）。
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

// CreateComment 加评论。
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

// CreateDatabase 建数据库（含它的第一个数据源）。
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

// UpdateSchema 改数据源的列。
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

// UploadFile 把平台文件层里的文件传进 Notion。
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
		// 出参是**引用而不是链接**：Notion 的文件要先传成 file_upload，再由块或属性引用它。
		field.String("file_upload_id").Label("文件引用 id").
			Desc(`用法：追加块 {"type":"file","file":{"type":"file_upload","file_upload":{"id":"<这个 id>"}}}，` +
				`或写进 files 属性`),
		field.String("filename").Label("文件名").Optional(),
	}
}

// —— 凭证 ——

// Credential 本插件的凭证契约。
//
// 两种认证并存：填 `token`（内部集成，自己部署最省事），或点「授权」走 Notion 的同意页
// （公开集成，用户自己勾要给的页面）。两者都不填 = 这条凭证不可用。
type Credential struct{}

// AuthMeta：Notion OAuth。**没有作用域**——Notion 的权限是用户在同意页上勾哪些页面，
// 不是申请 scope。平台侧的 notion provider 认这一点（见 server/internal/credential/oauth.go）。
func (Credential) AuthMeta() contract.AuthMeta { return auth.OAuth("notion") }

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Secret("token").Label("内部集成密钥").
			Desc("notion.so/profile/integrations 建一个「内部」集成，「密钥」页复制 ntn_ 开头那串；" +
				"走 OAuth 授权则留空。**填完还要去 Notion 里把页面「⋯ → 连接」给这个集成**，否则什么都读不到").
			Optional(),
		field.Secret("access_token").Label("访问令牌（授权注入）").Desc("点「授权」后自动写入，勿手填").Optional(),
		field.Secret("refresh_token").Label("刷新令牌（授权注入）").Desc("勿手填").Optional(),
		// Notion 在境外。没有这一项，插件在国内部署装上就是废的，而症状只是「超时」，
		// 看不出是网络不通（与搜索插件同一条教训）。
		field.Text("proxy").Label("出站代理").
			Desc("如 http://127.0.0.1:7897；部署环境直连不了 Notion 时必填").Optional(),
		// 事件源的配置。Notion 的 webhook 订阅只能在它的集成设置页手工建、API 建不了，
		// 所以「盯哪些表」只能由凭证说了算。
		field.Text("watch_data_sources").Label("监听的数据源").
			Desc("逗号分隔的数据源 id 或链接（数据库链接直接贴即可）；留空 = 不产事件。" +
				"改完保存即生效，事件源会自动重启").Optional(),
		field.Text("poll_seconds").Label("轮询间隔（秒）").
			Desc("默认 60；Notion 限流 3 次/秒，盯的表越多别调得越短").Default("60"),
		field.Text("watch_cursor").Label("轮询游标").Desc("事件源自动维护，勿手填").Optional(),
	}
}

// —— 凭证体检 ——

// HealthCheck 体检这条凭证：读一次 /users/me（集成自己的身份）。
//
// 操作 id 必须是 health_check——平台凭证页的「检查」按钮据此判断这个插件能不能验活。
// 令牌无效时返回 ok=false + message 而不是 error：平台把 error 当「这个插件没法体检」，
// 把 ok=false 当「体检结论是不可用」，后者才是这里要说的话。
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
		// 名称与工作空间是用来认人的：一个人手上常有好几个集成（各连不同页面），
		// 光说「令牌有效」看不出这条凭证连的是不是他以为的那个工作空间。
		field.String("name").Label("集成名称").Optional(),
		field.String("workspace_name").Label("工作空间").Optional(),
		field.String("message").Label("说明"),
	}
}

// —— 事件 ——
//
// 两个事件共享 page_id / data_source_id（见 Events.CommonFields）：平台把它们平铺到
// 触发输入顶层，两条分支共用同一变量——接「读页面」时绑一个就够，不必按分支分别取。
//
// 字段是「够用来决定要不要跑、以及跑什么」的那一层。正文不进事件：一页可能几十 KB，
// 而同一页会扇出给多个工作流，每个都带一份等于把内容复制 N 遍塞进事件载荷与运行记录。
// 要正文就接一个「读页面」。

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

// PageCreated 数据源里新增了一行。
type PageCreated struct{}

func (PageCreated) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "page_created", Label: "新增行"}
}
func (PageCreated) Fields() []contract.FieldSpec { return pageEventFields() }

// PageUpdated 数据源里某一行被改了。
type PageUpdated struct{}

func (PageUpdated) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "page_updated", Label: "行被修改"}
}
func (PageUpdated) Fields() []contract.FieldSpec { return pageEventFields() }

// Events 声明公共字段。
type Events struct{}

func (Events) CommonFields() []string { return []string{"page_id", "data_source_id"} }
