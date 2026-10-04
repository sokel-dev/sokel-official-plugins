package main

// 东方财富搜索适配器。
//
//	GET https://search-api-web.eastmoney.com/search/jsonp?cb=<回调名>&param=<JSON>
//
// 两处与别家不同：
//   - **应答是 JSONP 不是 JSON**：外面裹着 `jQueryxxx_123(...)`，得先把壳剥掉。
//     直接 json.Unmarshal 会得到「invalid character 'j'」，看不出是这个原因。
//   - **无鉴权无签名**：不用 cookie 也不用算 sign，是这几家里最省心的。
//
// 接口与参数形状从 RSSHub 的 eastmoney/search route 学来（只借鉴知识，没抄代码）。

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

// emSearchAPI：**是 var 不是 const**——测试要指到假上游。
var emSearchAPI = "https://search-api-web.eastmoney.com/search/jsonp"

// jsonpRe：剥 JSONP 的壳。回调名是我们自己给的，但仍按通配匹配——
// 对方偶尔会把回调名原样回显之外再包一层。
var jsonpRe = regexp.MustCompile(`(?s)^[^(]*\((.*)\)[\s;]*$`)

func fetchEastmoney(ctx plugin.Ctx, keyword string) ([]schema.Item, error) {
	kw := strings.TrimSpace(keyword)
	if kw == "" {
		return nil, fmt.Errorf("东方财富搜索要填关键词（如某只票的名称或代码）")
	}
	// param 是一整段 JSON 塞进查询串里，形状照它前端来。
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
		// 搜索结果里的标题带高亮标签（<em>），摘要同理——一律去掉。
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

// emTime：它给的是「2026-08-19 10:30:00」这种本地时间（东八区），没有时区标注。
// 按 UTC 解会整体差 8 小时——游标据此判断新旧，差 8 小时就意味着一整批被误判。
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
