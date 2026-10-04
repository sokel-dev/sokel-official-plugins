package main

// 读：搜索 / 时间线 / 提及 / 列表 / 按 id 取 / 查用户。
//
// 四个列表型操作共用 fetchPosts：**对外是 since_id 游标**（存数据表，跨运行接着来），
// 对内才用 X 的 next_token 翻页（它一小时就失效，且只在一次搜索会话内有效，
// 存进数据表下次用必然报错——这正是照抄上游分页会踩的坑）。

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/sokel-dev/sokel-official-plugins/x/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

const maxPageSize = 100

// —— 授权账号自己是谁 ——

var (
	meMu    sync.Mutex
	meCache = map[string]rawUser{} // access_token → 账号
)

// me：授权账号。提及、发推链接、私信都要它，而它不会变——按 token 缓存，
// 每次操作都去问一次是纯浪费（还每次都计费：查用户约 $0.01/次）。
func me(ctx plugin.Ctx) (rawUser, error) {
	cred := sokel.CredentialAs[Cred](ctx)
	tok, err := accessToken(cred)
	if err != nil {
		return rawUser{}, err
	}
	meMu.Lock()
	u, ok := meCache[tok]
	meMu.Unlock()
	if ok {
		return u, nil
	}
	q := url.Values{"user.fields": {userFieldList}}
	var resp struct {
		Data rawUser `json:"data"`
	}
	if err := callAPI(ctx, reqOpts{method: http.MethodGet, path: "/users/me", query: q}, &resp); err != nil {
		return rawUser{}, fmt.Errorf("查授权账号失败（是不是没勾 users.read 作用域？）: %w", err)
	}
	meMu.Lock()
	meCache[tok] = resp.Data
	meMu.Unlock()
	return resp.Data, nil
}

// meUsername：只为拼链接用，查不到就算了——发推成功却因为拼不出链接而报错是本末倒置。
func meUsername(ctx plugin.Ctx) string {
	u, err := me(ctx)
	if err != nil {
		return ""
	}
	return u.Username
}

// —— 列表型读的公共实现 ——

type pageReq struct {
	path   string
	query  url.Values // 端点自己的参数（query / exclude / …）
	cursor string     // since_id
	max    int
}

type pageResult struct {
	items      []schema.Post
	nextCursor string
	hasMore    bool
}

// fetchPosts：翻到攒够 max 条为止，返回时间正序的列表与新游标。
//
// 三条判断：
//   - **正序返回**：X 给的是倒序（新的在前），而下游处理顺序应当与事件发生顺序一致。
//   - **游标取本批最新的 id**：用 meta.newest_id；它在跨页时也是全局最新的那条。
//   - **空结果不动游标**：原样返回传入的值，否则一轮没有新推文就把进度冲掉了。
func fetchPosts(ctx plugin.Ctx, r pageReq) (pageResult, error) {
	max := r.max
	if max <= 0 {
		max = 50
	}
	q := readQuery()
	for k, vs := range r.query {
		q[k] = vs
	}
	if c := strings.TrimSpace(r.cursor); c != "" {
		q.Set("since_id", c)
	}

	var all []schema.Post
	newest := strings.TrimSpace(r.cursor)
	token := ""
	hasMore := false
	for {
		want := max - len(all)
		if want <= 0 {
			hasMore = true // 还没拉完就到量了：下一轮接着来
			break
		}
		q.Set("max_results", strconv.Itoa(pageSize(want, r.path)))
		if token != "" {
			q.Set("pagination_token", token)
		}
		var env listEnvelope
		if err := callRead(ctx, reqOpts{method: http.MethodGet, path: r.path, query: q}, &env); err != nil {
			return pageResult{}, err
		}
		all = append(all, toPosts(env)...)
		if env.Meta.NewestID != "" && env.Meta.NewestID > newest {
			newest = env.Meta.NewestID
		}
		token = env.Meta.NextToken
		if token == "" || len(env.Data) == 0 {
			break
		}
	}
	// 倒序拉回来的，按 id 正序给出去（推文 id 单调递增，比按时间字符串排稳）。
	sort.Slice(all, func(i, j int) bool { return idLess(all[i].ID, all[j].ID) })
	return pageResult{items: all, nextCursor: newest, hasMore: hasMore}, nil
}

// pageSize：每页要几条。X 各端点的下限不一样（搜索最低 10、提及最低 5），
// 低于下限直接 400——「只想要 3 条」这种再正常不过的配置会莫名其妙失败。
func pageSize(want int, path string) int {
	min := 5
	if strings.Contains(path, "/search/") {
		min = 10
	}
	if want < min {
		return min
	}
	if want > maxPageSize {
		return maxPageSize
	}
	return want
}

// idLess：推文 id 是雪花号，位数相同则字典序即数值序；位数不同时短的更小。
func idLess(a, b string) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}

func pageOut(res pageResult) ([]schema.Post, string, bool, int) {
	return res.items, res.nextCursor, res.hasMore, len(res.items)
}

// —— 各操作 ——

func opSearch(ctx plugin.Ctx, in *XSearchIn) (*XSearchOut, error) {
	query := strings.TrimSpace(in.Query)
	if query == "" {
		return nil, fmt.Errorf("查询式是空的")
	}
	q := url.Values{"query": {query}}
	if in.Sort != "" {
		q.Set("sort_order", in.Sort)
	}
	res, err := fetchPosts(ctx, pageReq{path: "/tweets/search/recent", query: q, cursor: in.Cursor, max: in.MaxItems})
	if err != nil {
		return nil, err
	}
	items, cur, more, n := pageOut(res)
	return &XSearchOut{Items: items, NextCursor: cur, HasMore: more, Count: n}, nil
}

func opUserTimeline(ctx plugin.Ctx, in *XUserTimelineIn) (*XUserTimelineOut, error) {
	uid, err := resolveUserID(ctx, in.UserID, in.Username)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	var excl []string
	if in.ExcludeReposts {
		excl = append(excl, "retweets")
	}
	if in.ExcludeReplies {
		excl = append(excl, "replies")
	}
	if len(excl) > 0 {
		q.Set("exclude", strings.Join(excl, ","))
	}
	res, err := fetchPosts(ctx, pageReq{path: "/users/" + uid + "/tweets", query: q, cursor: in.Cursor, max: in.MaxItems})
	if err != nil {
		return nil, err
	}
	items, cur, more, n := pageOut(res)
	return &XUserTimelineOut{Items: items, NextCursor: cur, HasMore: more, Count: n}, nil
}

func opMentions(ctx plugin.Ctx, in *XMentionsIn) (*XMentionsOut, error) {
	u, err := me(ctx)
	if err != nil {
		return nil, err
	}
	res, err := fetchPosts(ctx, pageReq{path: "/users/" + u.ID + "/mentions", cursor: in.Cursor, max: in.MaxItems})
	if err != nil {
		return nil, err
	}
	items, cur, more, n := pageOut(res)
	return &XMentionsOut{Items: items, NextCursor: cur, HasMore: more, Count: n}, nil
}

func opListPosts(ctx plugin.Ctx, in *XListPostsIn) (*XListPostsOut, error) {
	id := strings.TrimSpace(in.ListID)
	if id == "" {
		return nil, fmt.Errorf("列表 id 是空的（列表页地址 x.com/i/lists/<这一串>）")
	}
	res, err := fetchPosts(ctx, pageReq{path: "/lists/" + id + "/tweets", cursor: in.Cursor, max: in.MaxItems})
	if err != nil {
		return nil, err
	}
	items, cur, more, n := pageOut(res)
	return &XListPostsOut{Items: items, NextCursor: cur, HasMore: more, Count: n}, nil
}

func opPostGet(ctx plugin.Ctx, in *XPostGetIn) (*XPostGetOut, error) {
	ids := nonEmpty(in.PostIDs)
	if len(ids) == 0 {
		return nil, fmt.Errorf("一个推文 id 都没给")
	}
	if len(ids) > 100 {
		return nil, fmt.Errorf("一次最多取 100 条，给了 %d 条", len(ids))
	}
	q := readQuery()
	q.Set("ids", strings.Join(ids, ","))
	var env listEnvelope
	if err := callRead(ctx, reqOpts{method: http.MethodGet, path: "/tweets", query: q}, &env); err != nil {
		return nil, err
	}
	items := toPosts(env)
	return &XPostGetOut{Items: items, Count: len(items)}, nil
}

func opUserGet(ctx plugin.Ctx, in *XUserGetIn) (*XUserGetOut, error) {
	username := strings.TrimPrefix(strings.TrimSpace(in.Username), "@")
	uid := strings.TrimSpace(in.UserID)

	path := "/users/me"
	switch {
	case uid != "":
		path = "/users/" + uid
	case username != "":
		path = "/users/by/username/" + username
	}
	q := url.Values{"user.fields": {userFieldList}}
	var resp struct {
		Data rawUser `json:"data"`
	}
	err := callAPI(ctx, reqOpts{method: http.MethodGet, path: path, query: q}, &resp)
	if err != nil {
		// 账号不存在/已封禁是 X 的 400/404，对「查一下有没有这个人」而言这是答案，不是故障。
		var ae *apiError
		if errors.As(err, &ae) && (ae.Status == http.StatusNotFound || ae.Status == http.StatusBadRequest) {
			return &XUserGetOut{Found: false}, nil
		}
		return nil, err
	}
	if resp.Data.ID == "" {
		return &XUserGetOut{Found: false}, nil
	}
	return &XUserGetOut{User: toUser(resp.Data), Found: true}, nil
}

// resolveUserID：用户 id 优先，其次按用户名查一次。
//
// 两个都空**不默认成授权账号自己**：那会让「漏绑了一个变量」变成「悄悄拉了自己的时间线」，
// 而它看起来一切正常。
func resolveUserID(ctx plugin.Ctx, userID, username string) (string, error) {
	if id := strings.TrimSpace(userID); id != "" {
		return id, nil
	}
	name := strings.TrimPrefix(strings.TrimSpace(username), "@")
	if name == "" {
		return "", fmt.Errorf("用户 id 与用户名都是空的")
	}
	var resp struct {
		Data rawUser `json:"data"`
	}
	if err := callAPI(ctx, reqOpts{method: http.MethodGet, path: "/users/by/username/" + name}, &resp); err != nil {
		return "", fmt.Errorf("按用户名 @%s 查 id 失败: %w", name, err)
	}
	if resp.Data.ID == "" {
		return "", fmt.Errorf("没有这个账号：@%s", name)
	}
	return resp.Data.ID, nil
}

// opHealthCheck：这条凭证还活着吗。
//
// **不可用要返回 ok=false 而不是 error**：平台拿它的结论去写凭证状态，
// 报错的话上层只知道「调用失败」，分不清是凭证坏了还是网络抖了。
func opHealthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	u, err := me(ctx)
	if err != nil {
		return &HealthCheckOut{OK: false, Message: err.Error()}, nil
	}
	if u.ID == "" {
		return &HealthCheckOut{OK: false, Message: "X 没返回账号信息，多半是授权已被撤销"}, nil
	}
	return &HealthCheckOut{OK: true, Username: u.Username, Message: "@" + u.Username}, nil
}
