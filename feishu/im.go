package main

// Message operations. All go through the /open-apis/im/v1/messages family.
//
// Two easy-to-miss Feishu conventions are handled here in one place:
//
//   - **content is a "JSON string," not JSON**: the content field in the request body must
//     have {"text":"…"} serialized into a string first, then stuffed in (double encoding).
//     Getting this wrong shows up as invalid content.
//   - **sending requires a uuid idempotency key**: workflow nodes retry, and retrying without a
//     uuid means sending the message twice. The uuid is derived from the platform's run context
//     — however many times a single node execution retries, it's the same message.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

// sentMsg holds the two fields we want from the /im/v1/messages response.
type sentMsg struct {
	MessageID string `json:"message_id"`
	ChatID    string `json:"chat_id"`
}

// sendUUID is the send idempotency key: the same node execution (run_id+node_id+recipient+
// content) -> the same uuid, which Feishu dedups by (a 1-hour window), so a workflow retry
// won't send a second message.
//
// **Returns an empty string when there's no run identity (= no uuid attached)**: test calls and
// health checks have no retry semantics, and a constant key would wrongly dedup away "send the
// same text again a few minutes later" into not being sent at all.
func sendUUID(ctx plugin.Ctx, parts ...string) string {
	run, node := sokel.TraceValue(ctx, "run_id"), sokel.TraceValue(ctx, "node_id")
	if run == "" {
		return ""
	}
	h := sha256.New()
	h.Write([]byte(run + "|" + node))
	for _, p := range parts {
		h.Write([]byte("|" + p))
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

// sendMessage is the unified send path: msgType + content (an already-serialized JSON string).
func sendMessage(ctx plugin.Ctx, receiveID, receiveIDType, msgType, content string) (*sentMsg, error) {
	receiveID, receiveIDType = strings.TrimSpace(receiveID), strings.TrimSpace(receiveIDType)
	if receiveID == "" {
		return nil, fmt.Errorf("接收方 ID 是空的——群聊填 chat_id（oc_…），单聊填 open_id（ou_…）")
	}
	if receiveIDType == "" {
		receiveIDType = guessIDType(receiveID)
	}
	body := map[string]any{"receive_id": receiveID, "msg_type": msgType, "content": content}
	if u := sendUUID(ctx, receiveID, msgType, content); u != "" {
		body["uuid"] = u
	}
	data, err := callRaw(ctx, credOf(ctx), "POST",
		"/open-apis/im/v1/messages?receive_id_type="+url.QueryEscape(receiveIDType), body)
	if err != nil {
		return nil, err
	}
	var m sentMsg
	_ = json.Unmarshal(data, &m)
	return &m, nil
}

// guessIDType guesses from the prefix when the type isn't given — Feishu's id prefixes are a
// stable convention, and even the one case where guessing can be wrong (a bare email/user_id)
// still produces a readable error instead of silently sending to the wrong place.
func guessIDType(id string) string {
	switch {
	case strings.HasPrefix(id, "oc_"):
		return "chat_id"
	case strings.HasPrefix(id, "ou_"):
		return "open_id"
	case strings.HasPrefix(id, "on_"):
		return "union_id"
	case strings.Contains(id, "@"):
		return "email"
	}
	return "chat_id"
}

// jsonStr performs content's double encoding.
func jsonStr(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func opSendText(ctx plugin.Ctx, in *SendTextIn) (*SendTextOut, error) {
	if strings.TrimSpace(in.Text) == "" {
		return nil, fmt.Errorf("文本是空的")
	}
	m, err := sendMessage(ctx, in.ReceiveID, in.ReceiveIDType, "text", jsonStr(map[string]string{"text": in.Text}))
	if err != nil {
		return nil, err
	}
	return &SendTextOut{MessageID: m.MessageID, ChatID: m.ChatID}, nil
}

// mdCard wraps Markdown into a single-element card (Feishu has no bare markdown message type;
// lark_md only exists inside a card).
func mdCard(title, titleColor, md string) map[string]any {
	card := map[string]any{
		"config":   map[string]any{"wide_screen_mode": true},
		"elements": []any{map[string]any{"tag": "markdown", "content": md}},
	}
	if t := strings.TrimSpace(title); t != "" {
		if titleColor == "" {
			titleColor = "blue"
		}
		card["header"] = map[string]any{
			"title":    map[string]any{"tag": "plain_text", "content": t},
			"template": titleColor,
		}
	}
	return card
}

func opSendMarkdown(ctx plugin.Ctx, in *SendMarkdownIn) (*SendMarkdownOut, error) {
	if strings.TrimSpace(in.Markdown) == "" {
		return nil, fmt.Errorf("Markdown 正文是空的")
	}
	m, err := sendMessage(ctx, in.ReceiveID, in.ReceiveIDType, "interactive",
		jsonStr(mdCard(in.Title, in.TitleColor, in.Markdown)))
	if err != nil {
		return nil, err
	}
	return &SendMarkdownOut{MessageID: m.MessageID, ChatID: m.ChatID}, nil
}

func opSendCard(ctx plugin.Ctx, in *SendCardIn) (*SendCardOut, error) {
	if len(in.Card) == 0 {
		return nil, fmt.Errorf("卡片 JSON 是空的——在飞书「卡片搭建工具」拼好后把 JSON 粘进来")
	}
	m, err := sendMessage(ctx, in.ReceiveID, in.ReceiveIDType, "interactive", jsonStr(in.Card))
	if err != nil {
		return nil, err
	}
	return &SendCardOut{MessageID: m.MessageID, ChatID: m.ChatID}, nil
}

func opSendImage(ctx plugin.Ctx, in *SendImageIn) (*SendImageOut, error) {
	key, err := uploadImage(ctx, in.Image)
	if err != nil {
		return nil, err
	}
	m, err := sendMessage(ctx, in.ReceiveID, in.ReceiveIDType, "image", jsonStr(map[string]string{"image_key": key}))
	if err != nil {
		return nil, err
	}
	return &SendImageOut{MessageID: m.MessageID, ChatID: m.ChatID, ImageKey: key}, nil
}

func opSendFile(ctx plugin.Ctx, in *SendFileIn) (*SendFileOut, error) {
	key, err := uploadFile(ctx, in.File)
	if err != nil {
		return nil, err
	}
	m, err := sendMessage(ctx, in.ReceiveID, in.ReceiveIDType, "file", jsonStr(map[string]string{"file_key": key}))
	if err != nil {
		return nil, err
	}
	return &SendFileOut{MessageID: m.MessageID, ChatID: m.ChatID, FileKey: key}, nil
}

func opReplyMessage(ctx plugin.Ctx, in *ReplyMessageIn) (*ReplyMessageOut, error) {
	mid := strings.TrimSpace(in.MessageID)
	if mid == "" {
		return nil, fmt.Errorf("消息 ID 是空的——它来自「收到消息」事件或发送操作的产出（om_ 开头）")
	}
	msgType, content := "text", ""
	switch {
	case strings.TrimSpace(in.Markdown) != "":
		msgType, content = "interactive", jsonStr(mdCard("", "", in.Markdown))
	case strings.TrimSpace(in.Text) != "":
		content = jsonStr(map[string]string{"text": in.Text})
	default:
		return nil, fmt.Errorf("text 与 markdown 至少填一个")
	}
	rbody := map[string]any{"msg_type": msgType, "content": content}
	if u := sendUUID(ctx, mid, msgType, content); u != "" {
		rbody["uuid"] = u
	}
	data, err := callRaw(ctx, credOf(ctx), "POST",
		"/open-apis/im/v1/messages/"+url.PathEscape(mid)+"/reply", rbody)
	if err != nil {
		return nil, err
	}
	var m sentMsg
	_ = json.Unmarshal(data, &m)
	return &ReplyMessageOut{MessageID: m.MessageID, ChatID: m.ChatID}, nil
}

func opRecallMessage(ctx plugin.Ctx, in *RecallMessageIn) (*RecallMessageOut, error) {
	mid := strings.TrimSpace(in.MessageID)
	if mid == "" {
		return nil, fmt.Errorf("消息 ID 是空的")
	}
	if _, err := callRaw(ctx, credOf(ctx), "DELETE", "/open-apis/im/v1/messages/"+url.PathEscape(mid), nil); err != nil {
		return nil, err
	}
	return &RecallMessageOut{OK: true}, nil
}

// —— Uploads (multipart, handled by the typed SDK; exactly where the SDK earns its keep) ——

func uploadImage(ctx plugin.Ctx, f *plugin.File) (string, error) {
	if f == nil || f.ID == "" {
		return "", fmt.Errorf("没给图片")
	}
	data, err := ctx.Fetch(f)
	if err != nil {
		return "", fmt.Errorf("取文件失败: %w", err)
	}
	c, err := clientOf(credOf(ctx))
	if err != nil {
		return "", err
	}
	resp, err := c.Im.Image.Create(ctx, larkim.NewCreateImageReqBuilder().
		Body(larkim.NewCreateImageReqBodyBuilder().
			ImageType("message").
			Image(strings.NewReader(string(data))).
			Build()).Build())
	if err != nil {
		return "", connErr(err)
	}
	if !resp.Success() {
		return "", feishuErr(resp.Code, resp.Msg)
	}
	return larkcore.StringValue(resp.Data.ImageKey), nil
}

func uploadFile(ctx plugin.Ctx, f *plugin.File) (string, error) {
	if f == nil || f.ID == "" {
		return "", fmt.Errorf("没给文件")
	}
	data, err := ctx.Fetch(f)
	if err != nil {
		return "", fmt.Errorf("取文件失败: %w", err)
	}
	name := f.Name
	if name == "" {
		name = "file.bin"
	}
	c, err := clientOf(credOf(ctx))
	if err != nil {
		return "", err
	}
	resp, err := c.Im.File.Create(ctx, larkim.NewCreateFileReqBuilder().
		Body(larkim.NewCreateFileReqBodyBuilder().
			FileType(imFileType(name)).
			FileName(name).
			File(strings.NewReader(string(data))).
			Build()).Build())
	if err != nil {
		return "", connErr(err)
	}
	if !resp.Success() {
		return "", feishuErr(resp.Code, resp.Msg)
	}
	return larkcore.StringValue(resp.Data.FileKey), nil
}

// imFileType: Feishu requires reporting the type by extension; unrecognized ones fall back to stream.
func imFileType(name string) string {
	i := strings.LastIndex(name, ".")
	if i < 0 {
		return "stream"
	}
	switch strings.ToLower(name[i+1:]) {
	case "opus":
		return "opus"
	case "mp4":
		return "mp4"
	case "pdf":
		return "pdf"
	case "doc", "docx":
		return "doc"
	case "xls", "xlsx":
		return "xls"
	case "ppt", "pptx":
		return "ppt"
	}
	return "stream"
}

func opUploadImage(ctx plugin.Ctx, in *UploadImageIn) (*UploadImageOut, error) {
	key, err := uploadImage(ctx, in.Image)
	if err != nil {
		return nil, err
	}
	return &UploadImageOut{ImageKey: key}, nil
}

func opUploadFile(ctx plugin.Ctx, in *UploadFileIn) (*UploadFileOut, error) {
	key, err := uploadFile(ctx, in.File)
	if err != nil {
		return nil, err
	}
	return &UploadFileOut{FileKey: key}, nil
}
