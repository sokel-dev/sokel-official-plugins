package main

// Eastmoney search adapter.
//
//	GET https://search-api-web.eastmoney.com/search/jsonp?cb=<callback name>&param=<JSON>
//
// Two things differ from the other adapters:
//   - **The response is JSONP, not JSON**: it's wrapped in `jQueryxxx_123(...)` and the shell
//     must be stripped first. A direct json.Unmarshal would fail with "invalid character 'j'",
//     which doesn't make the real cause obvious.
//   - **No auth, no signature**: no cookie and no sign to compute, the easiest of this bunch.
//
// The endpoint and param shape were learned from RSSHub's eastmoney/search route (we borrowed
// the knowledge, not the code).

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-official-plugins/feed/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

// emSearchAPI: **var, not const** — tests need to point this at a fake upstream.
var emSearchAPI = "https://search-api-web.eastmoney.com/search/jsonp"

// jsonpRe strips the JSONP shell. We supply the callback name ourselves, but still match it
// loosely — the server occasionally wraps the echoed callback name in an extra layer.
var jsonpRe = regexp.MustCompile(`(?s)^[^(]*\((.*)\)[\s;]*$`)

func fetchEastmoney(ctx plugin.Ctx, keyword string) ([]schema.Item, error) {
	kw := strings.TrimSpace(keyword)
	if kw == "" {
		return nil, fmt.Errorf("东方财富搜索要填关键词（如某只票的名称或代码）")
	}
	// param is a whole JSON blob stuffed into the query string, shaped to match their frontend.
	param := map[string]any{
		"uid":           "",
		"keyword":       kw,
		"type":          []string{"cmsArticleWebOld"},
		"client":        "web",
		"clientType":    "web",
		"clientVersion": "curr",
		"param": map[string]any{
			"cmsArticleWebOld": map[string]any{
				"searchScope": "default", "sort": "default",
				"pageIndex": 1, "pageSize": 30, "preTag": "", "postTag": "",
			},
		},
	}
	pj, _ := json.Marshal(param)
	cb := "jQuery" + strconv.FormatInt(time.Now().UnixNano()%1e10, 10) + "_" + strconv.FormatInt(time.Now().UnixMilli(), 10)
	q := url.Values{"cb": {cb}, "param": {string(pj)}}

	raw, err := get(ctx, emSearchAPI+"?"+q.Encode(), "", "https://so.eastmoney.com/")
	if err != nil {
		return nil, err
	}
	body := strings.TrimSpace(string(raw))
	if m := jsonpRe.FindStringSubmatch(body); len(m) == 2 {
		body = m[1]
	}
	var resp struct {
		Result struct {
			CmsArticleWebOld []struct {
				Title     string `json:"title"`
				Content   string `json:"content"`
				URL       string `json:"url"`
				Date      string `json:"date"`
				MediaName string `json:"mediaName"`
				Code      string `json:"code"`
			} `json:"cmsArticleWebOld"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, fmt.Errorf("东方财富应答无法解析（JSONP 壳剥完还是不对，前 160 字：%s）: %w",
			clip([]byte(body), 160), err)
	}
	list := resp.Result.CmsArticleWebOld
	if len(list) == 0 {
		return nil, fmt.Errorf("东方财富没搜到「%s」的内容（换个关键词，或它改了应答形状）", kw)
	}
	items := make([]schema.Item, 0, len(list))
	for _, a := range list {
		// Titles in search results carry highlight tags (<em>), and summaries do too —
		// strip them all.
		title := plainText(a.Title)
		it := schema.Item{
			ID: firstNonEmpty(a.URL, title), Title: title, URL: a.URL,
			Summary: plainText(a.Content), ContentHTML: a.Content,
			Author: a.MediaName, Source: "eastmoney_search:" + kw,
			PublishedAt: emTime(a.Date),
		}
		it.DedupKey = "eastmoney:" + shortHash(it.ID)
		items = append(items, it)
	}
	return items, nil
}

// emTime: what it returns is local time like "2026-08-19 10:30:00" (UTC+8), with no timezone
// marker. Parsing it as UTC would be off by 8 hours across the board — the cursor uses this
// value to judge new vs. old, so an 8-hour offset means a whole batch gets misjudged.
func emTime(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	loc := time.FixedZone("CST", 8*3600)
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	return parseTime(s)
}
