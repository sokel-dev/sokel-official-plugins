// Package schema declares kbstore-pgvector's operation contracts.
//
// **Copied verbatim from kbstore-es's**—this is the whole point of this plugin existing: v4 claims
// "the storage engine is swappable", but with only one implementation, there's no way to tell how
// much of the contract's shape is secretly ES-shaped. A second implementation either satisfies it
// as-is, or surfaces where it doesn't fit (whatever gets surfaced is recorded in
// docs/contract-notes.md).
package schema

// Filter is a single filter condition.
type Filter struct {
	Field   string   `json:"field,omitempty" sokel:"field" label:"字段名"`
	Values  []string `json:"values,omitempty" sokel:"values,optional" label:"取值（任一命中）"`
	Missing bool     `json:"missing,omitempty" sokel:"missing,optional" label:"取「该字段不存在」" desc:"为真时忽略 values"`
	Exclude bool     `json:"exclude,omitempty" sokel:"exclude,optional" label:"反选" desc:"命中的排除掉"`
	Gte     string   `json:"gte,omitempty" sokel:"gte,optional" label:"下限（含）" desc:"范围过滤：能解析成数字的按数值比，否则按文本比（日期是 YYYY-MM-DD）"`
	Lte     string   `json:"lte,omitempty" sokel:"lte,optional" label:"上限（含）"`
}

// TimeRange is a time range (closed interval; empty means unbounded).
type TimeRange struct {
	From string `json:"from,omitempty" sokel:"from,optional" label:"起（含）" desc:"日期串，如 2026-01-01"`
	To   string `json:"to,omitempty" sokel:"to,optional" label:"止（含）"`
}

// Recency is a recency boost: content closer to pivot scores higher.
type Recency struct {
	Pivot string  `json:"pivot,omitempty" sokel:"pivot" label:"基准时间" desc:"如 2026-07-01"`
	Boost float64 `json:"boost,omitempty" sokel:"boost,optional" label:"权重"`
}

// MetaField is a metadata field declaration (used when creating/altering a knowledge base).
type MetaField struct {
	Field string `json:"field,omitempty" sokel:"field" label:"字段名"`
	Type  string `json:"type,omitempty" sokel:"type,optional" label:"类型" desc:"keyword / text / number / date；留空按 keyword"`
}

// Chunk is a single chunk. Fields other than id vary with the knowledge base's configuration; this
// lists the full set the engine understands.
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
	// This is the only unstructured field this plugin keeps, and the reason is spelled out
	// explicitly in the opaque tag: the keys come from this knowledge base's declared metaFields,
	// and the value type varies with that field's type (keyword→string, number→number,
	// keyword[]→string array), only known at runtime — there's no uniform structure to declare.
	// embedding is only carried on the chunks_upsert upload path (retrieval unit = child chunk); the
	// read path excludes it via the srcFields allowlist and never sends it back.
	// This field was entirely missed during the declarative migration once — binding silently
	// dropped the vector the platform sent, and newly written documents had empty vector search
	// results (seen in production).
	Embedding []float32      `json:"embedding,omitempty" sokel:"embedding,optional" label:"向量" desc:"召回单位(子块)的稠密向量;仅写入时携带"`
	Fields    map[string]any `json:"fields,omitempty" sokel:"fields,optional" label:"元数据" opaque:"键与值类型由本库声明的元数据字段决定，运行期才确定"`
	Images    []string       `json:"images,omitempty" sokel:"images,optional" label:"图片"`
	// assets / source_blocks are **arrays of objects**, not arrays of strings: the platform sends
	// {kind,url,…} and {block id,type,bbox,page number} (see Parent in pipeline/splitter.go).
	// Declaring them as []string would fail binding with "cannot unmarshal object into Go value of
	// type string" — and only documents carrying provenance/assets would ever hit it, so everything
	// looks fine the rest of the time.
	// This plugin just passes them through into ES as-is (the payload isn't indexed), so no
	// structure is declared.
	Assets       []map[string]any `json:"assets,omitempty" sokel:"assets,optional" label:"资产" opaque:"块级资产 {kind,url,…}，形状由上游解析器决定，本插件只透传"`
	SourceBlocks []map[string]any `json:"source_blocks,omitempty" sokel:"source_blocks,optional" label:"溯源块" opaque:"溯源引用 {块 id,type,bbox,页码}，形状由上游解析器决定，本插件只透传"`
}

// Hit is a single retrieval hit.
type Hit struct {
	ID    string  `json:"id,omitempty" sokel:"id" label:"分块 id"`
	Score float64 `json:"score,omitempty" sokel:"score" label:"相关度"`
	Chunk Chunk   `json:"chunk,omitempty" sokel:"chunk" label:"分块内容"`
}
