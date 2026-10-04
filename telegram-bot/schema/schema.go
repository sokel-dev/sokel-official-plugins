// Package schema declares telegram-bot's operation and event contracts.
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// AnswerCallbackQuery (migrated from the legacy contract)
type AnswerCallbackQuery struct{}

func (AnswerCallbackQuery) Meta() contract.Meta {
	return contract.Meta{ID: "answer_callback_query", Label: "回应按钮点击"}
}

func (AnswerCallbackQuery) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("callback_query_id").Label("回调 ID").Desc("来自 callback_query.id"),
		field.String("text").Label("提示文字").Desc("弹窗/toast 文本").Optional(),
		field.Bool("show_alert").Label("弹窗显示").Desc("true=模态弹窗，false=顶部 toast").Optional(),
	}
}

func (AnswerCallbackQuery) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("成功"),
		field.Any("result", "Bot API 的 result 随方法而变：可能是对象也可能是数组，形状由 Telegram 定义").Label("结果"),
	}
}

// Call (migrated from the legacy contract)
type Call struct{}

func (Call) Meta() contract.Meta {
	return contract.Meta{ID: "call", Label: "调用任意 Bot API"}
}

func (Call) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("method").Label("方法名").Desc("Bot API 方法，如 sendMessage / getChat / answerCallbackQuery"),
		field.Object("params", "该方法的参数对象，键名照 Telegram Bot API 文档；本插件不介入").Label("参数").Desc("该方法的参数对象（键名照 Bot API 文档）").Optional(),
	}
}

func (Call) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("成功"),
		field.Any("result", "Bot API 的 result 随方法而变：可能是对象也可能是数组，形状由 Telegram 定义").Label("结果").Desc("Bot API result 字段（对象/数组，按方法而定）"),
	}
}

// DeleteMessage (migrated from the legacy contract)
type DeleteMessage struct{}

func (DeleteMessage) Meta() contract.Meta {
	return contract.Meta{ID: "delete_message", Label: "删除消息"}
}

func (DeleteMessage) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("chat_id").Label("对话 ID"),
		field.Int("message_id").Label("消息 ID"),
	}
}

func (DeleteMessage) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("成功"),
		field.Any("result", "Bot API 的 result 随方法而变：可能是对象也可能是数组，形状由 Telegram 定义").Label("结果"),
	}
}

// DeleteWebhook (migrated from the legacy contract)
type DeleteWebhook struct{}

func (DeleteWebhook) Meta() contract.Meta {
	return contract.Meta{ID: "delete_webhook", Label: "删除 Webhook"}
}

func (DeleteWebhook) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("drop_pending_updates").Label("丢弃积压 update").Optional(),
	}
}

func (DeleteWebhook) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("成功"),
		field.Any("result", "Bot API 的 result 随方法而变：可能是对象也可能是数组，形状由 Telegram 定义").Label("结果"),
	}
}

// DownloadFile (migrated from the legacy contract)
type DownloadFile struct{}

func (DownloadFile) Meta() contract.Meta {
	return contract.Meta{ID: "download_file", Label: "下载文件"}
}

func (DownloadFile) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("file_id").Label("文件 ID").Desc("来自 message.photo/document 等的 file_id"),
	}
}

func (DownloadFile) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("file_path").Label("文件路径"),
		field.Int("size").Label("字节数"),
		field.String("base64").Label("文件内容(base64)").Desc("原始字节，≤20MB（Bot API 下载上限）"),
	}
}

// EditMessageText (migrated from the legacy contract)
type EditMessageText struct{}

func (EditMessageText) Meta() contract.Meta {
	return contract.Meta{ID: "edit_message_text", Label: "编辑消息文本"}
}

func (EditMessageText) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("chat_id").Label("对话 ID"),
		field.Int("message_id").Label("消息 ID"),
		field.String("text").Label("新文本"),
		field.Enum("parse_mode", field.Opt("MarkdownV2"), field.Opt("HTML"), field.Opt("Markdown")).Label("解析模式").Optional(),
		field.Object("reply_markup", "Telegram 的键盘/按钮对象（inline_keyboard 等），形状由 Bot API 定义").Label("键盘/按钮").Optional(),
	}
}

func (EditMessageText) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("成功"),
		field.Int("message_id").Label("消息 ID").Desc("新消息 id，供后续 edit/delete/reply 引用"),
		field.Any("result", "Bot API 的 result 随方法而变：可能是对象也可能是数组，形状由 Telegram 定义").Label("完整 Message 对象"),
	}
}

// ForwardMessage (migrated from the legacy contract)
type ForwardMessage struct{}

func (ForwardMessage) Meta() contract.Meta {
	return contract.Meta{ID: "forward_message", Label: "转发消息"}
}

func (ForwardMessage) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("chat_id").Label("目标对话 ID"),
		field.String("from_chat_id").Label("来源对话 ID"),
		field.Int("message_id").Label("消息 ID"),
	}
}

func (ForwardMessage) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("成功"),
		field.Int("message_id").Label("消息 ID").Desc("新消息 id，供后续 edit/delete/reply 引用"),
		field.Any("result", "Bot API 的 result 随方法而变：可能是对象也可能是数组，形状由 Telegram 定义").Label("完整 Message 对象"),
	}
}

// GetChat (migrated from the legacy contract)
type GetChat struct{}

func (GetChat) Meta() contract.Meta {
	return contract.Meta{ID: "get_chat", Label: "获取对话信息"}
}

func (GetChat) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("chat_id").Label("对话 ID"),
	}
}

func (GetChat) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("成功"),
		field.Any("result", "Bot API 的 result 随方法而变：可能是对象也可能是数组，形状由 Telegram 定义").Label("结果").Desc("Bot API result 字段（对象/数组，按方法而定）"),
	}
}

// GetMe (migrated from the legacy contract)
type GetMe struct{}

func (GetMe) Meta() contract.Meta {
	return contract.Meta{ID: "get_me", Label: "获取 bot 信息"}
}

func (GetMe) Inputs() []contract.FieldSpec {
	return nil
}

func (GetMe) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("成功"),
		field.Any("result", "Bot API 的 result 随方法而变：可能是对象也可能是数组，形状由 Telegram 定义").Label("结果").Desc("Bot API result 字段（对象/数组，按方法而定）"),
	}
}

// GetWebhookInfo (migrated from the legacy contract)
type GetWebhookInfo struct{}

func (GetWebhookInfo) Meta() contract.Meta {
	return contract.Meta{ID: "get_webhook_info", Label: "查看 Webhook 状态"}
}

func (GetWebhookInfo) Inputs() []contract.FieldSpec {
	return nil
}

func (GetWebhookInfo) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("成功"),
		field.Any("result", "Bot API 的 result 随方法而变：可能是对象也可能是数组，形状由 Telegram 定义").Label("结果").Desc("Bot API result 字段（对象/数组，按方法而定）"),
	}
}

// SendChatAction (migrated from the legacy contract)
type SendChatAction struct{}

func (SendChatAction) Meta() contract.Meta {
	return contract.Meta{ID: "send_chat_action", Label: "发送输入状态"}
}

func (SendChatAction) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("chat_id").Label("对话 ID"),
		field.Enum("action", field.Opt("typing"), field.Opt("upload_photo"), field.Opt("record_video"), field.Opt("upload_document"), field.Opt("choose_sticker")).Label("动作").Desc("对方看到「正在输入…」等").Default("typing"),
	}
}

func (SendChatAction) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("成功"),
		field.Any("result", "Bot API 的 result 随方法而变：可能是对象也可能是数组，形状由 Telegram 定义").Label("结果"),
	}
}

// SendDocument (migrated from the legacy contract)
type SendDocument struct{}

func (SendDocument) Meta() contract.Meta {
	return contract.Meta{ID: "send_document", Label: "发送文件"}
}

func (SendDocument) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("chat_id").Label("对话 ID"),
		field.String("document").Label("文件").Desc("HTTP URL 或 file_id"),
		field.String("caption").Label("说明文字").Optional(),
	}
}

func (SendDocument) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("成功"),
		field.Int("message_id").Label("消息 ID").Desc("新消息 id，供后续 edit/delete/reply 引用"),
		field.Any("result", "Bot API 的 result 随方法而变：可能是对象也可能是数组，形状由 Telegram 定义").Label("完整 Message 对象"),
	}
}

// SendMessage (migrated from the legacy contract)
type SendMessage struct{}

func (SendMessage) Meta() contract.Meta {
	return contract.Meta{ID: "send_message", Label: "发送消息"}
}

func (SendMessage) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("chat_id").Label("对话 ID").Desc("数字 id 或 @channelusername"),
		field.String("text").Label("文本").Desc("支持 Markdown/HTML（见 parse_mode）"),
		field.Enum("parse_mode", field.Opt("MarkdownV2"), field.Opt("HTML"), field.Opt("Markdown")).Label("解析模式").Optional(),
		field.Int("reply_to_message_id").Label("回复的消息 ID").Optional(),
		field.Object("reply_markup", "Telegram 的键盘/按钮对象（inline_keyboard 等），形状由 Bot API 定义").Label("键盘/按钮").Desc("inline_keyboard 等 reply_markup 对象").Optional(),
		field.Bool("disable_web_page_preview").Label("禁用链接预览").Optional(),
	}
}

func (SendMessage) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("成功"),
		field.Int("message_id").Label("消息 ID").Desc("新消息 id，供后续 edit/delete/reply 引用"),
		field.Any("result", "Bot API 的 result 随方法而变：可能是对象也可能是数组，形状由 Telegram 定义").Label("完整 Message 对象"),
	}
}

// SendPhoto (migrated from the legacy contract)
type SendPhoto struct{}

func (SendPhoto) Meta() contract.Meta {
	return contract.Meta{ID: "send_photo", Label: "发送图片"}
}

func (SendPhoto) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("chat_id").Label("对话 ID"),
		field.String("photo").Label("图片").Desc("HTTP URL 或 file_id"),
		field.String("caption").Label("说明文字").Optional(),
		field.Enum("parse_mode", field.Opt("MarkdownV2"), field.Opt("HTML"), field.Opt("Markdown")).Label("解析模式").Optional(),
	}
}

func (SendPhoto) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("成功"),
		field.Int("message_id").Label("消息 ID").Desc("新消息 id，供后续 edit/delete/reply 引用"),
		field.Any("result", "Bot API 的 result 随方法而变：可能是对象也可能是数组，形状由 Telegram 定义").Label("完整 Message 对象"),
	}
}

// SetMyCommands (migrated from the legacy contract)
type SetMyCommands struct{}

func (SetMyCommands) Meta() contract.Meta {
	return contract.Meta{ID: "set_my_commands", Label: "设置命令菜单"}
}

func (SetMyCommands) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("commands", []BotCommand{}).Label("命令列表"),
	}
}

func (SetMyCommands) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("成功"),
		field.Any("result", "Bot API 的 result 随方法而变：可能是对象也可能是数组，形状由 Telegram 定义").Label("结果"),
	}
}

// SetWebhook (migrated from the legacy contract)
type SetWebhook struct{}

func (SetWebhook) Meta() contract.Meta {
	return contract.Meta{ID: "set_webhook", Label: "设置 Webhook"}
}

func (SetWebhook) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("url").Label("Webhook URL").Desc("HTTPS 地址，Telegram 将 update POST 到此"),
		field.String("secret_token").Label("secret_token").Desc("1-256 字符；Telegram 每次投递带 X-Telegram-Bot-Api-Secret-Token 头供校验").Optional(),
		field.Array("allowed_updates", []string{}).Label("订阅事件类型").Desc("如 message,callback_query；空=默认集").Optional(),
		field.Int("max_connections").Label("最大并发投递").Desc("1-100，默认 40").Optional(),
	}
}

func (SetWebhook) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("成功"),
		field.Any("result", "Bot API 的 result 随方法而变：可能是对象也可能是数组，形状由 Telegram 定义").Label("结果"),
	}
}

// BotCommand is a single bot command. It used to be declared as unstructured json, but the shape
// was actually fixed all along — the reverse-migration tool flagged it as "needs a reason", which
// is exactly the point: a human has to decide here whether to add structure or write the reason.
type BotCommand struct {
	Command     string `sokel:"command" label:"命令" desc:"不带斜杠，如 start"`
	Description string `sokel:"description" label:"说明"`
}

// —— Credential health check ——

// HealthCheck checks this credential: fires a single getMe.
//
// The operation id must be health_check — the "check" button on the platform's credential page
// relies on it to decide whether this plugin can be health-checked. When the token is wrong, this
// returns ok=false + message rather than an error: the platform treats an error as "this plugin
// can't run its health check", and ok=false as "the check concluded the plugin is unavailable" —
// the latter is what we want to say here.
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "检查凭证", TimeoutSec: 30,
		Desc: "打一次 getMe，报 bot token 是否有效以及它是哪个 bot（不发消息、不碰任何对话）"}
}

func (HealthCheck) Inputs() []contract.FieldSpec { return nil }

func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("可用"),
		// The bot username is the only thing that can catch a "token copied from the wrong bot"
		// mistake — people often have both a test bot and a production bot whose tokens look identical.
		field.String("username").Label("bot 用户名").Optional(),
		field.String("message").Label("说明"),
	}
}

// —— Events ——
//
// All four events share chat_id (see Events.CommonFields): the platform flattens it to the top
// level of the trigger input, so every branch shares the same variable — the reply node just binds
// it, no need to fetch it separately per branch.

func messageFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Int("chat_id").Label("对话 ID"),
		field.Int("user_id").Label("发送者 ID"),
		field.String("username").Label("发送者用户名").Optional(),
		field.Text("text").Label("文本").Optional(),
		field.Int("message_id").Label("消息 ID"),
		field.Any("raw", "Telegram 的原始 update，形状由 Bot API 定义且随版本增补").Label("原始 update"),
	}
}

// MessageReceived: a message was received.
type MessageReceived struct{}

func (MessageReceived) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "message", Label: "收到消息"}
}
func (MessageReceived) Fields() []contract.FieldSpec { return messageFields() }

// MessageEdited: a message was edited.
type MessageEdited struct{}

func (MessageEdited) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "edited_message", Label: "消息被编辑"}
}
func (MessageEdited) Fields() []contract.FieldSpec { return messageFields() }

// CallbackQuery: a button was tapped.
type CallbackQuery struct{}

func (CallbackQuery) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "callback_query", Label: "按钮点击"}
}
func (CallbackQuery) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Int("chat_id").Label("对话 ID"),
		field.Int("user_id").Label("点击者 ID"),
		field.String("callback_id").Label("回调 ID").Desc("用于 answer_callback_query"),
		field.String("callback_data").Label("按钮数据").Optional(),
		field.Int("message_id").Label("消息 ID"),
		field.Any("raw", "Telegram 的原始 update，形状由 Bot API 定义且随版本增补").Label("原始 update"),
	}
}

// MyChatMember: the bot's membership status changed in some chat (added/removed/permissions changed).
type MyChatMember struct{}

func (MyChatMember) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "my_chat_member", Label: "bot 成员状态变化"}
}
func (MyChatMember) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Int("chat_id").Label("对话 ID"),
		field.Int("user_id").Label("用户 ID"),
		field.Any("raw", "Telegram 的原始 update，形状由 Bot API 定义且随版本增补").Label("原始 update"),
	}
}

// Events declares the shared fields.
type Events struct{}

func (Events) CommonFields() []string { return []string{"chat_id"} }

// —— Credential ——

// Credential: the Telegram bot credential. bot_token looks like "123456:ABC-DEF...", the only secret.
type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Secret("bot_token").Label("Bot Token").
			Desc("@BotFather 创建 bot 后给的 token（123456:ABC…）"),
	}
}
