package main

// 读操作九个，外加凭证体检（也是一次只读调用）。

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/sokel-dev/sokel-official-plugins/notion/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

// opSearch 搜索页面与数据源。Notion 的搜索**只匹配标题**，这一点已写进契约说明。
func opSearch(ctx plugin.Ctx, in *NotionSearchIn) (*NotionSearchOut, error) {
	body := map[string]any{}
	if in.Query != "" {
		body["query"] = in.Query
	}
	switch in.Type {
	case "page", "data_source":
		body["filter"] = map[string]any{"property": "object", "value": in.Type}
	}
	raws, _, _, err := paginate(ctx, reqOpts{method: http.MethodPost, path: "/search", body: body},
		clamp(in.MaxItems, 50, 200), "")
	if err != nil {
		return nil, err
	}
	items := make([]schema.SearchItem, 0, len(raws))
	for _, raw := range raws {
		var r searchResult
		if json.Unmarshal(raw, &r) != nil {
			continue
		}
		kind, pid := parentOf(r.Parent)
		title := r.Name
		if title == "" {
			title = richTitle(r.Title)
		}
		if title == "" {
			title = pageTitle(r.Properties)
		}
		items = append(items, schema.SearchItem{
			ID: r.ID, Object: r.Object, Title: title, URL: r.URL,
			ParentType: kind, ParentID: pid, LastEditedTime: r.LastEditedTime,
		})
	}
	return &NotionSearchOut{Items: items, Count: len(items)}, nil
}

// opPageGet 读一页：属性 + markdown 正文。
func opPageGet(ctx plugin.Ctx, in *NotionPageGetIn) (*NotionPageGetOut, error) {
	id, err := requireID("page_id", in.PageID)
	if err != nil {
		return nil, err
	}
	var p notionPage
	if err := callAPI(ctx, reqOpts{method: http.MethodGet, path: "/pages/" + id}, &p); err != nil {
		return nil, err
	}
	kind, pid := parentOf(p.Parent)
	out := &NotionPageGetOut{
		ID: p.ID, URL: p.URL, Title: pageTitle(p.Properties),
		Props: normalizeProps(p.Properties), PropertiesRaw: p.Properties,
		ParentType: kind, ParentID: pid,
		CreatedTime: p.CreatedTime, LastEditedTime: p.LastEditedTime,
		InTrash: p.InTrash || p.Archived,
	}
	if !in.WithContent {
		return out, nil
	}
	md, err := pageMarkdownOf(ctx, id)
	if err != nil {
		// 正文取不到不该让整个操作失败：属性已经拿到了，而正文这一路
		// 可能只是因为页面里有个 API 读不了的块。把原因带在正文位置上。
		out.Markdown = "（正文取不到：" + err.Error() + "）"
		return out, nil
	}
	out.Markdown, out.Truncated = md.Markdown, md.Truncated
	return out, nil
}

func pageMarkdownOf(ctx plugin.Ctx, id string) (pageMarkdown, error) {
	var md pageMarkdown
	err := callAPI(ctx, reqOpts{method: http.MethodGet, path: "/pages/" + id + "/markdown"}, &md)
	return md, err
}

// opDBQuery 查数据源的行。
func opDBQuery(ctx plugin.Ctx, in *NotionDBQueryIn) (*NotionDBQueryOut, error) {
	dsID, err := resolveDataSource(ctx, in.DataSourceID)
	if err != nil {
		return nil, err
	}
	body := map[string]any{}
	if in.Filter != nil {
		body["filter"] = in.Filter
	}
	if in.Sorts != nil {
		body["sorts"] = in.Sorts
	}
	raws, hasMore, next, err := paginate(ctx,
		reqOpts{method: http.MethodPost, path: "/data_sources/" + dsID + "/query", body: body},
		clamp(in.MaxItems, 100, 1000), in.StartCursor)
	if err != nil {
		return nil, err
	}
	pages := make([]schema.PageItem, 0, len(raws))
	for _, raw := range raws {
		var p notionPage
		if json.Unmarshal(raw, &p) != nil {
			continue
		}
		pages = append(pages, toPageItem(p))
	}
	return &NotionDBQueryOut{Pages: pages, Count: len(pages), HasMore: hasMore, NextCursor: next}, nil
}

// opDBSchema 取表结构。
func opDBSchema(ctx plugin.Ctx, in *NotionDBSchemaIn) (*NotionDBSchemaOut, error) {
	dsID, err := resolveDataSource(ctx, in.DataSourceID)
	if err != nil {
		return nil, err
	}
	ds, _, err := getDataSource(ctx, dsID)
	if err != nil {
		return nil, err
	}
	_, dbID := parentOf(ds.Parent)
	title := ds.Name
	if title == "" {
		title = richTitle(ds.Title)
	}
	return &NotionDBSchemaOut{
		DataSourceID: ds.ID, DatabaseID: dbID, Title: title,
		Properties: toPropSpecs(ds.Properties),
	}, nil
}

// opDBList 列出数据库下的数据源。
func opDBList(ctx plugin.Ctx, in *NotionDBListIn) (*NotionDBListOut, error) {
	id, err := requireID("database_id", in.DatabaseID)
	if err != nil {
		return nil, err
	}
	db, err := fetchDatabase(ctx, id)
	if err != nil {
		return nil, err
	}
	out := &NotionDBListOut{DatabaseID: db.ID, Title: richTitle(db.Title)}
	for _, d := range db.DataSources {
		out.DataSources = append(out.DataSources, schema.DataSourceRef{ID: d.ID, Name: d.Name})
	}
	if len(out.DataSources) > 0 {
		out.FirstDataSourceID = out.DataSources[0].ID
	}
	return out, nil
}

func fetchDatabase(ctx plugin.Ctx, id string) (database, error) {
	var db database
	err := callAPI(ctx, reqOpts{method: http.MethodGet, path: "/databases/" + id}, &db)
	return db, err
}

// resolveDataSource：把用户给的东西变成一个数据源 id。
//
// 人手上有的通常是**数据库**的链接（地址栏里那条），而查询要的是数据源 id。
// 不认这一层的话，最常见的一次操作会稳定地报 object_not_found，
// 而错因（"你给的是库不是表"）从错误里一个字也看不出来。
func resolveDataSource(ctx plugin.Ctx, raw string) (string, error) {
	id, err := requireID("data_source_id", raw)
	if err != nil {
		return "", err
	}
	// 先按数据源试。成了就是数据源，不必多问——而且这一次的结果进了缓存，
	// 紧接着的「取表结构 / 拼属性」不必再问一遍。
	ds, _, err := getDataSource(ctx, id)
	if err == nil {
		return ds.ID, nil
	}
	if !isNotFound(err) {
		return "", err
	}
	// 不是数据源，那多半是数据库：单源库直接取它唯一的数据源。
	db, dbErr := fetchDatabase(ctx, id)
	if dbErr != nil {
		return "", fmt.Errorf("%s\n（这个 id 既不是数据源也不是数据库；数据库链接可以直接贴，多源库请先用「列出数据源」）", dbErr)
	}
	switch len(db.DataSources) {
	case 0:
		return "", fmt.Errorf("数据库「%s」下没有数据源", richTitle(db.Title))
	case 1:
		return db.DataSources[0].ID, nil
	}
	// 多源库不能替用户挑：挑错了就是往另一张表里写数据，而且不会报错。
	var names string
	for _, d := range db.DataSources {
		names += fmt.Sprintf("\n  %s  %s", d.ID, d.Name)
	}
	return "", fmt.Errorf("数据库「%s」下有 %d 个数据源，请指明用哪一个：%s",
		richTitle(db.Title), len(db.DataSources), names)
}

func isNotFound(err error) bool {
	ae, ok := err.(*apiError)
	return ok && (ae.Code == "object_not_found" || ae.Status == http.StatusNotFound)
}

// opBlockChildren 读子块（原样）。
func opBlockChildren(ctx plugin.Ctx, in *NotionBlockChildrenIn) (*NotionBlockChildrenOut, error) {
	id, err := requireID("block_id", in.BlockID)
	if err != nil {
		return nil, err
	}
	raws, _, next, err := paginate(ctx,
		reqOpts{method: http.MethodGet, path: "/blocks/" + id + "/children"},
		clamp(in.MaxItems, 100, 500), "")
	if err != nil {
		return nil, err
	}
	blocks := make([]any, 0, len(raws))
	for _, raw := range raws {
		var b any
		if json.Unmarshal(raw, &b) == nil {
			blocks = append(blocks, b)
		}
	}
	return &NotionBlockChildrenOut{Blocks: blocks, Count: len(blocks), NextCursor: next}, nil
}

// opUserList 列成员。
func opUserList(ctx plugin.Ctx, in *NotionUserListIn) (*NotionUserListOut, error) {
	users, err := listAllUsers(ctx, clamp(in.MaxItems, 100, 500))
	if err != nil {
		return nil, err
	}
	return &NotionUserListOut{Users: users, Count: len(users)}, nil
}

func listAllUsers(ctx plugin.Ctx, max int) ([]schema.User, error) {
	raws, _, _, err := paginate(ctx, reqOpts{method: http.MethodGet, path: "/users"}, max, "")
	if err != nil {
		return nil, err
	}
	out := make([]schema.User, 0, len(raws))
	for _, raw := range raws {
		var u notionUser
		if json.Unmarshal(raw, &u) != nil {
			continue
		}
		out = append(out, toUser(u))
	}
	return out, nil
}

// opUserGet 读一个成员；留空 = 读集成自己的机器人身份（拿来验证令牌是否有效）。
func opUserGet(ctx plugin.Ctx, in *NotionUserGetIn) (*NotionUserGetOut, error) {
	path := "/users/me"
	if id := notionID(in.UserID); id != "" {
		path = "/users/" + id
	}
	var u notionUser
	if err := callAPI(ctx, reqOpts{method: http.MethodGet, path: path}, &u); err != nil {
		return nil, err
	}
	return &NotionUserGetOut{ID: u.ID, Name: u.Name, Type: u.Type, Email: u.Person.Email, AvatarURL: u.AvatarURL}, nil
}

// opHealthCheck 凭证体检：读一次 /users/me（集成自己的身份）。
//
// 选它是因为它是唯一**不依赖任何页面是否已交给集成**的调用：换成读一页，
// 令牌明明有效也会因为那一页没「连接」而报错，体检就变成了在考用户会不会贴 id。
// 反过来也要说清楚——它通过只代表令牌活着，不代表读得到某一页。
//
// 令牌无效时返回 ok=false + message 而**不是** error：平台把 error 当「这个插件没法体检」，
// 把 ok=false 当「体检结论是不可用」；apiError 那边已经把 unauthorized 翻成了人话，
// 那句话正是要显示的东西。
func opHealthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	var me struct {
		Name string `json:"name"`
		Bot  struct {
			WorkspaceName string `json:"workspace_name"`
		} `json:"bot"`
	}
	if err := callAPI(ctx, reqOpts{method: http.MethodGet, path: "/users/me"}, &me); err != nil {
		return &HealthCheckOut{Message: err.Error()}, nil
	}
	who := me.Name
	if who == "" {
		who = "未命名集成"
	}
	out := &HealthCheckOut{OK: true, Name: me.Name, WorkspaceName: me.Bot.WorkspaceName,
		Message: fmt.Sprintf("令牌有效（%s）", who)}
	if ws := me.Bot.WorkspaceName; ws != "" {
		out.Message = fmt.Sprintf("令牌有效（%s，工作空间 %s）", who, ws)
	}
	return out, nil
}

// opCommentList 列评论。
func opCommentList(ctx plugin.Ctx, in *NotionCommentListIn) (*NotionCommentListOut, error) {
	id, err := requireID("block_id", in.BlockID)
	if err != nil {
		return nil, err
	}
	q := map[string][]string{"block_id": {id}}
	raws, _, _, err := paginate(ctx,
		reqOpts{method: http.MethodGet, path: "/comments", query: q},
		clamp(in.MaxItems, 100, 500), "")
	if err != nil {
		return nil, err
	}
	out := make([]schema.Comment, 0, len(raws))
	for _, raw := range raws {
		var c notionComment
		if json.Unmarshal(raw, &c) != nil {
			continue
		}
		out = append(out, toComment(c))
	}
	return &NotionCommentListOut{Comments: out, Count: len(out)}, nil
}

// clamp：没给用默认，给多了压到上限。**不静默放行超大值**——
// 一次拉几万行会把限流吃光，还会把运行记录撑爆。
func clamp(v, def, max int) int {
	if v <= 0 {
		return def
	}
	if v > max {
		return max
	}
	return v
}
