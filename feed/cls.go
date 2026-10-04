package main

// 财联社电报适配器。
//
//	GET https://www.cls.cn/api/cache?name=telegraph&<公共参数>&sign=…
//
// **它要签名**：公共参数（appName/os/sv）+ 业务参数按键排序后拼串 → SHA1 → 再 MD5，
// 结果作为 sign 挂上去。少了它一律被拒——这是财联社唯一的门槛，没有 cookie 也没有登录。
//
// 签名算法是从 RSSHub 的 cls/utils.ts 学来的（只借鉴知识，没抄代码）。
// **它会随对方前端版本变**（sv 这个版本号尤其）：哪天全线失败，先看它的 utils.ts 改了没有。

import (
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-official-plugins/feed/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

// clsSite / clsVersion：**是 var 不是 const**——测试要指到假上游；版本号要能跟着对方升。
var (
	clsSite    = "https://www.cls.cn"
	clsVersion = "8.7.9"
)

type clsRoll struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Content  string `json:"content"`
	ShareURL string `json:"shareurl"`
	Ctime    int64  `json:"ctime"` // 秒
	Subjects []struct {
		SubjectName string `json:"subject_name"`
	} `json:"subjects"`
}

func fetchCLS(ctx plugin.Ctx, category string) ([]schema.Item, error) {
	q := url.Values{
		"appName": {"CailianpressWeb"},
		"os":      {"web"},
		"sv":      {clsVersion},
	}
	path := "/api/cache"
	if c := strings.TrimSpace(category); c != "" {
		// 分类电报走另一个接口；留空则是全量电报流。
		path = "/v1/roll/get_roll_list"
		q.Set("category", c)
	} else {
		q.Set("name", "telegraph")
	}
	q.Set("sign", clsSign(q))

	raw, err := get(ctx, clsSite+path+"?"+q.Encode(), "", clsSite+"/")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data struct {
			RollData []clsRoll `json:"roll_data"`
		} `json:"data"`
		Errno int    `json:"errno"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("财联社应答无法解析（前 160 字：%s）: %w", clip(raw, 160), err)
	}
	if len(resp.Data.RollData) == 0 {
		// 签名不对时它回的是空数据而不是报错——所以这里要把「空」说成可能的原因，
		// 否则表现是「一直没有新内容」，而实际上一条都拉不到。
		return nil, fmt.Errorf("财联社没给内容（errno=%d %s）：多半是签名参数变了——"+
			"对照 RSSHub 的 cls/utils.ts 看版本号 sv 是否要升", resp.Errno, resp.Error)
	}
	items := make([]schema.Item, 0, len(resp.Data.RollData))
	for _, r := range resp.Data.RollData {
		items = append(items, clsToItem(r))
	}
	return items, nil
}

// clsSign：按键排序拼串 → SHA1 → MD5。**顺序错了签名就废**，所以用 Encode（它按键排序）。
func clsSign(q url.Values) string {
	s1 := sha1.Sum([]byte(q.Encode()))
	s2 := md5.Sum([]byte(hex.EncodeToString(s1[:])))
	return hex.EncodeToString(s2[:])
}

func clsToItem(r clsRoll) schema.Item {
	id := strconv.FormatInt(r.ID, 10)
	body := firstNonEmpty(r.Content, r.Title)
	it := schema.Item{
		ID: id, Title: truncate(plainText(firstNonEmpty(r.Title, r.Content)), 60),
		URL: r.ShareURL, ContentHTML: body, Summary: plainText(body),
		Source: "cls_telegraph", DedupKey: "cls:" + id,
	}
	if r.Ctime > 0 {
		// 这里是**秒**（雪球那边是毫秒，别混）。
		it.PublishedAt = time.Unix(r.Ctime, 0).UTC().Format(time.RFC3339)
	}
	for _, s := range r.Subjects {
		if n := strings.TrimSpace(s.SubjectName); n != "" {
			it.Tags = append(it.Tags, n)
		}
	}
	return it
}
