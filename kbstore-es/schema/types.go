// Package schema 声明 kbstore-es 的操作契约。
//
// 这些类型此前根本不存在——契约里写的是 []map[string]any / map[string]any，
// 形状只活在 buildBool、mergeChunks 那些函数里，靠 f["field"].(string) 这样的断言现取。
// 迁移时把它们从代码里读出来写成类型：形状本来就是确定的，只是当年图省事没写。
package schema

// Filter 一条过滤条件。
type Filter struct {
	Field   string   `json:"field,omitempty" sokel:"field" label:"字段名"`
	Values  []string `json:"values,omitempty" sokel:"values,optional" label:"取值（任一命中）"`
	Missing bool     `json:"missing,omitempty" sokel:"missing,optional" label:"取「该字段不存在」" desc:"为真时忽略 values"`
	Exclude bool     `json:"exclude,omitempty" sokel:"exclude,optional" label:"反选" desc:"命中的排除掉"`
}

// TimeRange 时间范围（闭区间，空表示不限）。
type TimeRange struct {
	From string `json:"from,omitempty" sokel:"from,optional" label:"起（含）" desc:"日期串，如 2026-01-01"`
	To   string `json:"to,omitempty" sokel:"to,optional" label:"止（含）"`
}

// Recency 时效加权：越接近 pivot 的内容得分越高。
type Recency struct {
	Pivot string  `json:"pivot,omitempty" sokel:"pivot" label:"基准时间" desc:"如 2026-07-01"`
	Boost float64 `json:"boost,omitempty" sokel:"boost,optional" label:"权重"`
}

// MetaField 元数据字段声明（建库/改库时用）。
type MetaField struct {
	Field string `json:"field,omitempty" sokel:"field" label:"字段名"`
	Type  string `json:"type,omitempty" sokel:"type,optional" label:"类型" desc:"keyword / text / number / date；留空按 keyword"`
}

// Chunk 一个分块。id 之外的字段随知识库配置而定，这里列的是引擎认识的全集。
type Chunk struct {
	ID          string `json:"id,omitempty" sokel:"id" label:"分块 id"`
	Content     string `json:"content,omitempty" sokel:"content,optional" label:"正文"`
	Title       string `json:"title,omitempty" sokel:"title,optional" label:"标题"`
	Summary     string `json:"summary,omitempty" sokel:"summary,optional" label:"摘要"`
	Datetime    string `json:"datetime,omitempty" sokel:"datetime,optional" label:"内容时间"`
	DocID       string `json:"doc_id,omitempty" sokel:"doc_id,optional" label:"文档 id"`
	Role        string `json:"role,omitempty" sokel:"role,optional" label:"角色" desc:"parent / child"`
	ParentID    string `json:"parent_id,omitempty" sokel:"parent_id,optional" label:"父块 id"`
	ParentNo    int    `json:"parent_no" sokel:"parent_no,optional" label:"父块序号"`
	ChildNo     int    `json:"child_no" sokel:"child_no,optional" label:"子块序号"`
	PageNo      int    `json:"page_no" sokel:"page_no,optional" label:"页码"`
	ContentHTML string `json:"content_html,omitempty" sokel:"content_html,optional" label:"正文 HTML"`
	Boundary    string `json:"boundary,omitempty" sokel:"boundary,optional" label:"边界"`
	// 这是本插件唯一保留的无结构字段，理由显式写在 opaque tag 里：
	// 键来自本库声明的 metaFields，值类型随该字段的 type 而变（keyword→字符串、
	// number→数字、keyword[]→字符串数组），运行期才知道，没有可声明的统一结构。
	// embedding 仅在 chunks_upsert 上行携带（召回单位=子块）；读路径按 srcFields 白名单排除,不回流。
	// 声明式迁移时曾整个漏掉——绑定把平台发来的向量静默丢弃,新写入的文档向量检索全空(实机)。
	Embedding []float32      `json:"embedding,omitempty" sokel:"embedding,optional" label:"向量" desc:"召回单位(子块)的稠密向量;仅写入时携带"`
	Fields    map[string]any `json:"fields,omitempty" sokel:"fields,optional" label:"元数据" opaque:"键与值类型由本库声明的元数据字段决定，运行期才确定"`
	Images    []string       `json:"images,omitempty" sokel:"images,optional" label:"图片"`
	// assets / source_blocks 是**对象数组**，不是字符串数组：平台发的是
	// {kind,url,…} 与 {块 id,type,bbox,页码}（见 pipeline/splitter.go 的 Parent）。
	// 声明成 []string 会在绑定时报「cannot unmarshal object into Go value of type string」——
	// 而且只有带溯源/资产的文档才会踩到，平时看着一切正常。
	// 本插件只是原样透传进 ES（payload 不索引），所以不声明结构。
	Assets       []map[string]any `json:"assets,omitempty" sokel:"assets,optional" label:"资产" opaque:"块级资产 {kind,url,…}，形状由上游解析器决定，本插件只透传"`
	SourceBlocks []map[string]any `json:"source_blocks,omitempty" sokel:"source_blocks,optional" label:"溯源块" opaque:"溯源引用 {块 id,type,bbox,页码}，形状由上游解析器决定，本插件只透传"`
}

// Hit 一条检索命中。
type Hit struct {
	ID    string  `json:"id,omitempty" sokel:"id" label:"分块 id"`
	Score float64 `json:"score,omitempty" sokel:"score" label:"相关度"`
	Chunk Chunk   `json:"chunk,omitempty" sokel:"chunk" label:"分块内容"`
}
