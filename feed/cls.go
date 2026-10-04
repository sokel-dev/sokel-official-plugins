package main

// Cailianpress (CLS) telegraph adapter.
//
//	GET https://www.cls.cn/api/cache?name=telegraph&<common params>&sign=…
//
// **It requires a signature**: common params (appName/os/sv) plus business params are sorted
// by key, concatenated, run through SHA1, then MD5, and the result is attached as sign. Omit
// it and every request is rejected — this is CLS's only gate; there's no cookie or login.
//
// The signing algorithm was learned from RSSHub's cls/utils.ts (we borrowed the knowledge,
// not the code). **It changes along with their frontend version** (especially the sv version
// number): if everything starts failing one day, check whether their utils.ts changed first.

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

// clsSite / clsVersion: **var, not const** — tests need to point this at a fake upstream, and
// the version number needs to be bumpable to track theirs.
var (
	clsSite    = "https://www.cls.cn"
	clsVersion = "8.7.9"
)

type clsRoll struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Content  string `json:"content"`
	ShareURL string `json:"shareurl"`
	Ctime    int64  `json:"ctime"` // seconds
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
		// Categorized telegraphs use a different endpoint; empty means the full telegraph stream.
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
		// When the signature is wrong it responds with empty data instead of an error, so
		// we need to call out "empty" as a possible cause here — otherwise it looks like
		// "no new content ever," when in fact nothing is being fetched at all.
		return nil, fmt.Errorf("财联社没给内容（errno=%d %s）：多半是签名参数变了——"+
			"对照 RSSHub 的 cls/utils.ts 看版本号 sv 是否要升", resp.Errno, resp.Error)
	}
	items := make([]schema.Item, 0, len(resp.Data.RollData))
	for _, r := range resp.Data.RollData {
		items = append(items, clsToItem(r))
	}
	return items, nil
}

// clsSign concatenates params sorted by key -> SHA1 -> MD5. **Wrong order ruins the
// signature**, so we use Encode (which sorts by key).
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
		// This is in **seconds** (Xueqiu uses milliseconds — don't mix them up).
		it.PublishedAt = time.Unix(r.Ctime, 0).UTC().Format(time.RFC3339)
	}
	for _, s := range r.Subjects {
		if n := strings.TrimSpace(s.SubjectName); n != "" {
			it.Tags = append(it.Tags, n)
		}
	}
	return it
}
