// Package schema declares the feishu plugin's operation, event, and credential contracts.
//
// Scope: the full-capability side of a Feishu **custom app** — messages/cards/user lookup/chat
// management/docs/bitable/drive + a long-connection event source (message received / card
// button clicked / bot added to a chat -> starts a workflow). The group "custom bot webhook" is
// a separate plugin (feishu-webhook): the credential shape and authorization scope are
// completely different (one group webhook vs. one enterprise app), so they aren't pooled
// together in one credential pool.
//
// The layering follows the same decision as telegram-bot: **typed for common cases + call as a
// fallback**. Typed operations give the canvas a "fill in fields" experience; call takes
// method+path+body and covers the entire Open Platform API, so Feishu adding an endpoint
// requires zero code changes here.
//
// Two Feishu-specific conventions that the operation design revolves around:
//
//   - **receive_id is typed.** The same "who to send to" is interpreted via receive_id_type as
//     one of open_id / chat_id / user_id / email / union_id — so every send-type operation
//     carries a type dropdown instead of making the user guess "what kind of id is this string."
//   - **Images/files must be exchanged for a key first.** Feishu doesn't accept raw bytes sent
//     directly; they must first be uploaded to exchange for an image_key/file_key, then
//     referenced. send_image/send_file wrap both steps internally; the standalone upload_*
//     operations are for an "upload once, send multiple times" flow.
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// receiveIDFields is the "who to send to" pair shared by all send-type operations.
func receiveIDFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("receive_id").Label("接收方 ID").
			Desc("群聊填 chat_id（oc_ 开头）；单聊填对方 open_id（ou_ 开头）或 email"),
		field.Enum("receive_id_type",
			field.Opt("chat_id", "群聊 chat_id（oc_…）"),
			field.Opt("open_id", "用户 open_id（ou_…）"),
			field.Opt("user_id", "用户 user_id"),
			field.Opt("union_id", "用户 union_id"),
			field.Opt("email", "用户邮箱")).
			Label("ID 类型").Default("chat_id"),
	}
}

// sentMessageOutputs is the uniform output shared by all send-type operations.
func sentMessageOutputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("message_id").Label("消息 ID").Desc("om_ 开头；回复/撤回都认它"),
		field.String("chat_id").Label("所在对话 ID"),
	}
}

// SendText sends plain text.
type SendText struct{}

func (SendText) Meta() contract.Meta {
	return contract.Meta{ID: "send_text", Label: "发文本消息",
		Desc: "发一条纯文本；可在文本里用 <at user_id=\"ou_xxx\"></at> @人，或 <at user_id=\"all\"></at> @所有人"}
}

func (SendText) Inputs() []contract.FieldSpec {
	return append(receiveIDFields(),
		field.Text("text").Label("文本").Desc("支持 <at user_id=\"ou_…\"></at> @人"),
	)
}

func (SendText) Outputs() []contract.FieldSpec { return sentMessageOutputs() }

// SendMarkdown sends Markdown (internally wrapped into a single-element card).
type SendMarkdown struct{}

func (SendMarkdown) Meta() contract.Meta {
	return contract.Meta{ID: "send_markdown", Label: "发 Markdown",
		Desc: "以卡片形式渲染 Markdown（加粗/链接/列表/代码块/分割线）。工作流产出的报告用它最顺手"}
}

func (SendMarkdown) Inputs() []contract.FieldSpec {
	return append(receiveIDFields(),
		field.String("title").Label("标题").Desc("卡片头；留空则不带头").Optional(),
		field.Enum("title_color",
			field.Opt("blue", "蓝"), field.Opt("wathet", "浅蓝"), field.Opt("turquoise", "青"),
			field.Opt("green", "绿"), field.Opt("yellow", "黄"), field.Opt("orange", "橙"),
			field.Opt("red", "红"), field.Opt("carmine", "洋红"), field.Opt("violet", "紫"),
			field.Opt("purple", "紫罗兰"), field.Opt("grey", "灰")).
			Label("标题色").Default("blue").Optional(),
		field.Text("markdown").Label("Markdown 正文").
			Desc("飞书 lark_md 方言：**加粗**、[链接](url)、有序/无序列表、`代码`、--- 分割线"),
	)
}

func (SendMarkdown) Outputs() []contract.FieldSpec { return sentMessageOutputs() }

// SendCard sends an interactive card (full JSON).
type SendCard struct{}

func (SendCard) Meta() contract.Meta {
	return contract.Meta{ID: "send_card", Label: "发交互卡片",
		Desc: "发完整的交互卡片 JSON（按钮/下拉/表单）。按钮点击经「卡片按钮点击」事件回到工作流"}
}

func (SendCard) Inputs() []contract.FieldSpec {
	return append(receiveIDFields(),
		field.Object("card", "飞书卡片 JSON（card 2.0 的 schema/body 或 1.0 的 config/elements），形状由飞书卡片文档定义").
			Label("卡片 JSON").Desc("在飞书「卡片搭建工具」里拼好后把 JSON 粘进来"),
	)
}

func (SendCard) Outputs() []contract.FieldSpec { return sentMessageOutputs() }

// SendImage sends an image (upload-for-key + send, two steps wrapped into one).
type SendImage struct{}

func (SendImage) Meta() contract.Meta {
	return contract.Meta{ID: "send_image", Label: "发图片", TimeoutSec: 120,
		Desc: "上传图片换 image_key 并发送，两步包成一步"}
}

func (SendImage) Inputs() []contract.FieldSpec {
	return append(receiveIDFields(),
		field.File("image").Label("图片").Desc("jpg/png/webp/gif/bmp，≤10MB"),
	)
}

func (SendImage) Outputs() []contract.FieldSpec {
	return append(sentMessageOutputs(),
		field.String("image_key").Label("图片 key").Desc("可存下来复用，免得重复上传"))
}

// SendFile sends a file.
type SendFile struct{}

func (SendFile) Meta() contract.Meta {
	return contract.Meta{ID: "send_file", Label: "发文件", TimeoutSec: 300,
		Desc: "上传文件换 file_key 并发送，两步包成一步"}
}

func (SendFile) Inputs() []contract.FieldSpec {
	return append(receiveIDFields(),
		field.File("file").Label("文件").Desc("任意类型，≤30MB（更大走网盘上传）"),
	)
}

func (SendFile) Outputs() []contract.FieldSpec {
	return append(sentMessageOutputs(),
		field.String("file_key").Label("文件 key"))
}

// ReplyMessage replies to a specific message (the most common op in workflows triggered by a
// message event).
type ReplyMessage struct{}

func (ReplyMessage) Meta() contract.Meta {
	return contract.Meta{ID: "reply_message", Label: "回复消息",
		Desc: "在会话里回复某条消息（被回复的消息上方有引用）。message_id 通常来自「收到消息」事件"}
}

func (ReplyMessage) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("message_id").Label("消息 ID").Desc("om_ 开头；来自事件或发送操作的产出"),
		field.Text("text").Label("文本").Desc("与 markdown 二选一；都填时用 markdown").Optional(),
		field.Text("markdown").Label("Markdown").Desc("以卡片渲染").Optional(),
	}
}

func (ReplyMessage) Outputs() []contract.FieldSpec { return sentMessageOutputs() }

// RecallMessage recalls a message.
type RecallMessage struct{}

func (RecallMessage) Meta() contract.Meta {
	return contract.Meta{ID: "recall_message", Label: "撤回消息",
		Desc: "撤回 bot 自己发的消息（发出 24 小时内）"}
}

func (RecallMessage) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("message_id").Label("消息 ID")}
}

func (RecallMessage) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("成功")}
}

// UploadImage only uploads, doesn't send.
type UploadImage struct{}

func (UploadImage) Meta() contract.Meta {
	return contract.Meta{ID: "upload_image", Label: "上传图片", TimeoutSec: 120,
		Desc: "上传图片换 image_key（发消息/卡片里引用）。一次上传可多次发送"}
}

func (UploadImage) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.File("image").Label("图片")}
}

func (UploadImage) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("image_key").Label("图片 key")}
}

// UploadFile only uploads, doesn't send.
type UploadFile struct{}

func (UploadFile) Meta() contract.Meta {
	return contract.Meta{ID: "upload_file", Label: "上传文件", TimeoutSec: 300,
		Desc: "上传文件换 file_key（发消息里引用）"}
}

func (UploadFile) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.File("file").Label("文件")}
}

func (UploadFile) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("file_key").Label("文件 key")}
}

// GetUser looks up an open_id by email/mobile number — a required hop before DMing a user.
type GetUser struct{}

func (GetUser) Meta() contract.Meta {
	return contract.Meta{ID: "get_user", Label: "查用户",
		Desc: "按邮箱或手机号换 open_id（发私信前必经的一跳）。需要「通过手机号或邮箱获取用户 ID」权限"}
}

func (GetUser) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("email").Label("邮箱").Desc("与手机号至少填一个").Optional(),
		field.String("mobile").Label("手机号").Desc("带国家码，如 +8613800138000").Optional(),
	}
}

func (GetUser) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("open_id").Label("open_id").Desc("ou_ 开头；发消息用它"),
		field.String("user_id").Label("user_id").Desc("企业内的用户 ID；未授权时为空").Optional(),
	}
}

// ListChats lists chats the bot belongs to.
type ListChats struct{}

func (ListChats) Meta() contract.Meta {
	return contract.Meta{ID: "list_chats", Label: "群列表",
		Desc: "列出 bot 已加入的群（拿 chat_id 用于发消息）"}
}

func (ListChats) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("page_token").Label("翻页标记").Desc("上一批返回的 page_token").Optional(),
	}
}

func (ListChats) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("chats", []Chat{}).Label("群列表"),
		field.String("page_token").Label("翻页标记").Desc("还有下一页时非空"),
		field.Bool("has_more").Label("还有更多"),
	}
}

// Chat is one chat.
type Chat struct {
	ChatID      string `sokel:"chat_id" label:"群 ID"`
	Name        string `sokel:"name" label:"群名"`
	Description string `sokel:"description,optional" label:"群描述"`
	OwnerID     string `sokel:"owner_id,optional" label:"群主 open_id"`
	External    bool   `sokel:"external,optional" label:"是否外部群"`
}

// CreateChat creates a chat.
type CreateChat struct{}

func (CreateChat) Meta() contract.Meta {
	return contract.Meta{ID: "create_chat", Label: "建群",
		Desc: "创建群聊并拉入成员，bot 自动入群"}
}

func (CreateChat) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("name").Label("群名"),
		field.String("description").Label("群描述").Optional(),
		field.Array("user_open_ids", []string{}).Label("成员 open_id 列表").
			Desc("ou_ 开头；可留空建空群").Optional(),
	}
}

func (CreateChat) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("chat_id").Label("群 ID")}
}

// AddChatMembers adds members to a chat.
type AddChatMembers struct{}

func (AddChatMembers) Meta() contract.Meta {
	return contract.Meta{ID: "add_chat_members", Label: "拉人进群",
		Desc: "把用户拉进 bot 所在的群（bot 需有拉人权限）"}
}

func (AddChatMembers) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("chat_id").Label("群 ID"),
		field.Array("user_open_ids", []string{}).Label("成员 open_id 列表"),
	}
}

func (AddChatMembers) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("invalid_ids", []string{}).Label("未拉进的 ID").Desc("不存在/不可见的用户；全成功时为空"),
	}
}

// —— Docs (docx) ——

// DocxCreate creates a doc.
type DocxCreate struct{}

func (DocxCreate) Meta() contract.Meta {
	return contract.Meta{ID: "docx_create", Label: "创建云文档",
		Desc: "新建一篇云文档（docx），可带初始 Markdown 内容。需要 docx:document 权限"}
}

func (DocxCreate) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("title").Label("标题"),
		field.String("folder_token").Label("文件夹 token").
			Desc("放进哪个文件夹（网盘 URL 里 folder/ 后那串）。留空放在应用自己的空间——**用户在网盘里看不到**，所以通常要填").Optional(),
		field.Text("markdown").Label("初始内容").
			Desc("Markdown：# 标题 / 列表 / 代码块 / 引用会转成对应文档块，其余按段落").Optional(),
	}
}

func (DocxCreate) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("document_id").Label("文档 ID"),
		field.String("url").Label("文档链接"),
	}
}

// DocxAppend appends content to a doc.
type DocxAppend struct{}

func (DocxAppend) Meta() contract.Meta {
	return contract.Meta{ID: "docx_append", Label: "追加到云文档",
		Desc: "在文档末尾追加 Markdown 内容（日报流水、持续记录类场景）"}
}

func (DocxAppend) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("document_id").Label("文档 ID").Desc("docx_create 的产出，或文档 URL 里 docx/ 后那串"),
		field.Text("markdown").Label("内容").Desc("Markdown，转换规则同 docx_create"),
	}
}

func (DocxAppend) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Int("block_count").Label("追加的块数")}
}

// —— Bitable ——

// bitableLoc is the location pair shared by all Bitable operations.
func bitableLoc() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("app_token").Label("多维表格 token").
			Desc("表格 URL 里 base/ 后那串（bascn…）"),
		field.String("table_id").Label("数据表 ID").
			Desc("URL 里 table= 后那串（tbl…）"),
	}
}

// BitableListRecords queries records.
type BitableListRecords struct{}

func (BitableListRecords) Meta() contract.Meta {
	return contract.Meta{ID: "bitable_list_records", Label: "多维表格·查记录",
		Desc: "按条件查记录。需要 bitable:app 权限，且把多维表格分享给应用（添加为协作者）"}
}

func (BitableListRecords) Inputs() []contract.FieldSpec {
	return append(bitableLoc(),
		field.String("filter").Label("筛选公式").
			Desc(`如 CurrentValue.[状态]="待处理"；留空取全部`).Optional(),
		field.Int("page_size").Label("每页条数").Desc("默认 100，上限 500").Optional(),
		field.String("page_token").Label("翻页标记").Optional(),
	)
}

func (BitableListRecords) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("records", []BitableRecord{}).Label("记录列表"),
		field.Int("total").Label("总数"),
		field.String("page_token").Label("翻页标记"),
		field.Bool("has_more").Label("还有更多"),
	}
}

// BitableRecord is a single record. The keys of fields are **field names** (not field ids),
// matching what's shown on the web page.
type BitableRecord struct {
	RecordID string         `sokel:"record_id" label:"记录 ID"`
	Fields   map[string]any `sokel:"fields" label:"字段" desc:"键=字段名，值形状随字段类型（文本串/数字/选项数组…），由多维表格列配置决定"`
}

// BitableCreateRecord adds a record.
type BitableCreateRecord struct{}

func (BitableCreateRecord) Meta() contract.Meta {
	return contract.Meta{ID: "bitable_create_record", Label: "多维表格·加记录",
		Desc: "新增一条记录。fields 键=字段名：文本填字符串、数字填数字、单选填选项名、多选填选项名数组"}
}

func (BitableCreateRecord) Inputs() []contract.FieldSpec {
	return append(bitableLoc(),
		field.Object("fields", "键=字段名，值按字段类型：文本→字符串、数字→数字、单选→选项名、多选→数组、日期→毫秒时间戳；形状由表格列配置决定").
			Label("字段值"),
	)
}

func (BitableCreateRecord) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("record_id").Label("记录 ID")}
}

// BitableUpdateRecord updates a record.
type BitableUpdateRecord struct{}

func (BitableUpdateRecord) Meta() contract.Meta {
	return contract.Meta{ID: "bitable_update_record", Label: "多维表格·改记录",
		Desc: "按 record_id 更新记录的部分字段（没提到的字段不动）"}
}

func (BitableUpdateRecord) Inputs() []contract.FieldSpec {
	return append(bitableLoc(),
		field.String("record_id").Label("记录 ID"),
		field.Object("fields", "键=字段名，值按字段类型；只更新提到的字段").Label("字段值"),
	)
}

func (BitableUpdateRecord) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("成功")}
}

// BitableDeleteRecord deletes a record.
type BitableDeleteRecord struct{}

func (BitableDeleteRecord) Meta() contract.Meta {
	return contract.Meta{ID: "bitable_delete_record", Label: "多维表格·删记录"}
}

func (BitableDeleteRecord) Inputs() []contract.FieldSpec {
	return append(bitableLoc(), field.String("record_id").Label("记录 ID"))
}

func (BitableDeleteRecord) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("成功")}
}

// —— Drive ——

// DriveUpload uploads a file to Drive.
type DriveUpload struct{}

func (DriveUpload) Meta() contract.Meta {
	return contract.Meta{ID: "drive_upload", Label: "上传到网盘", TimeoutSec: 600,
		Desc: "把文件传到云空间的指定文件夹。与发文件不同：进网盘可长期存、可分享链接"}
}

func (DriveUpload) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.File("file").Label("文件"),
		field.String("folder_token").Label("文件夹 token").
			Desc("网盘 URL 里 folder/ 后那串。**必填**——应用空间里的文件用户看不到"),
	}
}

func (DriveUpload) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("file_token").Label("文件 token"),
		field.String("url").Label("文件链接"),
	}
}

// —— Fallback ——

// Call is a generic call that covers the entire Open Platform API.
type Call struct{}

func (Call) Meta() contract.Meta {
	return contract.Meta{ID: "call", Label: "通用调用",
		Desc: "直调任意开放平台接口（tenant_access_token 由插件管理）。typed 操作没覆盖的能力走这里"}
}

func (Call) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Enum("method",
			field.Opt("GET", "GET"), field.Opt("POST", "POST"),
			field.Opt("PUT", "PUT"), field.Opt("PATCH", "PATCH"), field.Opt("DELETE", "DELETE")).
			Label("HTTP 方法").Default("POST"),
		field.String("path").Label("接口路径").
			Desc("以 /open-apis/ 开头，如 /open-apis/im/v1/messages?receive_id_type=chat_id"),
		field.Object("body", "请求体 JSON，键名照飞书开放平台文档；本插件不介入").Label("请求体").Optional(),
	}
}

func (Call) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Int("code").Label("业务码").Desc("0=成功；非 0 时 msg 里有飞书的报错"),
		field.String("msg").Label("说明"),
		field.Any("data", "飞书应答的 data 字段，形状随接口而变，由开放平台文档定义").Label("数据"),
	}
}

// HealthCheck is the platform's conventional credential health-check operation.
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "检查凭证", TimeoutSec: 30,
		Desc: "用 app_id/app_secret 换一次 tenant_access_token 并查 bot 信息——token 换不出来即凭证失效"}
}

func (HealthCheck) Inputs() []contract.FieldSpec { return nil }

func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("可用"),
		field.String("bot_name").Label("bot 名称"),
		field.String("message").Label("说明"),
	}
}

// —— Events (pushed down over the long connection, starting a workflow) ——
//
// The three events share chat_id (Events.CommonFields): the platform flattens it to the top
// level of the trigger input, so a reply node can bind the same variable without drilling into
// each branch's own payload.

// MessageReceived fires on a received message (direct in a 1:1 chat; in a group chat it only
// arrives when the bot is @-mentioned — that's decided by Feishu's permissions, not filtered by
// this plugin).
type MessageReceived struct{}

func (MessageReceived) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "message", Label: "收到消息"}
}

func (MessageReceived) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("chat_id").Label("对话 ID").Desc("回消息用它（receive_id_type=chat_id）"),
		field.Enum("chat_type", field.Opt("p2p", "单聊"), field.Opt("group", "群聊")).Label("对话类型"),
		field.String("message_id").Label("消息 ID").Desc("reply_message 认它"),
		field.String("sender_open_id").Label("发送者 open_id"),
		field.String("message_type").Label("消息类型").Desc("text/image/file/post/audio…"),
		field.Text("text").Label("文本").Desc("text 消息的正文（已去掉 @bot 标记）；非文本消息为空").Optional(),
		field.Bool("mentioned_bot").Label("是否@了bot"),
		field.Any("raw", "飞书 im.message.receive_v1 的原始事件，形状由开放平台定义且随版本增补").Label("原始事件"),
	}
}

// CardAction fires on a card button click.
type CardAction struct{}

func (CardAction) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "card_action", Label: "卡片按钮点击"}
}

func (CardAction) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("chat_id").Label("对话 ID").Optional(),
		field.String("message_id").Label("卡片消息 ID"),
		field.String("operator_open_id").Label("点击者 open_id"),
		field.Any("action_value", "按钮上配置的 value，形状由卡片作者定义（通常是 {action:\"approve\"} 这类小对象）").
			Label("按钮值").Desc("卡片里给按钮配的 value——用它区分点了哪个钮"),
		field.Any("form_values", "卡片表单收集的值（没有表单则为空），键名由卡片作者定义").Label("表单值").Optional(),
		field.Any("raw", "飞书 card.action.trigger 的原始回调，形状由开放平台定义").Label("原始事件"),
	}
}

// BotAdded fires when the bot is added to a chat.
type BotAdded struct{}

func (BotAdded) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "bot_added", Label: "bot 被拉进群"}
}

func (BotAdded) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("chat_id").Label("群 ID"),
		field.String("chat_name").Label("群名").Optional(),
		field.String("inviter_open_id").Label("拉人者 open_id").Optional(),
		field.Any("raw", "飞书 im.chat.member.bot.added_v1 的原始事件，形状由开放平台定义").Label("原始事件"),
	}
}

// Events declares the common fields.
type Events struct{}

func (Events) CommonFields() []string { return []string{"chat_id"} }

// —— Credential ——

// Credential is a custom app's credential. One credential = one app; the event source opens one
// long-lived connection per credential (multiple apps in a single instance).
type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("app_id").Label("App ID").
			Desc("开放平台「开发者后台 → 凭证与基础信息」里的 App ID（cli_ 开头）"),
		field.Secret("app_secret").Label("App Secret").
			Desc("同页的 App Secret。只在插件内部换 tenant_access_token，不进节点与日志"),
		field.Select("domain", "feishu", "lark").
			Label("平台").Desc("feishu=国内（open.feishu.cn）；lark=国际版（open.larksuite.com）。默认 feishu").Optional(),
	}
}
