// Package schema declares the gmail plugin's operation and event contracts.
//
// Read-only for the first release: list messages / read a single message / get attachments.
// Adding "mark as read" would need gmail.modify (which already includes read) — a broader
// permission; the more restricted scopes requested, the harder Google's security review gets,
// so add it when actually needed.
//
// The credential goes through the platform's auth_google_gmail (the user clicks "consent" once
// -> refresh_token). The plugin **never holds** the client_secret, and never handles the
// refresh_token: all the platform injects is a freshly-exchanged access_token (Authorization:
// Bearer).
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/auth"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// ListMessages lists messages matching a query.
type ListMessages struct{}

func (ListMessages) Meta() contract.Meta {
	return contract.Meta{ID: "gmail_list", Label: "列邮件"}
}

func (ListMessages) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("query").Label("搜索条件").
			Desc("与 Gmail 搜索框同一套语法，如 is:unread from:boss@x.com newer_than:2d").Optional(),
		field.String("label_ids").Label("标签").Desc("逗号分隔，如 INBOX,UNREAD").Optional(),
		field.Int("max_results").Label("最多几封").Desc("默认 20，上限 100").Optional(),
		// The list endpoint only returns ids; the body has to be fetched per message —
		// listing 500 and then fetching each one's detail is a classic way to burn through
		// the quota.
		field.Bool("with_detail").Label("同时取正文").
			Desc("勾选后逐封拉详情（慢且更耗配额）；只要 id 就别勾").Optional(),
	}
}

func (ListMessages) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		// The element shape must be given: field.Array's second argument is a **shape**,
		// not description text — passing a string silently produces an unstructured array,
		// so downstream getting messages[0] has no idea what's inside, the variable picker
		// can't expand it, and references get no validation.
		field.Array("messages", []MessageItem{}).
			Desc("邮件列表；with_detail=false 时每项只有 id/thread_id 有值").Label("邮件"),
		field.Int("count").Label("条数"),
		field.String("next_page_token").Label("下一页令牌").Optional(),
	}
}

// GetMessage reads a single message.
type GetMessage struct{}

func (GetMessage) Meta() contract.Meta {
	return contract.Meta{ID: "gmail_get", Label: "读邮件"}
}

func (GetMessage) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("message_id").Label("邮件 ID").Desc("来自「列邮件」或新邮件事件"),
	}
}

func (GetMessage) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("id").Label("邮件 ID"),
		field.String("thread_id").Label("会话 ID"),
		field.String("subject").Label("主题").Optional(),
		field.String("from").Label("发件人").Optional(),
		field.String("to").Label("收件人").Optional(),
		field.String("date").Label("日期").Optional(),
		// Both body formats are provided: plain text is suited for feeding a model, HTML
		// for rendering as-is. Gmail often only has one of the two.
		field.Text("text").Label("正文（纯文本）").Optional(),
		field.Text("html").Label("正文（HTML）").Optional(),
		field.String("snippet").Label("摘要").Optional(),
		field.Array("attachments", []AttachmentRef{}).
			Desc("附件清单；字节要用「取附件」再拉").Label("附件"),
		field.Array("label_ids", []string{}).Desc("该邮件的标签 id 列表").Label("标签").Optional(),
	}
}

// GetAttachment fetches the attachment bytes and lands them in the platform's file layer.
type GetAttachment struct{}

func (GetAttachment) Meta() contract.Meta {
	return contract.Meta{ID: "gmail_attachment", Label: "取附件"}
}

func (GetAttachment) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("message_id").Label("邮件 ID"),
		field.String("attachment_id").Label("附件 ID").Desc("来自「读邮件」输出的 attachments"),
		field.String("filename").Label("文件名").Desc("落盘用；留空则用附件自带的名字").Optional(),
	}
}

func (GetAttachment) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		// Bytes don't travel through the contract: base64-encoding a tens-of-MB attachment
		// into a single call is bound to blow up (see playbook §5.0.2). The plugin moves
		// the bytes into the file layer via a chunked channel; only a reference is
		// returned here.
		field.File("file").Label("附件文件"),
	}
}

// —— Credential ——

// Credential is this plugin's credential contract.
//
// None of the three fields need to be **filled in by hand**: clicking "authorize" on the
// credential row goes through a Google consent screen once, the platform stores the
// refresh_token, and exchanges a fresh access_token right before each call; history_id is the
// cursor the event source writes back itself.
type Credential struct{}

// AuthMeta: the credential goes through Google OAuth; the scope is **declared right here**, not
// hardcoded by the platform — adding another Google-service plugin requires zero changes to the
// platform, and this keeps least privilege (this plugin's credential can't touch Drive).
// The declaration itself is what makes the "authorize" button appear on the credential row;
// the whole authentication flow is answered by the platform (which is why auth.OAuth has no steps).
func (Credential) AuthMeta() contract.AuthMeta {
	return auth.OAuth("google", "https://www.googleapis.com/auth/gmail.readonly")
}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Secret("access_token").Label("访问令牌（平台注入）").
			Desc("授权后由平台自动换取，勿手填").Optional(),
		field.Secret("refresh_token").Label("刷新令牌（授权获取）").
			Desc("点凭证行的「授权」获取，勿手填").Optional(),
		field.Text("history_id").Label("轮询游标").
			Desc("新邮件事件源自动维护，勿手填").Optional(),
	}
}

// —— Credential health check ——

// HealthCheck checks this credential by making one call to users.getProfile.
//
// The operation id must be health_check — the platform's "check" button on the credential page
// relies on this to decide whether this plugin supports a health check. When authorization has
// expired, this returns ok=false + message, not an error: the platform treats an error as "this
// plugin can't run a health check" and ok=false as "the health check concluded it's
// unavailable" — the latter is what needs to be said here.
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "检查凭证", TimeoutSec: 30,
		Desc: "打一次 Gmail 的 users.getProfile，报授权是否还有效、授的是哪个邮箱"}
}

func (HealthCheck) Inputs() []contract.FieldSpec { return nil }

func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("可用"),
		// The email address is the only thing that can catch "authorization was granted to
		// the wrong Google account": just reporting "connection OK" would leave this kind
		// of mistake hidden until a workflow fails to find the expected message.
		field.String("email").Label("邮箱").Optional(),
		field.Int("messages_total").Label("邮件总数").Optional(),
		field.String("message").Label("说明"),
	}
}

// —— Events ——

// MessageReceived fires when a new message arrives.
//
// The fields are just enough to decide "should this run, and what should it do" — checking the
// sender/subject doesn't require another call to read the message. The body and attachments
// aren't in the event — a message can be several MB, and the same message can fan out to
// multiple workflows; carrying the body in each one would mean duplicating the message content
// N times into event payloads and run records. Chain a "read message" step if the body is needed.
type MessageReceived struct{}

func (MessageReceived) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "message_received", Label: "收到新邮件"}
}

func (MessageReceived) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("message_id").Label("邮件 ID"),
		field.String("thread_id").Label("会话 ID"),
		field.String("subject").Label("主题").Optional(),
		field.String("from").Label("发件人").Optional(),
		field.String("to").Label("收件人").Optional(),
		field.String("date").Label("日期").Optional(),
		field.String("snippet").Label("摘要").Desc("Gmail 给的短摘要，判断要不要细读用").Optional(),
		field.Bool("has_attachments").Label("有附件").Optional(),
		field.Array("label_ids", []string{}).
			Desc("该邮件的标签 id 列表（如 INBOX/UNREAD/CATEGORY_PROMOTIONS）").Label("标签").Optional(),
	}
}

// —— Element shapes used in outputs ——

// AttachmentRef is one entry in the attachment list (the bytes aren't here; fetch them with "get attachment").
type AttachmentRef struct {
	AttachmentID string `sokel:"attachment_id" label:"附件 id"`
	Filename     string `sokel:"filename" label:"文件名"`
	MimeType     string `sokel:"mime_type" label:"类型"`
	Size         int    `sokel:"size" label:"字节数"`
}

// MessageItem is one message in a list.
//
// With with_detail=false only id/thread_id have values and the rest are empty — but all fields
// are still declared: an output in the contract is a **hint** for downstream consumers (they
// expand variables and write references based on it), and hiding a field because "it might not
// have a value this time" would leave people guessing what's inside a raw JSON blob.
type MessageItem struct {
	ID          string          `sokel:"id" label:"邮件 id"`
	ThreadID    string          `sokel:"thread_id" label:"会话 id"`
	Subject     string          `sokel:"subject,optional" label:"主题" desc:"需 with_detail=true；否则为空"`
	From        string          `sokel:"from,optional" label:"发件人" desc:"需 with_detail=true；否则为空"`
	To          string          `sokel:"to,optional" label:"收件人" desc:"需 with_detail=true；否则为空"`
	Date        string          `sokel:"date,optional" label:"日期" desc:"需 with_detail=true；否则为空"`
	Text        string          `sokel:"text,optional" label:"正文(纯文本)" desc:"需 with_detail=true；否则为空"`
	HTML        string          `sokel:"html,optional" label:"正文(HTML)" desc:"需 with_detail=true；否则为空"`
	Snippet     string          `sokel:"snippet,optional" label:"摘要" desc:"需 with_detail=true；否则为空"`
	Attachments []AttachmentRef `sokel:"attachments,optional" label:"附件" desc:"需 with_detail=true；否则为空"`
	LabelIDs    []string        `sokel:"label_ids,optional" label:"标签" desc:"需 with_detail=true；否则为空"`
	// Error holds the reason when fetching this message's detail failed. A whole batch
	// doesn't fail just because one message was deleted, but downstream needs to be able
	// to tell this entry is incomplete.
	Error string `sokel:"error,optional" label:"错误"`
}
