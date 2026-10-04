package main

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func b64url(s string) string { return base64.URLEncoding.EncodeToString([]byte(s)) }

// Gmail's payload is a **recursive MIME tree**. Only looking at the first level would mean:
// plain-text messages get their body, but any message with an attachment comes out with an
// empty body — because the body got pushed down into the alternative level.
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

// A flat message (the most common plain-text case) must also work.
func TestExtractBodiesFlat(t *testing.T) {
	text, html := extractBodies(gmailPart{MimeType: "text/plain", Body: gmailBody{Data: b64url("hi")}})
	if text != "hi" || html != "" {
		t.Errorf("单层邮件: text=%q html=%q", text, html)
	}
}

// **text/plain inside an attachment is not the body**: a message with readme.txt attached must
// not end up with its body turned into that txt file's content. The criterion is that a
// non-empty filename means it's an attachment.
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

// The body is base64url (-_ instead of +/), and often has no padding.
// Decoding with standard base64 fails on content containing - or _.
func TestDecodeBodyHandlesBase64URLVariants(t *testing.T) {
	raw := "a?b>c~d" // encodes to something containing - or _
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
	// prefer empty over garbled bytes when decoding fails
	if got := decodeBody("!!!not base64!!!"); got != "" {
		t.Errorf("非法输入应回空而不是乱码: %q", got)
	}
}

// Message headers are **case-insensitive** (RFC 5322). Comparing literally would make the
// subject of some forwarded messages come out inexplicably empty.
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

// The attachment criterion is "has an attachmentId," not "has a filename": an inline image
// (cid:) has no name but is downloadable; a part with only a name and no attachmentId has no
// bytes to fetch, and including it would just make someone click it and get an error.
func TestExtractAttachmentsByAttachmentID(t *testing.T) {
	root := gmailPart{
		MimeType: "multipart/mixed",
		Parts: []gmailPart{
			{MimeType: "image/png", Filename: "", Body: gmailBody{AttachmentID: "inline1", Size: 10}}, // inline image, unnamed
			{MimeType: "application/pdf", Filename: "a.pdf", Body: gmailBody{AttachmentID: "att1", Size: 20}},
			{MimeType: "text/plain", Filename: "看着像附件.txt"}, // no attachmentId -> no bytes to fetch
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

// A regression test against the real shape: run a JSON blob close to an actual Gmail response
// through the parser, making sure field names (camelCase) and nesting all line up — a wrong
// json tag fails silently.
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
