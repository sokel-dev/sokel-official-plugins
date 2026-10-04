package main

// 操作层：按来源分发 → 过游标 → 交出统一形状。

import (
	"fmt"
	"strings"

	"github.com/sokel-dev/sokel-official-plugins/feed/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

func opFetch(ctx plugin.Ctx, in *FeedFetchIn) (*FeedFetchOut, error) {
	src := strings.TrimSpace(in.Source)
	if src == "" {
		src = "rss"
	}
	var (
		items []schema.Item
		err   error
	)
	switch src {
	case "rss":
		items, err = fetchRSS(ctx, in.Target)
	case "xueqiu_user", "xueqiu_hots", "xueqiu_livenews":
		items, err = fetchXueqiu(ctx, src, in.Target, in.SkipReposts)
	case "cls_telegraph":
		items, err = fetchCLS(ctx, in.Target)
	case "eastmoney_search":
		items, err = fetchEastmoney(ctx, in.Target)
	case "jin10":
		items, err = fetchJin10(ctx, in.Target)
	default:
		return nil, fmt.Errorf("不认识的来源 %q（有 rss / xueqiu_user / xueqiu_hots / xueqiu_livenews / cls_telegraph / eastmoney_search / jin10）", src)
	}
	if err != nil {
		return nil, err
	}

	cur := parseCursor(in.Cursor)
	firstRun := strings.TrimSpace(in.Cursor) == ""
	max := in.MaxItems
	if max <= 0 {
		max = 50
	}
	kept, next, hasMore := filterNew(items, cur, max, firstRun)
	return &FeedFetchOut{
		Items: kept, NextCursor: next.dump(), HasMore: hasMore, Count: len(kept),
	}, nil
}

func opHealthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	// 本插件多数来源不需要凭证，这里验的是「出站通不通 + 雪球令牌拿不拿得到」。
	if _, err := xueqiuCookie(ctx); err != nil {
		return &HealthCheckOut{OK: false, Message: err.Error()}, nil
	}
	return &HealthCheckOut{OK: true, Message: "出站正常，雪球匿名令牌可取"}, nil
}
