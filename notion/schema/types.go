package schema

// Element shapes used in operation outputs.
//
// One decision runs through all of it: **properties are given both ways**. `props` is the
// normalized flat value (`props.状态 = "进行中"`), directly readable by downstream steps and models;
// `properties_raw` is Notion's nested JSON as-is. Giving only the latter means people have to pore
// over Notion's docs to assemble `{"状态":{"status":{"name":"进行中"}}}`; giving only the former
// leaves no fallback for types like rollup/formula where normalization inevitably loses information.

// SearchItem is one search hit (a page or a data source).
type SearchItem struct {
	ID             string `sokel:"id" label:"id"`
	Object         string `sokel:"object" label:"类别" desc:"page / data_source / database"`
	Title          string `sokel:"title,optional" label:"标题"`
	URL            string `sokel:"url,optional" label:"链接"`
	ParentType     string `sokel:"parent_type,optional" label:"父类别" desc:"page / data_source / database / workspace"`
	ParentID       string `sokel:"parent_id,optional" label:"父 id"`
	LastEditedTime string `sokel:"last_edited_time,optional" label:"最后编辑时间"`
}

// PageItem is one page's metadata (**no content**).
//
// Content has to be fetched separately via "get page": querying 100 rows at once and attaching
// each row's full markdown would be slow and bloat the run record, and most workflows only need to
// filter by properties anyway.
type PageItem struct {
	ID             string         `sokel:"id" label:"页面 id"`
	URL            string         `sokel:"url,optional" label:"链接"`
	Title          string         `sokel:"title,optional" label:"标题"`
	Icon           string         `sokel:"icon,optional" label:"图标" desc:"emoji 或图片链接"`
	ParentType     string         `sokel:"parent_type,optional" label:"父类别"`
	ParentID       string         `sokel:"parent_id,optional" label:"父 id"`
	CreatedTime    string         `sokel:"created_time,optional" label:"创建时间"`
	LastEditedTime string         `sokel:"last_edited_time,optional" label:"最后编辑时间"`
	InTrash        bool           `sokel:"in_trash,optional" label:"在回收站"`
	Props          map[string]any `sokel:"props,optional" label:"属性" opaque:"属性名与类型由该数据源的表结构决定，运行时才知道；值已归一化（文本→字符串、多选→字符串数组、日期→{start,end}）"`
}

// PropSpec is one column in a schema.
//
// Writable isn't decorative: formula / rollup / created_time are computed by Notion, and writing
// to them always errors. Flagging it in the schema is better than letting people build a whole
// workflow and hit the wall afterward.
type PropSpec struct {
	Name     string   `sokel:"name" label:"列名" desc:"写入属性时用的键"`
	ID       string   `sokel:"id,optional" label:"列 id"`
	Type     string   `sokel:"type" label:"类型" desc:"title / rich_text / number / select / status / multi_select / date / people / files / checkbox / url / email / phone_number / relation / formula / rollup …"`
	Options  []string `sokel:"options,optional" label:"候选值" desc:"select / status / multi_select 才有"`
	Writable bool     `sokel:"writable" label:"可写" desc:"false = Notion 算出来的（formula/rollup/created_time…），写入会被拒"`
}

// DataSourceRef is one data source under a database.
//
// Since 2025-09-03, a database is just a container — **both the schema and the rows live on the
// data source** — so queries and row creation both use its id.
type DataSourceRef struct {
	ID   string `sokel:"id" label:"数据源 id"`
	Name string `sokel:"name,optional" label:"名称"`
}

// User is a workspace member or integration bot.
type User struct {
	ID        string `sokel:"id" label:"用户 id"`
	Name      string `sokel:"name,optional" label:"名字"`
	Type      string `sokel:"type,optional" label:"类别" desc:"person / bot"`
	Email     string `sokel:"email,optional" label:"邮箱" desc:"仅 person，且集成有权限时才有"`
	AvatarURL string `sokel:"avatar_url,optional" label:"头像"`
}

// Comment is a single comment.
type Comment struct {
	ID           string `sokel:"id" label:"评论 id"`
	DiscussionID string `sokel:"discussion_id,optional" label:"讨论串 id" desc:"回同一串评论时带上它"`
	Text         string `sokel:"text,optional" label:"内容（纯文本）"`
	CreatedTime  string `sokel:"created_time,optional" label:"时间"`
	CreatedByID  string `sokel:"created_by_id,optional" label:"作者 id"`
}
