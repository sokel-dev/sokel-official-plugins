package main

// Notion's response shapes (only the fields actually used) and their conversion to contract shapes.
//
// Why this layer exists on its own: in Notion's objects, the same thing can be said three ways (the
// title lives in the properties column with type=title, or in a title array, or isn't there at all),
// and a parent's id is tucked under a parent.<type> key whose name changes. Keeping the conversion
// here means the 18 operations don't each have to guess it separately.

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

// —— Conversion ——

// pageTitle: a page's title is the properties column with type=title.
// An ordinary page outside any database works the same way (it has a hidden title column too),
// so there's no need to special-case it.
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

// titleColumn: the name of the title column (use it as the key when writing a title — every
// database calls it something different, e.g. "Name", "名称", "任务").
func titleColumn(specs map[string]propType) string {
	for name, s := range specs {
		if s.Type == "title" {
			return name
		}
	}
	return ""
}

// richTitle converts a title array (a database/data source's name) to plain text.
func richTitle(t []any) string { return plainText(t) }

// parentOf converts a parent object to (kind, id). The id's key name changes with the kind
// (page_id / data_source_id / ...), so it can't be read off a fixed key.
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

// iconOf converts an icon to an emoji or image URL.
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

// toPageItem converts a page to a list item (properties already normalized).
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

// toUser converts a workspace member.
func toUser(u notionUser) schema.User {
	return schema.User{ID: u.ID, Name: u.Name, Type: u.Type, Email: u.Person.Email, AvatarURL: u.AvatarURL}
}

// toComment converts a comment.
func toComment(c notionComment) schema.Comment {
	return schema.Comment{
		ID: c.ID, DiscussionID: c.DiscussionID, Text: plainText(c.RichText),
		CreatedTime: c.CreatedTime, CreatedByID: c.CreatedBy.ID,
	}
}

// toPropSpecs converts a schema to the contract's column list (sorted by column name so the
// order doesn't change from call to call).
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
