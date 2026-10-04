package main

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sokel-dev/sokel-official-plugins/hackernews/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

func opItemGet(ctx plugin.Ctx, in *HnItemGetIn) (*HnItemGetOut, error) {
	id, ok := parseItemID(in.ID)
	if !ok {
		return nil, fmt.Errorf("「%s」不是 HN 条目 id：填数字，或 news.ycombinator.com/item?id=… 这样的地址", in.ID)
	}
	var f fbItem
	if err := getJSON(ctx, clientFor(credOf(ctx).Proxy), firebaseBase+"/item/"+id+".json", &f); err != nil {
		return nil, err
	}
	if f.ID == 0 {
		return nil, fmt.Errorf("HN 上没有 id 为 %s 的条目", id)
	}
	return &HnItemGetOut{Item: itemFromFirebase(f)}, nil
}

var listPaths = map[string]string{
	"top": "topstories", "new": "newstories", "best": "beststories",
	"ask": "askstories", "show": "showstories", "job": "jobstories",
}

func clampLimit(n, def, max int) int {
	if n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

func opList(ctx plugin.Ctx, in *HnListIn) (*HnListOut, error) {
	path, ok := listPaths[in.List]
	if !ok {
		path = "topstories"
	}
	limit := clampLimit(in.Limit, 30, 100)
	hc := clientFor(credOf(ctx).Proxy)
	var ids []int64
	if err := getJSON(ctx, hc, firebaseBase+"/"+path+".json", &ids); err != nil {
		return nil, err
	}
	if len(ids) > limit {
		ids = ids[:limit]
	}
	// The list endpoint only returns ids; items are fetched one by one, a few at a time, keeping list order.
	items := make([]schema.Item, len(ids))
	ok2 := make([]bool, len(ids))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var f fbItem
			if getJSON(ctx, hc, firebaseBase+"/item/"+strconv.FormatInt(id, 10)+".json", &f) == nil && f.ID != 0 && !f.Deleted && !f.Dead {
				items[i], ok2[i] = itemFromFirebase(f), true
			}
		}()
	}
	wg.Wait()
	out := make([]schema.Item, 0, len(items))
	for i, it := range items {
		if ok2[i] {
			out = append(out, it)
		}
	}
	if len(out) == 0 && len(ids) > 0 {
		return nil, fmt.Errorf("榜单取到了 %d 个 id，但条目一个都没读到（网络不稳？）", len(ids))
	}
	return &HnListOut{Items: out, Count: len(out)}, nil
}

func opSearch(ctx plugin.Ctx, in *HnSearchIn) (*HnSearchOut, error) {
	query := strings.TrimSpace(in.Query)
	if query == "" {
		return nil, fmt.Errorf("关键词不能为空")
	}
	q := url.Values{"query": {query}, "hitsPerPage": {strconv.Itoa(clampLimit(in.Limit, 20, 100))}}
	switch in.Kind {
	case "story":
		q.Set("tags", "story")
	case "comment":
		q.Set("tags", "comment")
	default:
		q.Set("tags", "(story,comment)")
	}
	if in.SinceHours > 0 {
		q.Set("numericFilters", "created_at_i>"+strconv.FormatInt(time.Now().Add(-time.Duration(in.SinceHours)*time.Hour).Unix(), 10))
	}
	hits, err := searchByDate(ctx, clientFor(credOf(ctx).Proxy), q)
	if err != nil {
		return nil, err
	}
	items := make([]schema.Item, 0, len(hits))
	for _, h := range hits {
		items = append(items, itemFromHit(h))
	}
	return &HnSearchOut{Items: items, Count: len(items)}, nil
}

func opThreadComments(ctx plugin.Ctx, in *HnThreadCommentsIn) (*HnThreadCommentsOut, error) {
	id, ok := parseItemID(in.ID)
	if !ok {
		return nil, fmt.Errorf("「%s」不是 HN 帖子 id：填数字，或 news.ycombinator.com/item?id=… 这样的地址", in.ID)
	}
	var since int64
	if s := strings.TrimSpace(in.Since); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return nil, fmt.Errorf("「只要这之后的」要填 RFC3339 时间（如 2026-10-04T08:00:00Z）：%v", err)
		}
		since = t.Unix()
	}
	var root treeNode
	if err := getJSON(ctx, clientFor(credOf(ctx).Proxy), algoliaBase+"/items/"+id, &root); err != nil {
		return nil, err
	}
	if root.Type == "comment" {
		return nil, fmt.Errorf("%s 是一条评论，不是帖子：填它所在帖子的 id（%d）", id, root.StoryID)
	}
	items := flattenTree(root, since)
	return &HnThreadCommentsOut{Items: items, Count: len(items), Title: root.Title}, nil
}

func opHealthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	cred := credOf(ctx)
	hc := clientFor(cred.Proxy)
	var ids []int64
	if err := getJSON(ctx, hc, firebaseBase+"/topstories.json", &ids); err != nil {
		return &HealthCheckOut{OK: false, Message: "官方接口：" + err.Error()}, nil
	}
	if _, err := searchByDate(ctx, hc, url.Values{"tags": {"story"}, "hitsPerPage": {"1"}}); err != nil {
		return &HealthCheckOut{OK: false, Message: "搜索接口：" + err.Error()}, nil
	}
	if u := strings.TrimSpace(cred.WatchUser); u != "" {
		var user struct {
			ID string `json:"id"`
		}
		if err := getJSON(ctx, hc, firebaseBase+"/user/"+url.PathEscape(u)+".json", &user); err != nil {
			return &HealthCheckOut{OK: false, Message: err.Error()}, nil
		}
		if user.ID == "" {
			return &HealthCheckOut{OK: false, Message: fmt.Sprintf("HN 上没有用户「%s」（用户名区分大小写）", u)}, nil
		}
		return &HealthCheckOut{OK: true, Message: "两个接口都能连上，监听用户 " + user.ID + " 存在"}, nil
	}
	return &HealthCheckOut{OK: true, Message: "两个接口都能连上"}, nil
}
