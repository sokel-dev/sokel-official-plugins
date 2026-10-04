package schema

// Item：一条内容。**所有来源归一到这一个形状**——这是本插件全部的意义所在。
//
// 借鉴 RSSHub 的判断（统一条目形状 + 一源一适配器），但**不依赖它**：
// 我们直接打各站点的接口，产出 JSON 而不是 XML。要 RSS 的话在画布上加一步转换即可，
// 反过来（拿 XML 当中间形态）会让每个下游节点都得先解一次 XML。
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
	// DedupKey：下游落库按它 upsert。与本平台其它取数插件同一条约定。
	DedupKey string `sokel:"dedup_key,optional" label:"去重键"`
}
