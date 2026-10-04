package main

// 云文档（docx）/ 多维表格（bitable）/ 网盘（drive）。
//
// docx 的 Markdown 转换是**行级的、有意保守的**：# 标题、- 列表、1. 有序列表、
// ``` 代码块、> 引用、--- 分割线 → 对应文档块，其余一律按段落。行内样式
// （加粗/链接）原样保留为文本——docx 的行内 style 模型（elements + 坐标）复杂一个
// 数量级，首版不碰；要精排版的走 call 直调 blocks API。20% 的转换覆盖 95% 的用法。

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkdrive "github.com/larksuite/oapi-sdk-go/v3/service/drive/v1"

	"github.com/sokel-dev/sokel-official-plugins/feishu/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

// —— docx ——

// docxBlock 一个文档块（只用到 text 系块）。
func docxText(blockType int, content string) map[string]any {
	key := map[int]string{2: "text", 3: "heading1", 4: "heading2", 5: "heading3",
		12: "bullet", 13: "ordered", 14: "code", 15: "quote"}[blockType]
	return map[string]any{
		"block_type": blockType,
		key: map[string]any{
			"elements": []any{map[string]any{"text_run": map[string]any{"content": content}}},
		},
	}
}

// mdToBlocks Markdown → 文档块列表。
func mdToBlocks(md string) []map[string]any {
	var blocks []map[string]any
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	inCode := false
	var code []string
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") {
			if inCode { // 代码块收口
				blocks = append(blocks, docxText(14, strings.Join(code, "\n")))
				code, inCode = nil, false
			} else {
				inCode = true
			}
			continue
		}
		if inCode {
			code = append(code, line)
			continue
		}
		switch {
		case t == "":
			continue
		case t == "---" || t == "***":
			blocks = append(blocks, map[string]any{"block_type": 22, "divider": map[string]any{}})
		case strings.HasPrefix(t, "### "):
			blocks = append(blocks, docxText(5, t[4:]))
		case strings.HasPrefix(t, "## "):
			blocks = append(blocks, docxText(4, t[3:]))
		case strings.HasPrefix(t, "# "):
			blocks = append(blocks, docxText(3, t[2:]))
		case strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* "):
			blocks = append(blocks, docxText(12, t[2:]))
		case strings.HasPrefix(t, "> "):
			blocks = append(blocks, docxText(15, t[2:]))
		case orderedRe(t) != "":
			blocks = append(blocks, docxText(13, orderedRe(t)))
		default:
			blocks = append(blocks, docxText(2, t))
		}
	}
	if inCode && len(code) > 0 { // 没闭合的代码块也别丢内容
		blocks = append(blocks, docxText(14, strings.Join(code, "\n")))
	}
	return blocks
}

// orderedRe "1. xxx" → "xxx"；不是有序列表返回空串。
func orderedRe(t string) string {
	i := strings.Index(t, ". ")
	if i <= 0 || i > 3 {
		return ""
	}
	if _, err := strconv.Atoi(t[:i]); err != nil {
		return ""
	}
	return t[i+2:]
}

// appendBlocks 向文档根追加块。飞书单次上限 50 块，超了分批——
// 长报告一次几百块是常态，不分批就是 invalid param。
func appendBlocks(ctx plugin.Ctx, docID string, blocks []map[string]any) (int, error) {
	for i := 0; i < len(blocks); i += 50 {
		end := min(i+50, len(blocks))
		_, err := callRaw(ctx, credOf(ctx), "POST",
			"/open-apis/docx/v1/documents/"+docID+"/blocks/"+docID+"/children",
			map[string]any{"children": blocks[i:end]})
		if err != nil {
			if i > 0 {
				return i, fmt.Errorf("追加到第 %d 块后失败（前面的已写入）: %w", i, err)
			}
			return 0, err
		}
	}
	return len(blocks), nil
}

func docURL(cred Cred, docID string) string {
	host := "https://feishu.cn"
	if cred.Domain == "lark" {
		host = "https://larksuite.com"
	}
	return host + "/docx/" + docID
}

func opDocxCreate(ctx plugin.Ctx, in *DocxCreateIn) (*DocxCreateOut, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, fmt.Errorf("标题是空的")
	}
	body := map[string]any{"title": title}
	if ft := strings.TrimSpace(in.FolderToken); ft != "" {
		body["folder_token"] = ft
	}
	data, err := callRaw(ctx, credOf(ctx), "POST", "/open-apis/docx/v1/documents", body)
	if err != nil {
		return nil, err
	}
	var r struct {
		Document struct {
			DocumentID string `json:"document_id"`
		} `json:"document"`
	}
	_ = json.Unmarshal(data, &r)
	docID := r.Document.DocumentID
	if docID == "" {
		return nil, fmt.Errorf("文档建出来了但没拿到 document_id（应答形状变了？）")
	}
	if md := strings.TrimSpace(in.Markdown); md != "" {
		if _, err := appendBlocks(ctx, docID, mdToBlocks(md)); err != nil {
			return nil, fmt.Errorf("文档已创建（%s）但写内容失败: %w", docID, err)
		}
	}
	return &DocxCreateOut{DocumentID: docID, URL: docURL(credOf(ctx), docID)}, nil
}

// docIDFromAny 允许直接粘文档 URL。
func docIDFromAny(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "/docx/"); i >= 0 {
		s = s[i+len("/docx/"):]
		if j := strings.IndexAny(s, "?#/"); j >= 0 {
			s = s[:j]
		}
	}
	return s
}

func opDocxAppend(ctx plugin.Ctx, in *DocxAppendIn) (*DocxAppendOut, error) {
	docID := docIDFromAny(in.DocumentID)
	if docID == "" {
		return nil, fmt.Errorf("文档 ID 是空的（docx_create 的产出，或文档 URL）")
	}
	md := strings.TrimSpace(in.Markdown)
	if md == "" {
		return nil, fmt.Errorf("内容是空的")
	}
	n, err := appendBlocks(ctx, docID, mdToBlocks(md))
	if err != nil {
		return nil, err
	}
	return &DocxAppendOut{BlockCount: n}, nil
}

// —— bitable ——

// btPath 记录接口的公共前缀。app_token 允许直接粘表格 URL。
func btPath(appToken, tableID string) (string, error) {
	at, tid := strings.TrimSpace(appToken), strings.TrimSpace(tableID)
	if i := strings.Index(at, "/base/"); i >= 0 { // 允许粘 URL
		at = at[i+len("/base/"):]
		if j := strings.IndexAny(at, "?#/"); j >= 0 {
			at = at[:j]
		}
	}
	if at == "" || tid == "" {
		return "", fmt.Errorf("app_token 与 table_id 都要填（表格 URL 里 base/ 后与 table= 后那两串）")
	}
	return "/open-apis/bitable/v1/apps/" + at + "/tables/" + tid + "/records", nil
}

func opBitableListRecords(ctx plugin.Ctx, in *BitableListRecordsIn) (*BitableListRecordsOut, error) {
	base, err := btPath(in.AppToken, in.TableID)
	if err != nil {
		return nil, err
	}
	size := in.PageSize
	if size <= 0 {
		size = 100
	}
	if size > 500 {
		size = 500
	}
	path := fmt.Sprintf("%s?page_size=%d", base, size)
	if f := strings.TrimSpace(in.Filter); f != "" {
		path += "&filter=" + urlQuery(f)
	}
	if t := strings.TrimSpace(in.PageToken); t != "" {
		path += "&page_token=" + urlQuery(t)
	}
	data, err := callRaw(ctx, credOf(ctx), "GET", path, nil)
	if err != nil {
		return nil, err
	}
	var r struct {
		Items []struct {
			RecordID string         `json:"record_id"`
			Fields   map[string]any `json:"fields"`
		} `json:"items"`
		Total     int    `json:"total"`
		PageToken string `json:"page_token"`
		HasMore   bool   `json:"has_more"`
	}
	_ = json.Unmarshal(data, &r)
	out := &BitableListRecordsOut{Total: r.Total, PageToken: r.PageToken, HasMore: r.HasMore}
	for _, it := range r.Items {
		out.Records = append(out.Records, schema.BitableRecord{RecordID: it.RecordID, Fields: it.Fields})
	}
	return out, nil
}

func opBitableCreateRecord(ctx plugin.Ctx, in *BitableCreateRecordIn) (*BitableCreateRecordOut, error) {
	base, err := btPath(in.AppToken, in.TableID)
	if err != nil {
		return nil, err
	}
	if len(in.Fields) == 0 {
		return nil, fmt.Errorf("字段值是空的——键=字段名（与网页表头一致），值按字段类型填")
	}
	data, err := callRaw(ctx, credOf(ctx), "POST", base, map[string]any{"fields": in.Fields})
	if err != nil {
		return nil, err
	}
	var r struct {
		Record struct {
			RecordID string `json:"record_id"`
		} `json:"record"`
	}
	_ = json.Unmarshal(data, &r)
	return &BitableCreateRecordOut{RecordID: r.Record.RecordID}, nil
}

func opBitableUpdateRecord(ctx plugin.Ctx, in *BitableUpdateRecordIn) (*BitableUpdateRecordOut, error) {
	base, err := btPath(in.AppToken, in.TableID)
	if err != nil {
		return nil, err
	}
	rid := strings.TrimSpace(in.RecordID)
	if rid == "" {
		return nil, fmt.Errorf("记录 ID 是空的（rec 开头，来自查记录的产出）")
	}
	if len(in.Fields) == 0 {
		return nil, fmt.Errorf("字段值是空的")
	}
	if _, err := callRaw(ctx, credOf(ctx), "PUT", base+"/"+rid, map[string]any{"fields": in.Fields}); err != nil {
		return nil, err
	}
	return &BitableUpdateRecordOut{OK: true}, nil
}

func opBitableDeleteRecord(ctx plugin.Ctx, in *BitableDeleteRecordIn) (*BitableDeleteRecordOut, error) {
	base, err := btPath(in.AppToken, in.TableID)
	if err != nil {
		return nil, err
	}
	rid := strings.TrimSpace(in.RecordID)
	if rid == "" {
		return nil, fmt.Errorf("记录 ID 是空的")
	}
	if _, err := callRaw(ctx, credOf(ctx), "DELETE", base+"/"+rid, nil); err != nil {
		return nil, err
	}
	return &BitableDeleteRecordOut{OK: true}, nil
}

// —— drive ——

func opDriveUpload(ctx plugin.Ctx, in *DriveUploadIn) (*DriveUploadOut, error) {
	if in.File == nil || in.File.ID == "" {
		return nil, fmt.Errorf("没给文件")
	}
	folder := strings.TrimSpace(in.FolderToken)
	if i := strings.Index(folder, "/folder/"); i >= 0 { // 允许粘 URL
		folder = folder[i+len("/folder/"):]
		if j := strings.IndexAny(folder, "?#/"); j >= 0 {
			folder = folder[:j]
		}
	}
	if folder == "" {
		return nil, fmt.Errorf("文件夹 token 是空的——应用空间里的文件用户看不到，必须指定网盘文件夹（URL 里 folder/ 后那串）")
	}
	data, err := ctx.Fetch(in.File)
	if err != nil {
		return nil, fmt.Errorf("取文件失败: %w", err)
	}
	name := in.File.Name
	if name == "" {
		name = "file.bin"
	}
	c, err := clientOf(credOf(ctx))
	if err != nil {
		return nil, err
	}
	resp, err := c.Drive.File.UploadAll(ctx, larkdrive.NewUploadAllFileReqBuilder().
		Body(larkdrive.NewUploadAllFileReqBodyBuilder().
			FileName(name).ParentType("explorer").ParentNode(folder).
			Size(len(data)).File(strings.NewReader(string(data))).
			Build()).Build())
	if err != nil {
		return nil, connErr(err)
	}
	if !resp.Success() {
		return nil, feishuErr(resp.Code, resp.Msg)
	}
	token := larkcore.StringValue(resp.Data.FileToken)
	host := "https://feishu.cn"
	if credOf(ctx).Domain == "lark" {
		host = "https://larksuite.com"
	}
	return &DriveUploadOut{FileToken: token, URL: host + "/file/" + token}, nil
}
