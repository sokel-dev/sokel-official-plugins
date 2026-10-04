package main

// Nine write operations.
//
// One shared approach: **fetch the schema before assembling properties** (see props.go). The extra
// round trip buys clear, pre-request errors for "wrong column name", "this column is computed",
// "only these candidate values exist" — whereas Notion's own validation_error only tells you which
// field index in the body is invalid.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

// opPageCreate creates a page: a page parent means an ordinary subpage, a data source parent means
// creating a row.
func opPageCreate(ctx plugin.Ctx, in *NotionPageCreateIn) (*NotionPageCreateOut, error) {
	parentID, err := requireID("parent_id", in.ParentID)
	if err != nil {
		return nil, err
	}
	body := map[string]any{}
	props := map[string]any{}

	if in.ParentType == "data_source" {
		dsID, err := resolveDataSource(ctx, parentID)
		if err != nil {
			return nil, err
		}
		body["parent"] = map[string]any{"type": "data_source_id", "data_source_id": dsID}
		if props, err = buildProps(ctx, dsID, in.Props); err != nil {
			return nil, err
		}
		if props == nil {
			props = map[string]any{}
		}
		// Title gets its own input field: every table has this column, but the name differs
		// ("名称", "Name", "任务"), and making people look it up every time would just be friction.
		if in.Title != "" {
			specs, err := dataSourceProps(ctx, dsID)
			if err != nil {
				return nil, err
			}
			col := titleColumn(specs)
			if col == "" {
				return nil, fmt.Errorf("这个数据源没有标题列，标题请写进 props")
			}
			props[col] = map[string]any{"title": richText(in.Title)}
		}
	} else {
		body["parent"] = map[string]any{"type": "page_id", "page_id": parentID}
		if len(in.Props) > 0 {
			// An ordinary page has only a title property. Silently dropping what the user filled
			// in for props would be far harder to debug than erroring.
			return nil, fmt.Errorf("父是页面时不能写属性（普通页面只有标题）；要写属性请把父设成数据源")
		}
		if in.Title != "" {
			props["title"] = map[string]any{"title": richText(in.Title)}
		}
	}
	if len(props) > 0 {
		body["properties"] = props
	}
	// markdown goes straight into the create-page request (Notion supports this), avoiding a
	// second call to fill in content afterward — two requests would mean a half-done state where
	// "the page exists but the content wasn't written" is possible.
	if in.Markdown != "" {
		body["markdown"] = in.Markdown
	}
	if in.Icon != "" {
		body["icon"] = iconValue(in.Icon)
	}

	var p notionPage
	if err := callAPI(ctx, reqOpts{method: http.MethodPost, path: "/pages", body: body}, &p); err != nil {
		return nil, err
	}
	return &NotionPageCreateOut{ID: p.ID, URL: p.URL, Title: pageTitle(p.Properties)}, nil
}

// opPageUpdate updates properties.
func opPageUpdate(ctx plugin.Ctx, in *NotionPageUpdateIn) (*NotionPageUpdateOut, error) {
	id, err := requireID("page_id", in.PageID)
	if err != nil {
		return nil, err
	}
	body := map[string]any{}
	if len(in.Props) > 0 || in.Title != "" {
		// Properties have to be assembled by column type, and the type lives in the schema of
		// **the page's data source** — so updating properties always starts with reading the page
		// once. An ordinary page outside any database has no data source and can only have its
		// title changed.
		var cur notionPage
		if err := callAPI(ctx, reqOpts{method: http.MethodGet, path: "/pages/" + id}, &cur); err != nil {
			return nil, err
		}
		kind, dsID := parentOf(cur.Parent)
		props := map[string]any{}
		if len(in.Props) > 0 {
			if kind != "data_source" {
				return nil, fmt.Errorf("这一页不在数据源里（父是 %s），只有标题可以改", kind)
			}
			if props, err = buildProps(ctx, dsID, in.Props); err != nil {
				return nil, err
			}
		}
		if in.Title != "" {
			col := "title"
			if kind == "data_source" {
				specs, err := dataSourceProps(ctx, dsID)
				if err != nil {
					return nil, err
				}
				if c := titleColumn(specs); c != "" {
					col = c
				}
			}
			props[col] = map[string]any{"title": richText(in.Title)}
		}
		body["properties"] = props
	}
	if in.Icon != "" {
		body["icon"] = iconValue(in.Icon)
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("没有要改的东西（标题/属性/图标至少给一个）")
	}
	var p notionPage
	if err := callAPI(ctx, reqOpts{method: http.MethodPatch, path: "/pages/" + id, body: body}, &p); err != nil {
		return nil, err
	}
	return &NotionPageUpdateOut{ID: p.ID, URL: p.URL}, nil
}

// opPageContent edits content: append / full-page replace / find-and-replace.
func opPageContent(ctx plugin.Ctx, in *NotionPageContentIn) (*NotionPageContentOut, error) {
	id, err := requireID("page_id", in.PageID)
	if err != nil {
		return nil, err
	}
	var body map[string]any
	switch in.Mode {
	case "", "append":
		if in.Markdown == "" {
			return nil, fmt.Errorf("追加模式要给 markdown")
		}
		body = map[string]any{"insert_content": map[string]any{"new_str": in.Markdown, "position": "end"}}
	case "replace":
		if in.Markdown == "" {
			return nil, fmt.Errorf("整页替换要给 markdown（要清空正文请显式给一个空格）")
		}
		body = map[string]any{"replace_content": map[string]any{"new_str": in.Markdown}}
	case "edit":
		if in.OldStr == "" {
			return nil, fmt.Errorf("搜索替换要给 old_str（必须与页面里的文字一字不差）")
		}
		u := map[string]any{"old_str": in.OldStr, "new_str": in.NewStr}
		if in.ReplaceAll {
			u["replace_all_matches"] = true
		}
		body = map[string]any{"update_content": map[string]any{"content_updates": []any{u}}}
	default:
		return nil, fmt.Errorf("mode 只能是 append / replace / edit，给的是 %q", in.Mode)
	}

	var md pageMarkdown
	if err := callAPI(ctx, reqOpts{method: http.MethodPatch, path: "/pages/" + id + "/markdown", body: body}, &md); err != nil {
		return nil, err
	}
	return &NotionPageContentOut{ID: id, Markdown: md.Markdown, Truncated: md.Truncated}, nil
}

// opPageTrash moves to trash / restores.
func opPageTrash(ctx plugin.Ctx, in *NotionPageTrashIn) (*NotionPageTrashOut, error) {
	id, err := requireID("page_id", in.PageID)
	if err != nil {
		return nil, err
	}
	var p notionPage
	body := map[string]any{"in_trash": !in.Restore}
	if err := callAPI(ctx, reqOpts{method: http.MethodPatch, path: "/pages/" + id, body: body}, &p); err != nil {
		return nil, err
	}
	return &NotionPageTrashOut{ID: p.ID, InTrash: p.InTrash || p.Archived}, nil
}

// opBlockAppend appends raw blocks.
func opBlockAppend(ctx plugin.Ctx, in *NotionBlockAppendIn) (*NotionBlockAppendOut, error) {
	id, err := requireID("block_id", in.BlockID)
	if err != nil {
		return nil, err
	}
	children, ok := in.Blocks.([]any)
	if !ok {
		// A single block is accepted too: giving one object when filling this in by hand is the
		// natural thing to do, and erroring over it would just be friction.
		if m, isObj := in.Blocks.(map[string]any); isObj {
			children = []any{m}
		} else {
			return nil, fmt.Errorf("blocks 要是 Notion 块对象的数组")
		}
	}
	if len(children) == 0 {
		return nil, fmt.Errorf("blocks 是空的")
	}
	if len(children) > 100 {
		// Notion caps a single request at 100 blocks. Silently truncating would make people
		// think everything got written.
		return nil, fmt.Errorf("一次最多 100 块，给了 %d 块（分几次追加）", len(children))
	}
	var resp struct {
		Results []struct {
			ID string `json:"id"`
		} `json:"results"`
	}
	if err := callAPI(ctx, reqOpts{method: http.MethodPatch, path: "/blocks/" + id + "/children",
		body: map[string]any{"children": children}}, &resp); err != nil {
		return nil, err
	}
	out := &NotionBlockAppendOut{Count: len(resp.Results)}
	for _, r := range resp.Results {
		out.BlockIDs = append(out.BlockIDs, r.ID)
	}
	return out, nil
}

// opCommentCreate adds a comment.
func opCommentCreate(ctx plugin.Ctx, in *NotionCommentCreateIn) (*NotionCommentCreateOut, error) {
	if strings.TrimSpace(in.Text) == "" {
		return nil, fmt.Errorf("评论内容是空的")
	}
	body := map[string]any{"rich_text": richText(in.Text)}
	switch {
	case in.DiscussionID != "":
		body["discussion_id"] = notionID(in.DiscussionID)
	case in.PageID != "":
		body["parent"] = map[string]any{"type": "page_id", "page_id": notionID(in.PageID)}
	default:
		return nil, fmt.Errorf("要么给 page_id（新开一串评论），要么给 discussion_id（回已有的串）")
	}
	var c notionComment
	if err := callAPI(ctx, reqOpts{method: http.MethodPost, path: "/comments", body: body}, &c); err != nil {
		return nil, err
	}
	return &NotionCommentCreateOut{ID: c.ID, DiscussionID: c.DiscussionID}, nil
}

// opDBCreate creates a database.
func opDBCreate(ctx plugin.Ctx, in *NotionDBCreateIn) (*NotionDBCreateOut, error) {
	parentID, err := requireID("parent_page_id", in.ParentPageID)
	if err != nil {
		return nil, err
	}
	if len(in.Properties) == 0 {
		return nil, fmt.Errorf("要给列定义（至少一个 title 列，如 {\"名称\":{\"title\":{}}}）")
	}
	if !hasTitleColumn(in.Properties) {
		// Notion would reject this too, but its error is at the body.properties level and
		// doesn't reveal what's actually missing.
		return nil, fmt.Errorf("列定义里必须**恰好有一个** title 列，如 {\"名称\":{\"title\":{}}}")
	}
	body := map[string]any{
		"parent":              map[string]any{"type": "page_id", "page_id": parentID},
		"title":               richText(in.Title),
		"initial_data_source": map[string]any{"properties": in.Properties},
	}
	var db database
	if err := callAPI(ctx, reqOpts{method: http.MethodPost, path: "/databases", body: body}, &db); err != nil {
		return nil, err
	}
	out := &NotionDBCreateOut{DatabaseID: db.ID, URL: db.URL}
	if len(db.DataSources) > 0 {
		out.DataSourceID = db.DataSources[0].ID
	}
	return out, nil
}

func hasTitleColumn(props map[string]any) bool {
	for _, v := range props {
		if m, ok := v.(map[string]any); ok {
			if _, has := m["title"]; has {
				return true
			}
		}
	}
	return false
}

// opDBUpdateSchema updates a schema (add/remove columns, rename).
func opDBUpdateSchema(ctx plugin.Ctx, in *NotionDBUpdateSchemaIn) (*NotionDBUpdateSchemaOut, error) {
	dsID, err := resolveDataSource(ctx, in.DataSourceID)
	if err != nil {
		return nil, err
	}
	body := map[string]any{}
	if in.Title != "" {
		body["title"] = richText(in.Title)
	}
	if len(in.Properties) > 0 {
		body["properties"] = in.Properties
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("没有要改的东西（改名/列改动至少给一个）")
	}
	var ds dataSource
	if err := callAPI(ctx, reqOpts{method: http.MethodPatch, path: "/data_sources/" + dsID, body: body}, &ds); err != nil {
		return nil, err
	}
	invalidateDataSource(dsID) // Schema changed, evict the cached copy immediately
	return &NotionDBUpdateSchemaOut{DataSourceID: ds.ID, Properties: toPropSpecs(ds.Properties)}, nil
}

// opFileUpload sends a file from the platform's file layer into Notion.
//
// Three steps: request a file_upload -> POST the bytes -> hand the id to a block or property
// reference. The output gives an id, not a link: a Notion file only "lands" once some block or
// property references it, and handing back a link would make people think they got a pastable URL.
func opFileUpload(ctx plugin.Ctx, in *NotionFileUploadIn) (*NotionFileUploadOut, error) {
	if in.File == nil {
		return nil, fmt.Errorf("没有给文件")
	}
	data, err := in.File.Blob(ctx)
	if err != nil {
		return nil, fmt.Errorf("取文件字节失败: %w", err)
	}
	name := in.Filename
	if name == "" {
		name = in.File.Name
	}
	if name == "" {
		name = "upload.bin"
	}
	// Single-upload cap is 20MB (going over requires chunked upload, a separate flow). Say so
	// upfront, otherwise the symptom is Notion rejecting it partway through with no clue why.
	const singlePartMax = 20 << 20
	if len(data) > singlePartMax {
		return nil, fmt.Errorf("文件 %.1f MB，超过 Notion 单次上传上限 20 MB", float64(len(data))/(1<<20))
	}

	var created struct {
		ID        string `json:"id"`
		UploadURL string `json:"upload_url"`
	}
	body := map[string]any{"filename": name, "mode": "single_part"}
	if in.File.Mime != "" {
		body["content_type"] = in.File.Mime
	}
	if err := callAPI(ctx, reqOpts{method: http.MethodPost, path: "/file_uploads", body: body}, &created); err != nil {
		return nil, err
	}
	if err := sendFileBytes(ctx, created.UploadURL, name, in.File.Mime, data); err != nil {
		return nil, err
	}
	return &NotionFileUploadOut{FileUploadID: created.ID, Filename: name}, nil
}

// sendFileBytes sends bytes as multipart to the URL Notion handed back (not a JSON endpoint, so it
// doesn't go through callAPI).
func sendFileBytes(ctx plugin.Ctx, uploadURL, name, mime string, data []byte) error {
	cred := sokel.CredentialAs[Cred](ctx)
	tok, err := authToken(cred)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", name)
	if err != nil {
		return err
	}
	if _, err := part.Write(data); err != nil {
		return err
	}
	if mime != "" {
		_ = mw.WriteField("content_type", mime)
	}
	if err := mw.Close(); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Notion-Version", notionVersion)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	if err := limiterFor(tok).wait(ctx); err != nil {
		return err
	}
	resp, err := clientFor(cred.Proxy).Do(req)
	if err != nil {
		return fmt.Errorf("上传文件到 Notion 失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		var e struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &e)
		return (&apiError{Status: resp.StatusCode, Code: e.Code, Message: e.Message})
	}
	return nil
}

// iconValue builds an emoji or image URL icon.
func iconValue(s string) map[string]any {
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return map[string]any{"type": "external", "external": map[string]any{"url": s}}
	}
	return map[string]any{"type": "emoji", "emoji": s}
}
