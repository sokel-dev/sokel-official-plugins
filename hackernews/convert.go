package main

// Mapping both APIs onto schema.Item, and HN's HTML onto plain text.

import (
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-official-plugins/hackernews/schema"
)

func permalink(id string) string { return siteBase + "/item?id=" + id }

func userURL(name string) string {
	if name == "" {
		return ""
	}
	return siteBase + "/user?id=" + name
}

var (
	paraRe = regexp.MustCompile(`(?i)<p>`)
	brRe   = regexp.MustCompile(`(?i)<br\s*/?>`)
	// HN wraps links as <a href="URL" rel="nofollow">shortened text…</a>; the text is truncated, the href is not.
	linkRe = regexp.MustCompile(`(?is)<a\s+[^>]*href="([^"]*)"[^>]*>.*?</a>`)
	tagRe  = regexp.MustCompile(`(?s)<[^>]+>`)
)

// plainText turns HN's comment HTML into text: paragraphs become blank lines, links keep their full address (the
// visible text HN shows is cut with "..."), other tags go, entities are decoded.
func plainText(s string) string {
	if s == "" {
		return ""
	}
	s = paraRe.ReplaceAllString(s, "\n\n")
	s = brRe.ReplaceAllString(s, "\n")
	s = linkRe.ReplaceAllStringFunc(s, func(m string) string {
		if sub := linkRe.FindStringSubmatch(m); len(sub) == 2 {
			return html.UnescapeString(sub[1])
		}
		return m
	})
	s = tagRe.ReplaceAllString(s, "")
	return strings.TrimSpace(html.UnescapeString(s))
}

func idStr(p *int64) string {
	if p == nil || *p == 0 {
		return ""
	}
	return strconv.FormatInt(*p, 10)
}

func intOr0(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func unixRFC3339(t int64) string {
	if t == 0 {
		return ""
	}
	return time.Unix(t, 0).UTC().Format(time.RFC3339)
}

// hitType reads the item type from Algolia's tags (story / comment / poll / job / …).
func hitType(h algoliaHit) string {
	for _, t := range h.Tags {
		switch t {
		case "story", "comment", "poll", "pollopt", "job":
			return t
		}
	}
	return ""
}

func itemFromHit(h algoliaHit) schema.Item {
	it := schema.Item{
		ID:         h.ObjectID,
		Type:       hitType(h),
		Author:     h.Author,
		Title:      h.Title,
		URL:        h.URL,
		Permalink:  permalink(h.ObjectID),
		Score:      intOr0(h.Points),
		Comments:   intOr0(h.NumComments),
		StoryID:    idStr(h.StoryID),
		StoryTitle: h.StoryTitle,
		ParentID:   idStr(h.ParentID),
		CreatedAt:  unixRFC3339(h.CreatedAtI),
	}
	if it.Type == "comment" {
		it.TextHTML = h.CommentText
		it.Text = plainText(h.CommentText)
		it.Score, it.Comments = 0, 0 // comment scores are not public; Algolia reports null
	} else {
		it.TextHTML = h.StoryText
		it.Text = plainText(h.StoryText)
		it.StoryID, it.StoryTitle = h.ObjectID, h.Title
	}
	return it
}

func itemFromFirebase(f fbItem) schema.Item {
	id := strconv.FormatInt(f.ID, 10)
	it := schema.Item{
		ID:        id,
		Type:      f.Type,
		Author:    f.By,
		Title:     f.Title,
		Text:      plainText(f.Text),
		TextHTML:  f.Text,
		URL:       f.URL,
		Permalink: permalink(id),
		Score:     f.Score,
		Comments:  f.Descendants,
		CreatedAt: unixRFC3339(f.Time),
	}
	if f.Type == "comment" {
		it.ParentID = strconv.FormatInt(f.Parent, 10)
		it.Score = 0
	} else {
		it.StoryID, it.StoryTitle = id, f.Title
	}
	return it
}

// flattenTree walks Algolia's /items tree in discussion order (parent before children), depth 1 = top-level comment.
func flattenTree(root treeNode, sinceUnix int64) []schema.Item {
	var out []schema.Item
	storyID := strconv.FormatInt(root.ID, 10)
	var walk func(n treeNode, depth int)
	walk = func(n treeNode, depth int) {
		for _, c := range n.Children {
			// Deleted / dead comments come back with no author and no text: nothing to read or answer.
			if c.Type == "comment" && c.Author != "" && c.CreatedAtI >= sinceUnix {
				id := strconv.FormatInt(c.ID, 10)
				out = append(out, schema.Item{
					ID: id, Type: "comment", Author: c.Author, Text: plainText(c.Text), TextHTML: c.Text,
					Permalink: permalink(id), StoryID: storyID, StoryTitle: root.Title,
					ParentID: idStr(c.ParentID), CreatedAt: unixRFC3339(c.CreatedAtI), Depth: depth,
				})
			}
			walk(c, depth+1)
		}
	}
	walk(root, 1)
	return out
}

var itemIDRe = regexp.MustCompile(`(?:id=)?(\d{1,12})\s*$`)

// parseItemID accepts a bare id or an HN address (…/item?id=123).
func parseItemID(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if m := itemIDRe.FindStringSubmatch(s); len(m) == 2 {
		return m[1], true
	}
	return "", false
}

// parseItemIDs splits a comma / space / newline separated list of ids or addresses.
func parseItemIDs(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '，' || r == ' ' || r == '\n' }) {
		if id, ok := parseItemID(part); ok && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
