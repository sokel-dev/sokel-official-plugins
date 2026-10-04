// Package schema declares wechat-claw's operation and event contracts.
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/auth"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// —— Send operations ——

func okOutput() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("是否成功")}
}

// SendText sends a text message to a given wxid.
type SendText struct{}

func (SendText) Meta() contract.Meta {
	return contract.Meta{ID: "send_text", Label: "发送文本", Desc: "向指定 wxid 发送文本消息"}
}
func (SendText) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("to").Label("对方 wxid").Desc("收信人（事件里的 chat_id）"),
		field.Text("text").Label("文本内容"),
	}
}
func (SendText) Outputs() []contract.FieldSpec { return okOutput() }

// SendImage uploads and sends an image.
type SendImage struct{}

func (SendImage) Meta() contract.Meta {
	return contract.Meta{ID: "send_image", Label: "发送图片", Desc: "上传并发送图片（可带说明文字）"}
}
func (SendImage) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("to").Label("对方 wxid"),
		field.File("image").Label("图片"),
		field.String("caption").Label("说明文字").Optional(),
	}
}
func (SendImage) Outputs() []contract.FieldSpec { return okOutput() }

// SendFile uploads and sends a file attachment.
type SendFile struct{}

func (SendFile) Meta() contract.Meta {
	return contract.Meta{ID: "send_file", Label: "发送文件", Desc: "上传并发送文件附件"}
}
func (SendFile) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("to").Label("对方 wxid"),
		field.File("file").Label("文件"),
		field.String("filename").Label("文件名").Desc("缺省用上游文件名").Optional(),
		field.String("caption").Label("说明文字").Optional(),
	}
}
func (SendFile) Outputs() []contract.FieldSpec { return okOutput() }

// —— Credential ——

// Credential is this plugin's credential contract and how it's obtained.
type Credential struct{}

// AuthMeta: the session is obtained by **scanning a QR code** — the login flow is driven by the
// plugin (generating the QR code, polling the status), and the platform just relays it, writing
// the session into the credential row once confirmed (never sent back to the frontend in plaintext).
func (Credential) AuthMeta() contract.AuthMeta {
	return auth.QR()
}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Secret("session").Label("会话（扫码登录获取）").
			Desc("点凭证行的「登录/授权」扫码获取，无需手填").Optional(),
	}
}

// —— Events ——

// MessageReceived: a WeChat message was received.
type MessageReceived struct{}

func (MessageReceived) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "message", Label: "收到消息"}
}
func (MessageReceived) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("chat_id").Label("对话（发信人 wxid）"),
		field.String("to").Label("收信方（本 bot）"),
		field.Text("text").Label("文本内容").Optional(),
		field.Int("message_id").Label("消息 ID"),
		field.Files("images").Label("图片").Desc("消息附带图片（平台文件引用，可多张）").Optional(),
		field.Files("files").Label("文件").Desc("消息附带文件（平台文件引用）").Optional(),
		field.File("voice").Label("语音").Desc("消息语音（平台文件引用）").Optional(),
		field.Any("raw", "上游原始消息，形状由 clawbot 决定").Label("原始消息"),
	}
}

// Events declares the shared fields.
type Events struct{}

func (Events) CommonFields() []string { return []string{"chat_id"} }
