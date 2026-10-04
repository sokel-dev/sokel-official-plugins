package main

// 雪球适配器。**走它的读接口，不爬网页**——这一条是从 RSSHub 的 route 里学到的：
//
//	GET https://api.xueqiu.com/v4/statuses/user_timeline.json?user_id=&page=
//	GET https://api.xueqiu.com/statuses/hot/listV2.json?since_id=-1&max_id=-1&size=
//	GET https://api.xueqiu.com/statuses/livenews/list.json?count=
//
// **这三条路径是实测出来的，别按名字猜**：`/v4/statuses/hots.json` 看着最像热帖，
// 实际 404；而网页端那个 `xueqiu.com/statuses/hot/listV2.json` 会撞上阿里云 WAF
// 回一页 HTML——只有 api 域名下的同名路径是通的。
//
// 关键在于**它只要一个匿名令牌**（访问首页即得，见 client.go），不需要用户登录——
// 所以取数与发帖是两条完全独立的路：取数的凭证可以是空的。

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
	CreatedAt   int64  `json:"created_at"` // 毫秒
	User        struct {
		ScreenName string `json:"screen_name"`
	} `json:"user"`
	RetweetedStatus *xqStatus `json:"retweeted_status"`
	// OriginalStatus：热帖列表是一层壳，壳自己的 id 是「热帖榜条目 id」而不是帖子 id。
	// 不拆壳的话拿到的 id、时间、正文全是空的或错的。
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
			uid = uid[i+1:] // 允许直接粘主页地址
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
	// 两个接口的外壳不同：user_timeline 是 {statuses:[…]}，hots 是数组或 {list:[…]}。
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
			s = *s.OriginalStatus // 热帖榜：拆掉外面那层壳
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
		// target 两种形状都出现过：帖子接口给相对路径 /uid/statusid，
		// 快讯接口给的是整条 http:// 绝对地址。拼前先分清，否则会拼出
		// https://xueqiu.comhttp://… 这种打不开的链接。
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
		// 雪球给的是毫秒时间戳。当秒用会把 2026 年的帖子算成 56000 年——
		// 那会让游标一次跳到未来，此后再也收不到新内容。
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
