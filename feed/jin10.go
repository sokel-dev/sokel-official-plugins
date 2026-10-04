package main

// 金十数据快讯适配器。
//
//	GET https://flash-api.jin10.com/get_flash_list?channel=-8200&vip=1
//	headers: x-app-id / x-version
//
// **两个固定请求头是全部门槛**：少了直接被拒。没有 cookie、没有签名。
// 接口与请求头从 RSSHub 的 jin10 route 学来（只借鉴知识，没抄代码）——
// 这类固定值会随对方前端升级而变，所以抽成了 var，错的时候错误信息直接指向它。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-official-plugins/feed/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

var (
	jin10API     = "https://flash-api.jin10.com/get_flash_list"
	jin10AppID   = "bVBF4FyRTn5NJF5n"
	jin10Version = "1.0.0"
)

type jin10Flash struct {
	ID        string `json:"id"`
	Time      string `json:"time"`
	Important int    `json:"important"`
	Data      struct {
		Content string `json:"content"`
		Title   string `json:"title"`
		Link    string `json:"link"`
		Pic     string `json:"pic"`
	} `json:"data"`
}

func fetchJin10(ctx plugin.Ctx, channel string) ([]schema.Item, error) {
	ch := strings.TrimSpace(channel)
	if ch == "" {
		ch = "-8200" // 全部快讯
	}
	uri := jin10API + "?channel=" + ch + "&vip=1"

	cred := credOf(ctx)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", uaOf(cred))
	req.Header.Set("Referer", "https://www.jin10.com/")
	// 这两个头是门槛，少一个就被拒。
	req.Header.Set("x-app-id", jin10AppID)
	req.Header.Set("x-version", jin10Version)

	resp, err := clientFor(cred.Proxy).Do(req)
	if err != nil {
		return nil, fmt.Errorf("连接金十失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := readAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("金十返回 HTTP %d：多半是 x-app-id / x-version 变了"+
			"（对照 RSSHub 的 jin10 route 看一眼）：%s", resp.StatusCode, clip(raw, 160))
	}
	var wrap struct {
		Data []jin10Flash `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, fmt.Errorf("金十应答无法解析（前 160 字：%s）: %w", clip(raw, 160), err)
	}
	if len(wrap.Data) == 0 {
		return nil, fmt.Errorf("金十没给内容：可能是频道号 %q 不对，或请求头失效", ch)
	}
	items := make([]schema.Item, 0, len(wrap.Data))
	for _, f := range wrap.Data {
		items = append(items, jin10ToItem(f, ch))
	}
	return items, nil
}

func jin10ToItem(f jin10Flash, channel string) schema.Item {
	body := firstNonEmpty(f.Data.Content, f.Data.Title)
	it := schema.Item{
		ID: f.ID, Title: truncate(plainText(firstNonEmpty(f.Data.Title, body)), 60),
		URL: f.Data.Link, ContentHTML: body, Summary: plainText(body),
		Source: "jin10:" + channel, DedupKey: "jin10:" + f.ID,
		PublishedAt: jin10Time(f.Time),
	}
	if f.Important == 1 {
		it.Tags = append(it.Tags, "重要")
	}
	if p := strings.TrimSpace(f.Data.Pic); p != "" {
		it.Images = append(it.Images, p)
	}
	return it
}

// jin10Time：它给的是「2026-08-19 10:30:00」这种**东八区本地时间**，没有时区标注。
// 与东财同一个坑：按 UTC 解会整体差 8 小时，而游标据此判断新旧。
func jin10Time(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	loc := time.FixedZone("CST", 8*3600)
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02 15:04"} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	return parseTime(s)
}
