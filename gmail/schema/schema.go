// Package schema 声明 gmail 插件的操作与事件契约。
//
// 首发只读：列邮件 / 读单封 / 取附件。加「标已读」要 gmail.modify（它已包含读），
// 那是更大的权限——restricted 作用域要得越多，Google 的安全评估越难过，等真需要再加。
//
// 凭证走平台的 auth_google_gmail（用户点一次「同意」→ refresh_token）。
// 插件**不持有** client_secret，也不经手 refresh_token：平台注入的只是一个现换的
// access_token（Authorization: Bearer）。
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/auth"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// ListMessages 按查询条件列邮件。
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
		// 列表接口只回 id，正文要再逐封取——一次列 500 封再逐封拉是把配额烧光的经典写法。
		field.Bool("with_detail").Label("同时取正文").
			Desc("勾选后逐封拉详情（慢且更耗配额）；只要 id 就别勾").Optional(),
	}
}

func (ListMessages) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		// 元素形状必须给出来：field.Array 的第二个参数是**形状**不是描述文字，
		// 传字符串会静默产出一个无结构数组——下游拿到 messages[0] 就不知道里面有什么，
		// 变量选择器展不开、引用也没有校验。
		field.Array("messages", []MessageItem{}).
			Desc("邮件列表；with_detail=false 时每项只有 id/thread_id 有值").Label("邮件"),
		field.Int("count").Label("条数"),
		field.String("next_page_token").Label("下一页令牌").Optional(),
	}
}

// GetMessage 读一封邮件。
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
		// 正文两种都给：纯文本适合喂模型，HTML 适合原样展示。Gmail 常常只有其中一种。
		field.Text("text").Label("正文（纯文本）").Optional(),
		field.Text("html").Label("正文（HTML）").Optional(),
		field.String("snippet").Label("摘要").Optional(),
		field.Array("attachments", []AttachmentRef{}).
			Desc("附件清单；字节要用「取附件」再拉").Label("附件"),
		field.Array("label_ids", []string{}).Desc("该邮件的标签 id 列表").Label("标签").Optional(),
	}
}

// GetAttachment 取附件字节，落进平台文件层。
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
		// 字节不走契约：几十 MB 的附件 base64 塞进一次调用必炸（见 playbook §5.0.2）。
		// 插件用分块通道把字节搬进文件层，这里只回引用。
		field.File("file").Label("附件文件"),
	}
}

// —— 凭证 ——

// Credential 本插件的凭证契约。
//
// 三个字段都**不用手填**：点凭证行的「授权」走一次 Google 同意页，平台落 refresh_token，
// 调用前现换 access_token 塞进来；history_id 是事件源自己写回的游标。
type Credential struct{}

// AuthMeta 凭证走 Google OAuth：作用域**在这儿声明**，平台不写死——
// 加别的 Google 服务插件时平台一行都不用改，且最小权限（本插件的凭证碰不到 Drive）。
// 声明本身就让凭证行上出现「授权」按钮；认证全程由平台代答（auth.OAuth 因此没有步骤）。
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

// —— 凭证体检 ——

// HealthCheck 体检这条凭证：打一次 users.getProfile。
//
// 操作 id 必须是 health_check——平台凭证页的「检查」按钮据此判断这个插件能不能验活。
// 授权失效时返回 ok=false + message 而不是 error：平台把 error 当「这个插件没法体检」，
// 把 ok=false 当「体检结论是不可用」，后者才是这里要说的话。
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "检查凭证", TimeoutSec: 30,
		Desc: "打一次 Gmail 的 users.getProfile，报授权是否还有效、授的是哪个邮箱"}
}

func (HealthCheck) Inputs() []contract.FieldSpec { return nil }

func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("可用"),
		// 邮箱地址是唯一能戳穿「授权授到了另一个 Google 账号」的东西：
		// 只报「连得上」的话，这种错要等工作流拉不到那封信才暴露。
		field.String("email").Label("邮箱").Optional(),
		field.Int("messages_total").Label("邮件总数").Optional(),
		field.String("message").Label("说明"),
	}
}

// —— 事件 ——

// MessageReceived 收到新邮件。
//
// 字段是「够用来决定要不要跑、以及跑什么」的那一层：判断发件人/主题不必再调一次读邮件。
// 正文与附件不进事件——一封邮件可能几 MB，而同一封会扇出给多个工作流，
// 每个都带一份正文等于把邮件内容复制 N 遍塞进事件载荷与运行记录。要正文就接一个「读邮件」。
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

// —— 输出里的元素形状 ——

// AttachmentRef：附件清单里的一项（字节不在这里，要用「取附件」再拉）。
type AttachmentRef struct {
	AttachmentID string `sokel:"attachment_id" label:"附件 id"`
	Filename     string `sokel:"filename" label:"文件名"`
	MimeType     string `sokel:"mime_type" label:"类型"`
	Size         int    `sokel:"size" label:"字节数"`
}

// MessageItem：列表里的一封邮件。
//
// with_detail=false 时只有 id/thread_id 有值，其余为空——但字段仍然全部声明：
// 契约里的输出是给下游看的**提示**（下游据此展开变量、写引用），
// 按「这次可能没有」把字段藏起来，等于让人对着一个 json 猜里面有什么。
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
	// Error：取该封详情失败时的原因。整批不因一封信被删就失败，但下游要能看出这条是残缺的。
	Error string `sokel:"error,optional" label:"错误"`
}
