package main

// 写操作九个。
//
// 一条共同的路子：**先取表结构再拼属性**（见 props.go）。多一次往返换来的是
// 「列名写错了」「这列是算出来的」「候选值只有这几个」在发请求之前就说清楚——
// 而 Notion 自己的 validation_error 只会告诉你 body 里第几个字段不合法。

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

// opPageCreate 建页面：父是页面就是普通子页，父是数据源就是建一行。
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
		// 标题单独给一个入参：它是每张表都有的那一列，但列名各不相同
		// （「名称」「Name」「任务」），让人每次去查列名纯属添堵。
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
			// 普通页面只有标题一个属性。悄悄丢掉用户填的属性，比报错难查得多。
			return nil, fmt.Errorf("父是页面时不能写属性（普通页面只有标题）；要写属性请把父设成数据源")
		}
		if in.Title != "" {
			props["title"] = map[string]any{"title": richText(in.Title)}
		}
	}
	if len(props) > 0 {
		body["properties"] = props
	}
	// markdown 直接进建页请求（Notion 支持），不必建完再补一次正文——
	// 两次请求意味着「页建好了但正文没写进去」这种半截状态。
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

// opPageUpdate 改属性。
func opPageUpdate(ctx plugin.Ctx, in *NotionPageUpdateIn) (*NotionPageUpdateOut, error) {
	id, err := requireID("page_id", in.PageID)
	if err != nil {
		return nil, err
	}
	body := map[string]any{}
	if len(in.Props) > 0 || in.Title != "" {
		// 属性要按列的类型来拼，而类型在**页面所属数据源**的表结构里——
		// 所以改属性必须先读一次页面。库外的普通页面没有数据源，只能改标题。
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

// opPageContent 改正文：追加 / 整页替换 / 搜索替换。
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

// opPageTrash 移进回收站 / 恢复。
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

// opBlockAppend 追加原始块。
func opBlockAppend(ctx plugin.Ctx, in *NotionBlockAppendIn) (*NotionBlockAppendOut, error) {
	id, err := requireID("block_id", in.BlockID)
	if err != nil {
		return nil, err
	}
	children, ok := in.Blocks.([]any)
	if !ok {
		// 单个块也认：手填时给一个对象是很自然的写法，为此报错纯属添堵。
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
		// Notion 单请求上限 100 块。静默截断会让人以为全写进去了。
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

// opCommentCreate 加评论。
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

// opDBCreate 建数据库。
func opDBCreate(ctx plugin.Ctx, in *NotionDBCreateIn) (*NotionDBCreateOut, error) {
	parentID, err := requireID("parent_page_id", in.ParentPageID)
	if err != nil {
		return nil, err
	}
	if len(in.Properties) == 0 {
		return nil, fmt.Errorf("要给列定义（至少一个 title 列，如 {\"名称\":{\"title\":{}}}）")
	}
	if !hasTitleColumn(in.Properties) {
		// Notion 会拒，但它的报错是 body.properties 层面的，看不出缺的是什么。
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

// opDBUpdateSchema 改表结构（加列/删列/改名）。
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
	invalidateDataSource(dsID) // 表结构变了，缓存里那份立刻作废
	return &NotionDBUpdateSchemaOut{DataSourceID: ds.ID, Properties: toPropSpecs(ds.Properties)}, nil
}

// opFileUpload 把平台文件层里的文件传进 Notion。
//
// 三步：申请一个 file_upload → 把字节 POST 上去 → 拿 id 给块或属性引用。
// 出参给的是 id 不是链接：Notion 的文件必须被某个块/属性引用才算落地，
// 直接给链接会让人以为拿到了一条能贴的地址。
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
	// 单次上传上限 20MB（超过要分片，那是另一套流程）。先说清楚，
	// 否则表现是传到一半被 Notion 拒，错因看不出来。
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

// sendFileBytes：字节走 multipart 传到 Notion 给的地址（不是 JSON 接口，所以不走 callAPI）。
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

// iconValue：一个 emoji 或一条图片地址。
func iconValue(s string) map[string]any {
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return map[string]any{"type": "external", "external": map[string]any{"url": s}}
	}
	return map[string]any{"type": "emoji", "emoji": s}
}
