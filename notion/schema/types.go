package schema

// 出参里的元素形状。
//
// 一条贯穿全篇的判断：**属性给两份**。`props` 是归一化后的平铺值（`props.状态 = "进行中"`），
// 下游与模型直接可读；`properties_raw` 是 Notion 原样的嵌套 JSON。只给后者等于让人对着
// Notion 文档拼 `{"状态":{"status":{"name":"进行中"}}}`，只给前者则 rollup/formula 这类
// 归一化必然丢信息的类型没有退路。

// SearchItem：搜索命中的一条（页面或数据源）。
type SearchItem struct {
	ID             string `sokel:"id" label:"id"`
	Object         string `sokel:"object" label:"类别" desc:"page / data_source / database"`
	Title          string `sokel:"title,optional" label:"标题"`
	URL            string `sokel:"url,optional" label:"链接"`
	ParentType     string `sokel:"parent_type,optional" label:"父类别" desc:"page / data_source / database / workspace"`
	ParentID       string `sokel:"parent_id,optional" label:"父 id"`
	LastEditedTime string `sokel:"last_edited_time,optional" label:"最后编辑时间"`
}

// PageItem：一页的元信息（**不含正文**）。
//
// 正文要用「读页面」再拉：一次查 100 行、每行都把整页 markdown 带上，
// 既慢又会把运行记录撑爆，而绝大多数流程只按属性筛选。
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

// PropSpec：表结构里的一列。
//
// Writable 不是装饰：formula / rollup / created_time 这些是 Notion 算出来的，
// 往里写一律报错。表结构里就标出来，比让人写完流程再撞一次墙强。
type PropSpec struct {
	Name     string   `sokel:"name" label:"列名" desc:"写入属性时用的键"`
	ID       string   `sokel:"id,optional" label:"列 id"`
	Type     string   `sokel:"type" label:"类型" desc:"title / rich_text / number / select / status / multi_select / date / people / files / checkbox / url / email / phone_number / relation / formula / rollup …"`
	Options  []string `sokel:"options,optional" label:"候选值" desc:"select / status / multi_select 才有"`
	Writable bool     `sokel:"writable" label:"可写" desc:"false = Notion 算出来的（formula/rollup/created_time…），写入会被拒"`
}

// DataSourceRef：数据库下的一个数据源。
//
// 2025-09-03 起 database 只是容器，**表结构与行都在数据源上**——查询与建行用的都是它的 id。
type DataSourceRef struct {
	ID   string `sokel:"id" label:"数据源 id"`
	Name string `sokel:"name,optional" label:"名称"`
}

// User：工作空间成员或集成机器人。
type User struct {
	ID        string `sokel:"id" label:"用户 id"`
	Name      string `sokel:"name,optional" label:"名字"`
	Type      string `sokel:"type,optional" label:"类别" desc:"person / bot"`
	Email     string `sokel:"email,optional" label:"邮箱" desc:"仅 person，且集成有权限时才有"`
	AvatarURL string `sokel:"avatar_url,optional" label:"头像"`
}

// Comment：一条评论。
type Comment struct {
	ID           string `sokel:"id" label:"评论 id"`
	DiscussionID string `sokel:"discussion_id,optional" label:"讨论串 id" desc:"回同一串评论时带上它"`
	Text         string `sokel:"text,optional" label:"内容（纯文本）"`
	CreatedTime  string `sokel:"created_time,optional" label:"时间"`
	CreatedByID  string `sokel:"created_by_id,optional" label:"作者 id"`
}
