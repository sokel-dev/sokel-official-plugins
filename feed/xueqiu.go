package main

// Xueqiu adapter. **Uses its read API, not page scraping** — learned from RSSHub's route:
//
//	GET https://api.xueqiu.com/v4/statuses/user_timeline.json?user_id=&page=
//	GET https://api.xueqiu.com/statuses/hot/listV2.json?since_id=-1&max_id=-1&size=
//	GET https://api.xueqiu.com/statuses/livenews/list.json?count=
//
// **These three paths were found by trial, don't guess from the name**: `/v4/statuses/hots.json`
// looks the most like "hot posts" but 404s; and the web-facing
// `xueqiu.com/statuses/hot/listV2.json` hits Alibaba Cloud's WAF and returns an HTML page —
// only the same-named path under the api domain actually works.
//
// The key is that **it only needs an anonymous token** (obtained just by hitting the homepage,
// see client.go), not a user login — so fetching data and posting are two completely separate
// paths: the credential used for fetching can be empty.

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-official-plugins/feed/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

type xqStatus struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Text        string `json:"text"`
	Description string `json:"description"`
	Target      string `json:"target"`
	CreatedAt   int64  `json:"created_at"` // milliseconds
	User        struct {
		ScreenName string `json:"screen_name"`
	} `json:"user"`
	RetweetedStatus *xqStatus `json:"retweeted_status"`
	// OriginalStatus: the hot-posts list wraps a shell whose own id is the "hot-list entry
	// id," not the post id. Without unwrapping it, the id, time, and body all come out
	// empty or wrong.
	OriginalStatus *xqStatus `json:"original_status"`
}

func fetchXueqiu(ctx plugin.Ctx, kind, target string, skipReposts bool) ([]schema.Item, error) {
	cookie, err := xueqiuCookie(ctx)
	if err != nil {
		return nil, err
	}
	var uri, source string
	switch kind {
	case "xueqiu_user":
		uid := strings.TrimSpace(target)
		if i := strings.LastIndex(uid, "/"); i >= 0 {
			uid = uid[i+1:] // allow pasting the profile URL directly
		}
		if uid == "" {
			return nil, fmt.Errorf("雪球用户来源要填 uid（主页地址 xueqiu.com/u/<这一串>）")
		}
		uri = xueqiuAPI + "/v4/statuses/user_timeline.json?user_id=" + uid
		source = "xueqiu_user:" + uid
	case "xueqiu_livenews":
		uri = xueqiuAPI + "/statuses/livenews/list.json?count=30"
		source = "xueqiu_livenews"
	default:
		uri = xueqiuAPI + "/statuses/hot/listV2.json?since_id=-1&max_id=-1&size=30"
		source = "xueqiu_hots"
	}

	raw, err := get(ctx, uri, cookie, xueqiuSite+"/")
	if err != nil {
		return nil, err
	}
	// The two endpoints wrap results differently: user_timeline is {statuses:[…]}, hots is
	// either an array or {list:[…]}.
	var wrap struct {
		Statuses []xqStatus `json:"statuses"`
		List     []xqStatus `json:"list"`
		Items    []xqStatus `json:"items"`
	}
	_ = json.Unmarshal(raw, &wrap)
	list := wrap.Statuses
	if len(list) == 0 {
		list = wrap.List
	}
	if len(list) == 0 {
		list = wrap.Items
	}
	if len(list) == 0 {
		var bare []xqStatus
		if json.Unmarshal(raw, &bare) == nil {
			list = bare
		}
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("雪球没给内容（前 160 字：%s）——令牌可能被风控挡了，"+
			"可在凭证里粘一条真 Cookie", clip(raw, 160))
	}

	items := make([]schema.Item, 0, len(list))
	for _, s := range list {
		if s.OriginalStatus != nil {
			s = *s.OriginalStatus // hot-posts list: unwrap the outer shell
		}
		if skipReposts && s.RetweetedStatus != nil {
			continue
		}
		items = append(items, xqToItem(s, source))
	}
	return items, nil
}

func xqToItem(s xqStatus, source string) schema.Item {
	body := firstNonEmpty(s.Text, s.Description)
	id := strconv.FormatInt(s.ID, 10)
	link := ""
	if t := strings.TrimSpace(s.Target); t != "" {
		// target shows up in two shapes: the posts endpoint gives a relative path
		// /uid/statusid, while the flash-news endpoint gives a full http:// absolute URL.
		// Distinguish before concatenating, otherwise you get an unopenable link like
		// https://xueqiu.comhttp://….
		switch {
		case strings.HasPrefix(t, "http://"):
			link = "https://" + strings.TrimPrefix(t, "http://")
		case strings.HasPrefix(t, "https://"):
			link = t
		default:
			link = xueqiuSite + t
		}
	}
	it := schema.Item{
		ID: id, Title: strings.TrimSpace(s.Title), URL: link,
		ContentHTML: body, Summary: plainText(body),
		Author: s.User.ScreenName, Source: source,
		DedupKey: "xueqiu:" + id,
	}
	if s.CreatedAt > 0 {
		// Xueqiu gives a millisecond timestamp. Treating it as seconds would turn a 2026
		// post into the year 56000 — that would jump the cursor into the future in one
		// shot, and no new content would ever arrive after that.
		it.PublishedAt = time.UnixMilli(s.CreatedAt).UTC().Format(time.RFC3339)
	}
	if s.Title == "" {
		it.Title = truncate(it.Summary, 40)
	}
	it.Images = imagesIn(body)
	return it
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
