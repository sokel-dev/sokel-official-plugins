package main

// RSS/Atom adapter: **input is XML, output is JSON**.
//
// XML is only our interface format with the outside world; it never leaks to downstream nodes —
// handing XML downstream would mean every node has to parse it itself.
//
// Three tolerances are necessary (especially for domestic feeds):
//   - **Encoding**: plenty of sites use GBK, which would come out garbled if parsed as UTF-8
//     (see charset.go);
//   - **RSS and Atom use different field names** (item/entry, pubDate/updated,
//     description/summary); one parser has to recognize both, otherwise "switch feeds and it's
//     empty";
//   - **Non-strict mode**: invalid entities (like &nbsp;) are common in domestic feeds; strict
//     mode would fail to parse the whole document.

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
	// Author: **can't have a separate field for RSS and one for Atom** — encoding/xml
	// doesn't allow `author` and `author>name` to coexist (the whole parse would fail).
	// Use a single nested struct that catches both shapes.
	Author     authorField `xml:"author"`
	Creator    string      `xml:"creator"` // dc:creator
	Categories []string    `xml:"category"`
}

// authorField: Atom uses <author><name>Alice</name></author>, RSS uses <author>a@b.com</author>.
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

// atomLink: RSS uses <link>url</link>, Atom uses <link href="…"/>. Both need to be recognized.
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

// parseTime: RSS uses RFC1123, Atom uses RFC3339, and plenty of feeds follow neither.
// **If it can't be recognized, leave it blank — don't guess**: guessing wrong makes the cursor
// skip genuinely new items.
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
	// Inline tags are **removed without leaving a space**: search-result highlighting uses
	// <em>, and replacing it with a space the way block-level tags are handled would turn
	// "贵州<em>茅台</em>三季报" into "贵州 茅台 三季报" — those two spaces stand out badly
	// in Chinese text.
	inlineTagRe = regexp.MustCompile(`(?is)</?(em|strong|b|i|u|span|mark|font)[^>]*>`)
	imgRe       = regexp.MustCompile(`(?i)<img[^>]+src\s*=\s*["']([^"']+)["']`)
	wsRe        = regexp.MustCompile(`\s+`)
)

// plainText produces the plain text used for the summary — what downstream consumers ("feed
// to a model", "send a notification") want, whereas an RSS description is often a blob of raw
// HTML. The HTML body is kept in a separate field, so nothing is lost.
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
