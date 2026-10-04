package main

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func b64url(s string) string { return base64.URLEncoding.EncodeToString([]byte(s)) }

// Gmail 的 payload 是**递归的 MIME 树**。只看第一层的话，
// 纯文本邮件能取到正文、带附件的邮件正文全空——因为正文被推进了 alternative 那层。
func TestExtractBodiesWalksMimeTree(t *testing.T) {
	// multipart/mixed → [multipart/alternative → (plain, html), application/pdf]
	root := gmailPart{
		MimeType: "multipart/mixed",
		Parts: []gmailPart{
			{
				MimeType: "multipart/alternative",
				Parts: []gmailPart{
					{MimeType: "text/plain", Body: gmailBody{Data: b64url("纯文本正文")}},
					{MimeType: "text/html", Body: gmailBody{Data: b64url("<p>HTML 正文</p>")}},
				},
			},
			{MimeType: "application/pdf", Filename: "a.pdf", Body: gmailBody{AttachmentID: "att1", Size: 100}},
		},
	}
	text, html := extractBodies(root)
	if text != "纯文本正文" {
		t.Errorf("嵌套正文没取到: %q", text)
	}
	if html != "<p>HTML 正文</p>" {
		t.Errorf("嵌套 HTML 没取到: %q", html)
	}
}

// 单层邮件（最常见的纯文本信）也要работать。
func TestExtractBodiesFlat(t *testing.T) {
	text, html := extractBodies(gmailPart{MimeType: "text/plain", Body: gmailBody{Data: b64url("hi")}})
	if text != "hi" || html != "" {
		t.Errorf("单层邮件: text=%q html=%q", text, html)
	}
}

// **附件里的 text/plain 不是正文**：一封带 readme.txt 的邮件，
// 正文不能变成那个 txt 的内容。判据是 filename 非空即附件。
func TestTextAttachmentIsNotBody(t *testing.T) {
	root := gmailPart{
		MimeType: "multipart/mixed",
		Parts: []gmailPart{
			{MimeType: "text/plain", Filename: "readme.txt", Body: gmailBody{Data: b64url("我是附件不是正文"), AttachmentID: "a1"}},
			{MimeType: "text/plain", Body: gmailBody{Data: b64url("我才是正文")}},
		},
	}
	if text, _ := extractBodies(root); text != "我才是正文" {
		t.Errorf("把 .txt 附件当成正文了: %q", text)
	}
}

// 正文是 base64url（-_ 而非 +/），且常常没有 padding。
// 用标准 base64 解会在含 - 或 _ 的内容上失败。
func TestDecodeBodyHandlesBase64URLVariants(t *testing.T) {
	raw := "a?b>c~d" // 编码后会出现 - 或 _
	std := base64.URLEncoding.EncodeToString([]byte(raw))
	rawNoPad := base64.RawURLEncoding.EncodeToString([]byte(raw))
	if got := decodeBody(std); got != raw {
		t.Errorf("带 padding 的 base64url 解不出: %q", got)
	}
	if got := decodeBody(rawNoPad); got != raw {
		t.Errorf("无 padding 的 base64url 解不出: %q", got)
	}
	if got := decodeBody(""); got != "" {
		t.Errorf("空应回空")
	}
	// 解不出宁可空，也不要返回乱码
	if got := decodeBody("!!!not base64!!!"); got != "" {
		t.Errorf("非法输入应回空而不是乱码: %q", got)
	}
}

// 邮件头**大小写不敏感**（RFC 5322）。按字面比对的话，
// 某些转发链路来的邮件主题会莫名为空。
func TestHeaderIsCaseInsensitive(t *testing.T) {
	p := gmailPart{Headers: []gmailHdr{
		{Name: "subject", Value: "小写的主题"},
		{Name: "FROM", Value: "a@b.com"},
	}}
	if got := header(p, "Subject"); got != "小写的主题" {
		t.Errorf("大小写不敏感失败: %q", got)
	}
	if got := header(p, "From"); got != "a@b.com" {
		t.Errorf("大小写不敏感失败: %q", got)
	}
	if got := header(p, "Cc"); got != "" {
		t.Errorf("不存在的头应回空: %q", got)
	}
}

// 附件判据是「有 attachmentId」而不是「有 filename」：
// 内嵌图片（cid:）没名字但可下载；只有名字没 attachmentId 的拉不到字节，收进来只会让人点了报错。
func TestExtractAttachmentsByAttachmentID(t *testing.T) {
	root := gmailPart{
		MimeType: "multipart/mixed",
		Parts: []gmailPart{
			{MimeType: "image/png", Filename: "", Body: gmailBody{AttachmentID: "inline1", Size: 10}}, // 内嵌图，无名
			{MimeType: "application/pdf", Filename: "a.pdf", Body: gmailBody{AttachmentID: "att1", Size: 20}},
			{MimeType: "text/plain", Filename: "看着像附件.txt"}, // 无 attachmentId → 拉不到字节
		},
	}
	got := extractAttachments(root)
	if len(got) != 2 {
		t.Fatalf("应收 2 个可下载部件, got %d: %+v", len(got), got)
	}
	if got[0].AttachmentID != "inline1" || got[1].Filename != "a.pdf" {
		t.Errorf("附件解析不对: %+v", got)
	}
	if !hasAttachments(root) {
		t.Error("hasAttachments 应为 true")
	}
	if hasAttachments(gmailPart{MimeType: "text/plain"}) {
		t.Error("纯文本信不该判成有附件")
	}
}

// 真实形状回归：拿一段贴近 Gmail 实际应答的 JSON 走一遍，
// 确保字段名（camelCase）与嵌套都对得上——写错一个 json tag 是静默失效。
func TestParseRealisticPayload(t *testing.T) {
	raw := `{
	  "id":"18f","threadId":"18t","snippet":"摘要…","labelIds":["INBOX","UNREAD"],
	  "payload":{
	    "mimeType":"multipart/mixed",
	    "headers":[{"name":"Subject","value":"季度报告"},{"name":"From","value":"boss@x.com"}],
	    "parts":[
	      {"mimeType":"multipart/alternative","parts":[
	        {"mimeType":"text/plain","body":{"size":9,"data":"` + b64url("请查收") + `"}}
	      ]},
	      {"mimeType":"application/pdf","filename":"q3.pdf","body":{"attachmentId":"ANGjd","size":52428}}
	    ]
	  }}`
	var m gmailMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	if m.ID != "18f" || m.ThreadID != "18t" || len(m.LabelIDs) != 2 {
		t.Errorf("顶层字段没解出来: %+v", m)
	}
	if got := header(m.Payload, "subject"); got != "季度报告" {
		t.Errorf("主题: %q", got)
	}
	if text, _ := extractBodies(m.Payload); text != "请查收" {
		t.Errorf("正文: %q", text)
	}
	att := extractAttachments(m.Payload)
	if len(att) != 1 || att[0].Filename != "q3.pdf" || att[0].Size != 52428 {
		t.Errorf("附件: %+v", att)
	}
}
