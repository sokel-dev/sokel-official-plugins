package main

// Reading: search / timeline / mentions / lists / fetch by id / user lookup.
//
// The four list-style operations share fetchPosts: **externally it's a since_id cursor** (stored
// in a data table, continuing across runs), and only internally does it use X's next_token for
// pagination (which expires in an hour and is only valid within a single search session — storing
// it in a data table is guaranteed to error the next time it's used, exactly the pitfall of copying
// the upstream's own pagination).

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

// —— Who the authorized account is ——

var (
	meMu    sync.Mutex
	meCache = map[string]rawUser{} // access_token -> account
)

// me returns the authorized account. Mentions, post links, and DMs all need it, and it doesn't
// change — cached per token, since asking on every operation would be pure waste (and billed every
// time: looking up a user costs roughly $0.01/call).
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

// meUsername is only used to build links; if the lookup fails, just give up on it — failing a
// successful post because a link couldn't be assembled would be putting the cart before the horse.
func meUsername(ctx plugin.Ctx) string {
	u, err := me(ctx)
	if err != nil {
		return ""
	}
	return u.Username
}

// —— Shared implementation for list-style reads ——

type pageReq struct {
	path   string
	query  url.Values // The endpoint's own parameters (query / exclude / ...)
	cursor string     // since_id
	max    int
}

type pageResult struct {
	items      []schema.Post
	nextCursor string
	hasMore    bool
}

// fetchPosts pages through results until max items are collected, returning a chronologically
// ascending list and the new cursor.
//
// Three decisions:
//   - **Returned in ascending order**: X gives results in descending order (newest first), but
//     downstream processing order should match the order events actually happened.
//   - **The cursor takes the newest id across the whole batch**: using meta.newest_id, which stays
//     the globally newest one across pages too.
//   - **An empty result leaves the cursor untouched**: it's returned as given, otherwise a round
//     with no new tweets would wipe out progress.
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
			hasMore = true // Hit the limit before pagination was exhausted: continue next round
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
	// Fetched in descending order; given out sorted ascending by id (tweet ids are monotonically
	// increasing, which sorts more reliably than the created_at string).
	sort.Slice(all, func(i, j int) bool { return idLess(all[i].ID, all[j].ID) })
	return pageResult{items: all, nextCursor: newest, hasMore: hasMore}, nil
}

// pageSize decides how many per page. X's endpoints have different minimums (search requires at
// least 10, mentions at least 5), and going below the minimum is a flat 400 — an entirely
// reasonable config like "I just want 3" would fail for no apparent reason.
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

// idLess compares tweet ids: they're Snowflake ids, so equal-length strings sort lexically the same
// as numerically; a shorter string is always smaller.
func idLess(a, b string) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}

func pageOut(res pageResult) ([]schema.Post, string, bool, int) {
	return res.items, res.nextCursor, res.hasMore, len(res.items)
}

// —— Individual operations ——

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
		// A nonexistent/suspended account comes back as X's 400/404, and for "check whether this
		// person exists" that's an answer, not a failure.
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

// resolveUserID prefers a user id, falling back to a username lookup.
//
// When both are empty, this **does not default to the authorized account itself**: that would turn
// "forgot to bind a variable" into "silently pulling your own timeline", and it would look like
// everything was working fine.
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

// opHealthCheck checks whether this credential is still alive.
//
// **Unavailable must return ok=false, not an error**: the platform uses the conclusion to record
// credential status; returning an error would only tell the caller "the call failed", with no way
// to tell a broken credential from a network blip.
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
