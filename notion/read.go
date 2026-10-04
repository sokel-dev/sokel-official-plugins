package main

// Nine read operations, plus the credential health check (also a read-only call).

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/sokel-dev/sokel-official-plugins/notion/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

// opSearch searches pages and data sources. Notion search **only matches titles** — this is
// documented in the contract description.
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

// opPageGet reads one page: properties + markdown content.
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
		// Failing to fetch the content shouldn't fail the whole operation: the properties are
		// already in hand, and this path can fail simply because the page contains a block the
		// API can't read. Carry the reason in the content field itself.
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

// opDBQuery queries a data source's rows.
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

// opDBSchema fetches a schema.
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

// opDBList lists the data sources under a database.
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

// resolveDataSource turns whatever the user gave into a data source id.
//
// What a person has on hand is usually a **database** link (the one in the address bar), while
// queries need a data source id. Without handling this layer, the most common operation would
// reliably report object_not_found, with the real cause ("you gave a database, not a data source")
// nowhere in the error.
func resolveDataSource(ctx plugin.Ctx, raw string) (string, error) {
	id, err := requireID("data_source_id", raw)
	if err != nil {
		return "", err
	}
	// Try it as a data source first. If that works, it is one — no need to ask twice — and this
	// result goes into the cache, so the "fetch schema / build properties" step right after
	// doesn't have to ask again.
	ds, _, err := getDataSource(ctx, id)
	if err == nil {
		return ds.ID, nil
	}
	if !isNotFound(err) {
		return "", err
	}
	// Not a data source, so it's most likely a database: a single-source database can use its one
	// data source directly.
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
	// A multi-source database can't be picked on the user's behalf: picking wrong means writing
	// data into the wrong table, and it wouldn't even error.
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

// opBlockChildren reads child blocks (as-is).
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

// opUserList lists members.
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

// opUserGet reads one member; leaving it blank = read the integration's own bot identity (used to
// verify whether the token is valid).
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

// opHealthCheck runs a credential health check: a single read of /users/me (the integration's own
// identity).
//
// It's chosen because it's the only call that **doesn't depend on any page having been shared with
// the integration**: reading a page instead would error even for a perfectly valid token, just
// because that page hasn't been "connected" — turning the health check into a test of whether the
// user knows how to paste an id. The flip side needs saying too — passing only means the token is
// alive, not that any particular page is readable.
//
// When the token is invalid, this returns ok=false + message, **not** an error: the platform
// treats an error as "this plugin can't run a health check" and ok=false as "the health check
// concluded unavailable"; apiError has already translated unauthorized into plain language, and
// that message is exactly what should be shown.
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

// opCommentList lists comments.
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

// clamp uses the default when nothing is given, and caps anything above the max. **It never
// silently lets an oversized value through** — pulling tens of thousands of rows at once would
// burn through the rate limit and bloat the run record.
func clamp(v, def, max int) int {
	if v <= 0 {
		return def
	}
	if v > max {
		return max
	}
	return v
}
