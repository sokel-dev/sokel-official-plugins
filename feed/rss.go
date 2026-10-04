package main

// RSS/Atom 适配器：**输入是 XML，输出是 JSON**。
//
// XML 只是我们与外界的接口格式，不外泄给下游——给下游 XML 等于让每个节点都先解一次。
//
// 三条容错是必需的（国内源尤其）：
//   - **编码**：不少站点是 GBK，按 UTF-8 解会得到一串乱码（见 charset.go）；
//   - **RSS 与 Atom 字段名不同**（item/entry、pubDate/updated、description/summary），
//     一个解析器要同时认，否则「换个源就空了」；
//   - **非严格模式**：国内源里非法实体（&nbsp; 之类）很常见，严格模式会整份解不动。

import (
	"encoding/xml"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-official-plugins/feed/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

type rssFeed struct {
	Channel struct {
		Title string    `xml:"title"`
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
	Title   string    `xml:"title"` // Atom
	Entries []rssItem `xml:"entry"` // Atom
}

type rssItem struct {
	Title       string     `xml:"title"`
	Links       []atomLink `xml:"link"`
	GUID        string     `xml:"guid"`
	ID          string     `xml:"id"`
	Description string     `xml:"description"`
	Summary     string     `xml:"summary"`
	Content     string     `xml:"content"`
	Encoded     string     `xml:"encoded"` // content:encoded
	PubDate     string     `xml:"pubDate"`
	Updated     string     `xml:"updated"`
	Published   string     `xml:"published"`
	// 作者：**RSS 与 Atom 不能各写一个字段**——encoding/xml 不允许 `author` 与 `author>name`
	// 同时存在（会整份解析失败）。用一个嵌套结构同时接住两种形状。
	Author     authorField `xml:"author"`
	Creator    string      `xml:"creator"` // dc:creator
	Categories []string    `xml:"category"`
}

// authorField：Atom 是 <author><name>老王</name></author>，RSS 是 <author>a@b.com</author>。
type authorField struct {
	Name string `xml:"name"`
	Text string `xml:",chardata"`
}

func (a authorField) value() string {
	if n := strings.TrimSpace(a.Name); n != "" {
		return n
	}
	return strings.TrimSpace(a.Text)
}

// atomLink：RSS 的是 <link>地址</link>，Atom 的是 <link href="…"/>。两种都要认。
type atomLink struct {
	Href string `xml:"href,attr"`
	Text string `xml:",chardata"`
}

func (l atomLink) value() string {
	if h := strings.TrimSpace(l.Href); h != "" {
		return h
	}
	return strings.TrimSpace(l.Text)
}

func fetchRSS(ctx plugin.Ctx, target string) ([]schema.Item, error) {
	uri := strings.TrimSpace(target)
	if !strings.HasPrefix(uri, "http://") && !strings.HasPrefix(uri, "https://") {
		return nil, fmt.Errorf("RSS 来源要填完整的 feed 地址（http:// 或 https:// 开头），得到 %q", target)
	}
	raw, err := get(ctx, uri, "", "")
	if err != nil {
		return nil, err
	}
	var f rssFeed
	dec := xml.NewDecoder(strings.NewReader(string(raw)))
	dec.CharsetReader = charsetReader
	dec.Strict = false
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("解析 feed 失败（前 160 字：%s）: %w", clip(raw, 160), err)
	}
	src := firstNonEmpty(f.Channel.Title, f.Title, hostOf(uri))
	entries := f.Channel.Items
	if len(entries) == 0 {
		entries = f.Entries
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("这个地址解出来一条内容都没有（是 feed 地址吗？前 160 字：%s）", clip(raw, 160))
	}
	items := make([]schema.Item, 0, len(entries))
	for _, e := range entries {
		items = append(items, toItem(e, "rss:"+src))
	}
	return items, nil
}

func toItem(e rssItem, source string) schema.Item {
	link := ""
	for _, l := range e.Links {
		if v := l.value(); v != "" {
			link = v
			break
		}
	}
	id := firstNonEmpty(strings.TrimSpace(e.GUID), strings.TrimSpace(e.ID), link, strings.TrimSpace(e.Title))
	body := firstNonEmpty(e.Encoded, e.Content, e.Description, e.Summary)
	it := schema.Item{
		ID: id, Title: strings.TrimSpace(e.Title), URL: link,
		ContentHTML: body, Summary: plainText(body),
		Author:      firstNonEmpty(strings.TrimSpace(e.Creator), e.Author.value()),
		PublishedAt: parseTime(firstNonEmpty(e.PubDate, e.Published, e.Updated)),
		Source:      source, Tags: trimAll(e.Categories),
		DedupKey: "feed:" + shortHash(id),
	}
	it.Images = imagesIn(body)
	return it
}

// parseTime：RSS 用 RFC1123，Atom 用 RFC3339，还有一堆源两者都不守。
// **认不出就留空，不猜**——猜错会让游标跳过真实的新条目。
func parseTime(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, layout := range []string{
		time.RFC1123Z, time.RFC1123, time.RFC3339, time.RFC822Z, time.RFC822,
		"2006-01-02T15:04:05Z07:00", "2006-01-02 15:04:05", "2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	return ""
}

var (
	tagRe = regexp.MustCompile(`(?s)<[^>]*>`)
	// 行内标签**去掉不留空格**：搜索结果的高亮是 <em>，按块级标签那样换成空格的话，
	// 「贵州<em>茅台</em>三季报」会变成「贵州 茅台 三季报」——中文里那两个空格很扎眼。
	inlineTagRe = regexp.MustCompile(`(?is)</?(em|strong|b|i|u|span|mark|font)[^>]*>`)
	imgRe       = regexp.MustCompile(`(?i)<img[^>]+src\s*=\s*["']([^"']+)["']`)
	wsRe        = regexp.MustCompile(`\s+`)
)

// plainText：摘要给的是纯文本——下游「喂给模型」「发通知」时要的是这个，
// 而 RSS 的 description 里常常是一整坨 HTML。正文 HTML 另有字段，不丢。
func plainText(html string) string {
	s := inlineTagRe.ReplaceAllString(html, "")
	s = tagRe.ReplaceAllString(s, " ")
	s = strings.NewReplacer("&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'").Replace(s)
	s = strings.TrimSpace(wsRe.ReplaceAllString(s, " "))
	if len([]rune(s)) > 500 {
		s = string([]rune(s)[:500]) + "…"
	}
	return s
}

func imagesIn(html string) []string {
	var out []string
	for _, m := range imgRe.FindAllStringSubmatch(html, -1) {
		if len(m) == 2 {
			out = append(out, m[1])
		}
	}
	return out
}

func trimAll(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
