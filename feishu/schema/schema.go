// Package schema 声明 feishu 插件的操作、事件与凭证契约。
//
// 定位：飞书**自建应用**的全能力侧——消息/卡片/查人/群管理/云文档/多维表格/网盘 +
// 长连接事件源（收到消息 / 卡片按钮 / bot 进群 → 起工作流）。
// 群里的「自定义机器人 webhook」是另一个插件（feishu-webhook）：凭证形态与授权范围
// 完全不同（一条群 webhook vs 一个企业应用），不混在一个凭证池里。
//
// 分层沿用 telegram-bot 的判断：**typed 常用 + call 保底**。typed 操作给画布
// 「填字段」的体验；call 收 method+path+body 覆盖全量开放平台 API，飞书加接口零改代码。
//
// 两条飞书特有的约定，操作设计都围着它们转：
//
//   - **receive_id 是带类型的**。同一个「发给谁」按 receive_id_type 解释成
//     open_id / chat_id / user_id / email / union_id 之一——所以发送类操作都带
//     一个类型下拉，而不是让用户猜「这串 id 是什么」。
//   - **图片/文件要先换 key**。飞书不收原始字节直发，要先 upload 换 image_key/file_key
//     再引用。send_image/send_file 内部包掉这两步；单独的 upload_* 留给「一次上传、
//     多次发送」的流程。
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// receiveIDFields 发送类操作共有的「发给谁」两件套。
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

// sentMessageOutputs 发送类操作统一的产出。
func sentMessageOutputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("message_id").Label("消息 ID").Desc("om_ 开头；回复/撤回都认它"),
		field.String("chat_id").Label("所在对话 ID"),
	}
}

// SendText 发文本。
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

// SendMarkdown 发 Markdown（内部包成单元素卡片）。
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

// SendCard 发交互卡片（完整 JSON）。
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

// SendImage 发图片（上传换 key + 发送，两步包成一步）。
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

// SendFile 发文件。
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

// ReplyMessage 回复某条消息（消息事件触发的工作流里最常用）。
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

// RecallMessage 撤回。
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

// UploadImage 只上传不发送。
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

// UploadFile 只上传不发送。
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

// GetUser 按 email/手机号查 open_id——给用户发私信前必经的一跳。
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

// ListChats bot 所在的群列表。
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

// Chat 一个群。
type Chat struct {
	ChatID      string `sokel:"chat_id" label:"群 ID"`
	Name        string `sokel:"name" label:"群名"`
	Description string `sokel:"description,optional" label:"群描述"`
	OwnerID     string `sokel:"owner_id,optional" label:"群主 open_id"`
	External    bool   `sokel:"external,optional" label:"是否外部群"`
}

// CreateChat 建群。
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

// AddChatMembers 拉人进群。
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

// —— 云文档（docx）——

// DocxCreate 创建云文档。
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

// DocxAppend 向文档追加内容。
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

// —— 多维表格（bitable）——

// bitableLoc 多维表格操作共有的定位两件套。
func bitableLoc() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("app_token").Label("多维表格 token").
			Desc("表格 URL 里 base/ 后那串（bascn…）"),
		field.String("table_id").Label("数据表 ID").
			Desc("URL 里 table= 后那串（tbl…）"),
	}
}

// BitableListRecords 查记录。
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

// BitableRecord 一条记录。fields 的键是**字段名**（不是字段 id），与网页上看到的一致。
type BitableRecord struct {
	RecordID string         `sokel:"record_id" label:"记录 ID"`
	Fields   map[string]any `sokel:"fields" label:"字段" desc:"键=字段名，值形状随字段类型（文本串/数字/选项数组…），由多维表格列配置决定"`
}

// BitableCreateRecord 加记录。
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

// BitableUpdateRecord 改记录。
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

// BitableDeleteRecord 删记录。
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

// —— 网盘 ——

// DriveUpload 上传文件到网盘。
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

// —— 保底 ——

// Call 通用调用：覆盖整个开放平台 API。
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

// HealthCheck 平台约定的凭证体检操作。
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

// —— 事件（长连接推下来，起工作流）——
//
// 三个事件共享 chat_id（Events.CommonFields）：平台把它平铺到触发输入顶层，
// 回复节点绑同一个变量即可，不必按分支从各自 payload 下钻。

// MessageReceived 收到消息（单聊直发，群聊要 @bot 才收得到——由飞书权限决定，不是本插件筛的）。
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

// CardAction 卡片按钮点击。
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

// BotAdded bot 被拉进群。
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

// Events 声明公共字段。
type Events struct{}

func (Events) CommonFields() []string { return []string{"chat_id"} }

// —— 凭证 ——

// Credential：自建应用凭证。一条凭证 = 一个应用；事件源按凭证起长连接（多应用单实例）。
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
