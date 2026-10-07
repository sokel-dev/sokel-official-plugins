package main

// Reddit's public Atom feeds (`.rss`): parsing and the plain-text rendering of an entry's HTML.

import (
	"encoding/xml"
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"
)

type atomFeed struct {
	XMLName xml.Name    `xml:"feed"` // anything else (an HTML block page) is not a feed
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	ID        string `xml:"id"`
	Title     string `xml:"title"`
	Updated   string `xml:"updated"`
	Published string `xml:"published"`
	Author    struct {
		Name string `xml:"name"`
		URI  string `xml:"uri"`
	} `xml:"author"`
	Category struct {
		Term  string `xml:"term,attr"`
		Label string `xml:"label,attr"`
	} `xml:"category"`
	Content string `xml:"content"`
	Link    struct {
		Href string `xml:"href,attr"`
	} `xml:"link"`
}

// item is one feed entry, the fields the plugin uses.
type item struct {
	Name      string // t3_… / t1_…
	Kind      string // post / comment
	Subreddit string
	Author    string
	AuthorURL string
	Title     string
	HTML      string
	URL       string
	PostID    string
	Created   int64
}

var (
	// /r/<sub>/comments/<post id>/<slug>/<comment id>/
	permalinkRe = regexp.MustCompile(`/comments/([a-z0-9]+)/`)
	paraRe      = regexp.MustCompile(`(?i)<p>`)
	brRe        = regexp.MustCompile(`(?i)<br\s*/?>`)
	linkRe      = regexp.MustCompile(`(?is)<a\s+[^>]*href="([^"]*)"[^>]*>.*?</a>`)
	tagRe       = regexp.MustCompile(`(?s)<[^>]+>`)
)

func parseFeed(b []byte) ([]item, error) {
	var f atomFeed
	if err := xml.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("Reddit 返回的不是 Atom feed：%w", err)
	}
	out := make([]item, 0, len(f.Entries))
	for _, e := range f.Entries {
		it := item{Name: e.ID, Subreddit: e.Category.Term, Title: e.Title, HTML: e.Content, URL: e.Link.Href,
			Author: strings.TrimPrefix(e.Author.Name, "/u/"), AuthorURL: e.Author.URI}
		switch {
		case strings.HasPrefix(e.ID, "t3_"):
			it.Kind = "post"
		case strings.HasPrefix(e.ID, "t1_"):
			it.Kind = "comment"
		default:
			continue
		}
		if m := permalinkRe.FindStringSubmatch(e.Link.Href); len(m) == 2 {
			it.PostID = m[1]
		}
		when := e.Published
		if when == "" {
			when = e.Updated
		}
		if t, err := time.Parse(time.RFC3339, when); err == nil {
			it.Created = t.Unix()
		}
		out = append(out, it)
	}
	return out, nil
}

// plainText renders Reddit's HTML as text: paragraphs and line breaks kept, links replaced by their address.
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

func unixRFC3339(t int64) string {
	if t == 0 {
		return ""
	}
	return time.Unix(t, 0).UTC().Format(time.RFC3339)
}
