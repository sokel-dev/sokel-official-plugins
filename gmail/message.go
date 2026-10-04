package main

// Parsing of the Gmail message structure.
//
// Gmail's message.payload is a **recursive MIME tree**, not flat fields:
//
//	multipart/mixed
//	├── multipart/alternative
//	│   ├── text/plain      ← body (plain text)
//	│   └── text/html       ← body (HTML)
//	└── application/pdf     ← attachment
//
// Only looking at the first level would mean: plain-text messages get their body, but any
// message with an attachment comes out with an empty body — because the body got pushed down
// into the alternative level. So recursion is required.

import (
	"encoding/base64"
	"strings"

	"github.com/sokel-dev/sokel-official-plugins/gmail/schema"
)

// gmailMessage declares only the fields we use (Gmail's response is large, and declaring
// everything would obscure which fields we actually depend on).
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

// header fetches a message header (**case-insensitive**).
// Gmail returns "Subject", but "subject"/"SUBJECT" are equally valid in a forwarding chain
// (RFC 5322 specifies header names are case-insensitive). Comparing literally would make some
// messages' subjects come out inexplicably empty.
func header(p gmailPart, name string) string {
	for _, h := range p.Headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

// decodeBody: Gmail's body is **base64url** (using -_ instead of +/), and often has no padding.
// Decoding with standard base64 fails on content containing - or _; using RawURLEncoding on
// padded content also fails. Try both, and return empty if neither works — a missing body beats
// returning garbled bytes.
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

// extractBodies recursively extracts the body. Returns (plain text, HTML).
//
// Only recognizes **non-attachment** text/plain and text/html: an attachment can also be
// text/plain (e.g. a .txt file), and treating it as the body would turn a message with
// readme.txt attached into the contents of that txt file. The criterion is an empty filename —
// a named part is an attachment.
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

// extractAttachments recursively collects attachments.
//
// The criterion is "has an attachmentId," not "has a filename": an inline image (a cid:
// reference in the body) also has an attachmentId but may have no name, and it's genuinely a
// downloadable part. Conversely, a part with only a filename and no attachmentId has no bytes
// to fetch, and including it would just make someone click it and get an error.
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

// hasAttachments reports whether the message has attachments (the event only carries this
// boolean, not the list — the list requires calling "read message," and an event payload
// shouldn't carry a potentially long attachment list).
func hasAttachments(p gmailPart) bool { return len(extractAttachments(p)) > 0 }
