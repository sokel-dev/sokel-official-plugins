package main

// Gmail 邮件结构的解析。
//
// Gmail 的 message.payload 是一棵**递归的 MIME 树**，不是平铺字段：
//
//	multipart/mixed
//	├── multipart/alternative
//	│   ├── text/plain      ← 正文（纯文本）
//	│   └── text/html       ← 正文（HTML）
//	└── application/pdf     ← 附件
//
// 只看第一层就会：纯文本邮件能取到正文，带附件的邮件正文全空——因为正文被推到了
// alternative 那一层里。所以必须递归。

import (
	"encoding/base64"
	"strings"

	"github.com/sokel-dev/sokel-official-plugins/gmail/schema"
)

// gmailMessage：只声明用得上的字段（Gmail 的应答很大，全声明反而掩盖真正依赖的是哪些）。
type gmailMessage struct {
	ID       string    `json:"id"`
	ThreadID string    `json:"threadId"`
	Snippet  string    `json:"snippet"`
	LabelIDs []string  `json:"labelIds"`
	Payload  gmailPart `json:"payload"`
}

type gmailPart struct {
	PartID   string      `json:"partId"`
	MimeType string      `json:"mimeType"`
	Filename string      `json:"filename"`
	Headers  []gmailHdr  `json:"headers"`
	Body     gmailBody   `json:"body"`
	Parts    []gmailPart `json:"parts"`
}

type gmailHdr struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type gmailBody struct {
	AttachmentID string `json:"attachmentId"`
	Size         int    `json:"size"`
	Data         string `json:"data"` // base64url
}

// header：取邮件头（**大小写不敏感**）。
// Gmail 回的是 "Subject"，但转发链路里出现 "subject"/"SUBJECT" 都合法（RFC 5322 规定头名不区分大小写）。
// 按字面比对的话，某些来源的邮件主题会莫名其妙为空。
func header(p gmailPart, name string) string {
	for _, h := range p.Headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

// decodeBody：Gmail 的正文是 **base64url**（用 -_ 而不是 +/），且常常没有 padding。
// 用标准 base64 解会在含 - 或 _ 的内容上失败；带 padding 的用 RawURLEncoding 又会失败。
// 两种都试，解不出就返回空——正文缺失比返回一段乱码强。
func decodeBody(data string) string {
	if data == "" {
		return ""
	}
	if b, err := base64.URLEncoding.DecodeString(data); err == nil {
		return string(b)
	}
	if b, err := base64.RawURLEncoding.DecodeString(data); err == nil {
		return string(b)
	}
	return ""
}

// extractBodies：递归取正文。返回 (纯文本, HTML)。
//
// 只认**非附件**的 text/plain 与 text/html：附件也可能是 text/plain（比如 .txt 附件），
// 把它当正文的话，一封带 readme.txt 的邮件正文会变成那个 txt 的内容。
// 判据是 filename 为空 —— 有名字的就是附件。
func extractBodies(p gmailPart) (text, html string) {
	var walk func(gmailPart)
	walk = func(part gmailPart) {
		if part.Filename == "" {
			switch {
			case strings.HasPrefix(part.MimeType, "text/plain") && text == "":
				text = decodeBody(part.Body.Data)
			case strings.HasPrefix(part.MimeType, "text/html") && html == "":
				html = decodeBody(part.Body.Data)
			}
		}
		for _, c := range part.Parts {
			walk(c)
		}
	}
	walk(p)
	return text, html
}

// extractAttachments：递归收集附件。
//
// 判据是「有 attachmentId」而不是「有 filename」：内嵌图片（正文里引用的 cid: 图）
// 同样有 attachmentId 但可能没名字，它们确实是可下载的部件。
// 反过来，只有 filename 没有 attachmentId 的部件拉不到字节，收进来只会让人点了报错。
func extractAttachments(p gmailPart) []schema.AttachmentRef {
	out := []schema.AttachmentRef{}
	var walk func(gmailPart)
	walk = func(part gmailPart) {
		if part.Body.AttachmentID != "" {
			out = append(out, schema.AttachmentRef{
				AttachmentID: part.Body.AttachmentID,
				Filename:     part.Filename,
				MimeType:     part.MimeType,
				Size:         part.Body.Size,
			})
		}
		for _, c := range part.Parts {
			walk(c)
		}
	}
	walk(p)
	return out
}

// hasAttachments：是否带附件（事件里只给这个布尔，不给清单——
// 清单要调「读邮件」才有，事件载荷不该驮着可能很长的附件列表）。
func hasAttachments(p gmailPart) bool { return len(extractAttachments(p)) > 0 }
