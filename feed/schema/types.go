package schema

// Item is one piece of content. **All sources are normalized into this one shape** — that's the
// entire point of this plugin.
//
// Borrows RSSHub's design (uniform item shape + one adapter per source), but **does not depend
// on it**: we hit each site's API directly and produce JSON instead of XML. Add a conversion
// step on the canvas if RSS is needed; doing the reverse (using XML as the intermediate shape)
// would mean every downstream node has to parse XML first.
type Item struct {
	ID          string   `sokel:"id" label:"条目 id" desc:"来源给的唯一标识；没有时用链接的哈希"`
	Title       string   `sokel:"title,optional" label:"标题"`
	URL         string   `sokel:"url,optional" label:"原文链接"`
	Summary     string   `sokel:"summary,optional" label:"摘要" desc:"纯文本，已去掉 HTML 标签"`
	ContentHTML string   `sokel:"content_html,optional" label:"正文 HTML" desc:"来源给多少是多少；RSS 常常只有摘要"`
	Author      string   `sokel:"author,optional" label:"作者"`
	PublishedAt string   `sokel:"published_at,optional" label:"发布时间" desc:"RFC3339；来源没给就是空"`
	Source      string   `sokel:"source,optional" label:"来源" desc:"vendor 名 + 具体源（如 xueqiu_user:1234）"`
	Tags        []string `sokel:"tags,optional" label:"标签"`
	Images      []string `sokel:"images,optional" label:"图片地址"`
	// DedupKey is what downstream storage upserts by. Same convention as this platform's
	// other data-fetching plugins.
	DedupKey string `sokel:"dedup_key,optional" label:"去重键"`
}
