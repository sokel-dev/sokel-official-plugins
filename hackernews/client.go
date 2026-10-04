package main

// HTTP access to the two HN APIs.
//
// Both are public and free. The only configuration is an outbound proxy, because news.ycombinator.com and its APIs
// are often unreachable from mainland China and the symptom is only "timeout".

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

// Base URLs are variables so tests can point them at a recording server.
var (
	algoliaBase  = "https://hn.algolia.com/api/v1"
	firebaseBase = "https://hacker-news.firebaseio.com/v0"
	siteBase     = "https://news.ycombinator.com"
)

var (
	clientsMu sync.Mutex
	clients   = map[string]*http.Client{}
)

func clientFor(proxy string) *http.Client {
	proxy = strings.TrimSpace(proxy)
	clientsMu.Lock()
	defer clientsMu.Unlock()
	if c, ok := clients[proxy]; ok {
		return c
	}
	c := &http.Client{Timeout: 30 * time.Second}
	if proxy != "" {
		if u, err := url.Parse(proxy); err == nil {
			tr := http.DefaultTransport.(*http.Transport).Clone()
			tr.Proxy = http.ProxyURL(u)
			c.Transport = tr
		}
	}
	clients[proxy] = c
	return c
}

// getJSON fetches a URL and decodes the JSON body into out.
func getJSON(ctx context.Context, hc *http.Client, raw string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "SokelBot/1.0 (+https://github.com/sokel-dev)")
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("连不上 Hacker News（%v）。部署环境在国内时，到凭证里填「出站代理」", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("Hacker News 接口回 HTTP %d：%s", resp.StatusCode, strings.TrimSpace(string(body[:min(len(body), 200)])))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("Hacker News 接口返回的不是预期的 JSON：%w", err)
	}
	return nil
}

func credOf(ctx plugin.Ctx) Cred { return sokel.CredentialAs[Cred](ctx) }

// algoliaHit is one result of /search and /search_by_date. Field names are Algolia's.
type algoliaHit struct {
	ObjectID    string   `json:"objectID"`
	Author      string   `json:"author"`
	Title       string   `json:"title"`
	URL         string   `json:"url"`
	StoryText   string   `json:"story_text"`
	CommentText string   `json:"comment_text"`
	Points      *int     `json:"points"`
	NumComments *int     `json:"num_comments"`
	StoryID     *int64   `json:"story_id"`
	StoryTitle  string   `json:"story_title"`
	StoryURL    string   `json:"story_url"`
	ParentID    *int64   `json:"parent_id"`
	CreatedAt   string   `json:"created_at"`
	CreatedAtI  int64    `json:"created_at_i"`
	Tags        []string `json:"_tags"`
}

type algoliaPage struct {
	Hits    []algoliaHit `json:"hits"`
	NbHits  int          `json:"nbHits"`
	Message string       `json:"message"`
}

// searchByDate runs one /search_by_date query, newest first.
func searchByDate(ctx context.Context, hc *http.Client, q url.Values) ([]algoliaHit, error) {
	var page algoliaPage
	if err := getJSON(ctx, hc, algoliaBase+"/search_by_date?"+q.Encode(), &page); err != nil {
		return nil, err
	}
	return page.Hits, nil
}

// fbItem is the official API's item. Absent fields are zero values.
type fbItem struct {
	ID          int64   `json:"id"`
	Type        string  `json:"type"`
	By          string  `json:"by"`
	Time        int64   `json:"time"`
	Title       string  `json:"title"`
	Text        string  `json:"text"`
	URL         string  `json:"url"`
	Score       int     `json:"score"`
	Descendants int     `json:"descendants"`
	Parent      int64   `json:"parent"`
	Kids        []int64 `json:"kids"`
	Deleted     bool    `json:"deleted"`
	Dead        bool    `json:"dead"`
}

// treeNode is Algolia's /items/{id}: the whole thread in one response.
type treeNode struct {
	ID         int64      `json:"id"`
	Type       string     `json:"type"`
	Author     string     `json:"author"`
	Title      string     `json:"title"`
	Text       string     `json:"text"`
	URL        string     `json:"url"`
	Points     *int       `json:"points"`
	StoryID    int64      `json:"story_id"`
	ParentID   *int64     `json:"parent_id"`
	CreatedAt  string     `json:"created_at"`
	CreatedAtI int64      `json:"created_at_i"`
	Children   []treeNode `json:"children"`
}
