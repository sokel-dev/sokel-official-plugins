package main

// Notion 的应答形状（只声明用得上的字段）与它们到契约形状的转换。
//
// 这一层单独存在的理由：Notion 的对象里，同一件事有三种说法（标题在 properties 里那个
// type=title 的列上、在 title 数组上、或者压根没有），父的 id 藏在 parent.<type> 这个
// 变着名字的键里。转换集中在这儿，18 个操作就不必各自猜一遍。

import (
	"sort"
	"strings"

	"github.com/sokel-dev/sokel-official-plugins/notion/schema"
)

type notionPage struct {
	ID             string         `json:"id"`
	URL            string         `json:"url"`
	CreatedTime    string         `json:"created_time"`
	LastEditedTime string         `json:"last_edited_time"`
	InTrash        bool           `json:"in_trash"`
	Archived       bool           `json:"archived"`
	Icon           map[string]any `json:"icon"`
	Parent         map[string]any `json:"parent"`
	Properties     map[string]any `json:"properties"`
}

type dataSource struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Title      []any          `json:"title"`
	Properties map[string]any `json:"properties"`
	Parent     map[string]any `json:"parent"`
}

type database struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	Title       []any  `json:"title"`
	DataSources []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"data_sources"`
}

type pageMarkdown struct {
	ID        string `json:"id"`
	Markdown  string `json:"markdown"`
	Truncated bool   `json:"truncated"`
}

type notionUser struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	AvatarURL string `json:"avatar_url"`
	Person    struct {
		Email string `json:"email"`
	} `json:"person"`
}

type notionComment struct {
	ID           string `json:"id"`
	DiscussionID string `json:"discussion_id"`
	RichText     []any  `json:"rich_text"`
	CreatedTime  string `json:"created_time"`
	CreatedBy    struct {
		ID string `json:"id"`
	} `json:"created_by"`
}

type searchResult struct {
	ID             string         `json:"id"`
	Object         string         `json:"object"`
	URL            string         `json:"url"`
	LastEditedTime string         `json:"last_edited_time"`
	Title          []any          `json:"title"`
	Name           string         `json:"name"`
	Parent         map[string]any `json:"parent"`
	Properties     map[string]any `json:"properties"`
}

// —— 转换 ——

// pageTitle：页面标题 = properties 里那个 type=title 的列。
// 库外的普通页面同样如此（它有一个隐藏的 title 列），所以不必分情况。
func pageTitle(props map[string]any) string {
	for _, v := range props {
		m, ok := v.(map[string]any)
		if !ok || str(m["type"]) != "title" {
			continue
		}
		return plainText(m["title"])
	}
	return ""
}

// titleColumn：标题列的列名（写标题时要用它当键——每个库的叫法不同，「名称」「Name」「任务」都有）。
func titleColumn(specs map[string]propType) string {
	for name, s := range specs {
		if s.Type == "title" {
			return name
		}
	}
	return ""
}

// richTitle：title 数组（数据库/数据源的名字）→ 纯文本。
func richTitle(t []any) string { return plainText(t) }

// parentOf：parent 对象 → (类别, id)。id 的键名跟着类别变（page_id / data_source_id / …），
// 所以不能写死一个键去取。
func parentOf(p map[string]any) (kind, id string) {
	if p == nil {
		return "", ""
	}
	t := str(p["type"])
	switch t {
	case "page_id", "database_id", "data_source_id", "block_id":
		return strings.TrimSuffix(t, "_id"), str(p[t])
	case "workspace":
		return "workspace", ""
	}
	return t, ""
}

// iconOf：图标 → emoji 或图片地址。
func iconOf(icon map[string]any) string {
	if icon == nil {
		return ""
	}
	switch str(icon["type"]) {
	case "emoji":
		return str(icon["emoji"])
	case "external":
		if m, ok := icon["external"].(map[string]any); ok {
			return str(m["url"])
		}
	case "file":
		if m, ok := icon["file"].(map[string]any); ok {
			return str(m["url"])
		}
	}
	return ""
}

// toPageItem：页面 → 列表元素（属性已归一化）。
func toPageItem(p notionPage) schema.PageItem {
	kind, pid := parentOf(p.Parent)
	return schema.PageItem{
		ID: p.ID, URL: p.URL, Title: pageTitle(p.Properties), Icon: iconOf(p.Icon),
		ParentType: kind, ParentID: pid,
		CreatedTime: p.CreatedTime, LastEditedTime: p.LastEditedTime,
		InTrash: p.InTrash || p.Archived,
		Props:   normalizeProps(p.Properties),
	}
}

// toUser：成员。
func toUser(u notionUser) schema.User {
	return schema.User{ID: u.ID, Name: u.Name, Type: u.Type, Email: u.Person.Email, AvatarURL: u.AvatarURL}
}

// toComment：评论。
func toComment(c notionComment) schema.Comment {
	return schema.Comment{
		ID: c.ID, DiscussionID: c.DiscussionID, Text: plainText(c.RichText),
		CreatedTime: c.CreatedTime, CreatedByID: c.CreatedBy.ID,
	}
}

// toPropSpecs：表结构 → 契约里的列清单（按列名排序，免得每次调用顺序都不一样）。
func toPropSpecs(raw map[string]any) []schema.PropSpec {
	parsed := parseProps(raw)
	names := make([]string, 0, len(parsed))
	for n := range parsed {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]schema.PropSpec, 0, len(names))
	for _, n := range names {
		p := parsed[n]
		id := ""
		if m, ok := raw[n].(map[string]any); ok {
			id = str(m["id"])
		}
		out = append(out, schema.PropSpec{
			Name: n, ID: id, Type: p.Type, Options: p.Options, Writable: writableTypes[p.Type],
		})
	}
	return out
}
