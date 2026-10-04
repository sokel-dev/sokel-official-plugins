// telegram-bot —— Sokel 第一方可部署插件：Telegram Bot API 的「发送/操作」侧。
//
// 定位（见 docs/telegram-integration.md）：一个「全功能」TG bot 分两半——
//   - 发送/操作（本插件）：sendMessage/sendPhoto/answerCallbackQuery/getFile… 纯出站调 api.telegram.org，
//     无需平台新 feature，做成 SDK 插件即可（与 http-egress 同构，出站接入 broker，可跑公网服务器）。
//   - 接收/触发（不在本插件）：收到消息→起工作流，需平台「事件触发」运行时或公网 webhook，见文档 §3 待对齐。
//
// 设计：一个通用 call 覆盖【整个】Bot API（method + params，新方法零改代码）+ 一组 typed 便捷操作（画布字段友好）。
// 凭证 = bot_token（secret）；token 只在插件内部拼进 URL 路径，绝不进节点入参/输出（避免泄漏到画布与日志）。
//
// 运行：SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx SOKEL_NATS_TOKEN=xxx ./telegram-bot
package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"reflect"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

func main() {
	token := sokel.Env("TOKEN")
	if token == "" && sokel.Env("DEPLOY_KEY") == "" {
		log.Fatal("请设置 SOKEL_TOKEN(接入组「接入命令」里复制);随部署托管的容器可改设 SOKEL_DEPLOY_KEY 自动注册")
	}
	p := sokel.New(sokel.Config{
		Endpoint: env("SOKEL_ENDPOINT", "http://localhost:8088"),
		Token:    token,
		Name:     "telegram-bot",
	})
	RegisterCredential(p)  // 凭证契约（schema 声明生成；Cred 在 zz_credential.go）
	p.SetDoc(usageDoc, "") // 使用说明（docs/*.md）：凭证怎么拿、有什么坑，随握手上报给平台

	// 通用兜底：覆盖整个 Bot API（method 任意 + params 任意 JSON）。
	OnCall(p, opCall)

	// —— 高频 typed 便捷操作（画布字段友好；内部都走同一 callAPI）——
	OnSendMessage(p, opSendMessage)
	OnSendPhoto(p, opSendPhoto)
	OnSendDocument(p, opSendDocument)
	OnSendChatAction(p, opSendChatAction)
	OnEditMessageText(p, opEditMessageText)
	OnDeleteMessage(p, opDeleteMessage)
	OnForwardMessage(p, opForwardMessage)
	OnAnswerCallbackQuery(p, opAnswerCallback)
	OnGetChat(p, opGetChat)
	OnGetMe(p, opGetMe)
	OnHealthCheck(p, opHealthCheck) // 凭证页「检查」按钮调它（id 必须是 health_check）
	OnDownloadFile(p, opDownloadFile)

	// —— bot 配置类（webhook 模式铺路 + 菜单命令）——
	OnSetWebhook(p, opSetWebhook)
	OnDeleteWebhook(p, opDeleteWebhook)
	OnGetWebhookInfo(p, opGetWebhookInfo)
	OnSetMyCommands(p, opSetMyCommands)

	// —— 事件契约（接收侧）：声明本 bot 产的事件类型 + 各自 payload（前端 trigger_event 节点据此派生出口 handle）——
	DeclareEvents(p)
	// 公共字段：所有事件 payload 都有 chat_id → 平台触发时平铺到输入顶层（{{节点.chat_id}}），
	// 各事件分支共享同一变量，不必按分支从各自事件 payload 下钻。SDK 强校验（缺字段/类型不一致即报错）。

	// 常驻事件源：长轮询 getUpdates → 每条 update 按类型 Trigger 推给平台（见 updates.go）。
	// 长轮询无需 setWebhook、不要求平台公网可达（适配 NAT）；建议单副本运行（平台按 update_id 去重兜底）。
	sokel.RegisterSource(p, sokel.Source{ID: "updates", Label: "TG 更新长轮询"}, runUpdatesSource)

	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}

// —— 通用调用 ——

func opCall(ctx plugin.Ctx, in *CallIn) (*CallOut, error) {
	res, err := callAPI(ctx, in.Method, in.Params)
	if err != nil {
		return nil, err
	}
	return remap[CallOut](map[string]any{"ok": true, "result": res})
}

// —— typed 便捷操作 ——

func opSendMessage(ctx plugin.Ctx, in *SendMessageIn) (*SendMessageOut, error) {
	return messageOut[SendMessageOut](ctx, "sendMessage", compact(map[string]any{
		"chat_id": in.ChatID, "text": in.Text, "parse_mode": in.ParseMode,
		"reply_to_message_id": in.ReplyToMessageID, "reply_markup": in.ReplyMarkup,
		"disable_web_page_preview": in.DisableWebPagePreview,
	}))
}

func opSendPhoto(ctx plugin.Ctx, in *SendPhotoIn) (*SendPhotoOut, error) {
	return messageOut[SendPhotoOut](ctx, "sendPhoto", compact(map[string]any{
		"chat_id": in.ChatID, "photo": in.Photo, "caption": in.Caption, "parse_mode": in.ParseMode,
	}))
}

func opSendDocument(ctx plugin.Ctx, in *SendDocumentIn) (*SendDocumentOut, error) {
	return messageOut[SendDocumentOut](ctx, "sendDocument", compact(map[string]any{
		"chat_id": in.ChatID, "document": in.Document, "caption": in.Caption,
	}))
}

func opSendChatAction(ctx plugin.Ctx, in *SendChatActionIn) (*SendChatActionOut, error) {
	return okOut[SendChatActionOut](ctx, "sendChatAction", map[string]any{"chat_id": in.ChatID, "action": in.Action})
}

func opEditMessageText(ctx plugin.Ctx, in *EditMessageTextIn) (*EditMessageTextOut, error) {
	return messageOut[EditMessageTextOut](ctx, "editMessageText", compact(map[string]any{
		"chat_id": in.ChatID, "message_id": in.MessageID, "text": in.Text,
		"parse_mode": in.ParseMode, "reply_markup": in.ReplyMarkup,
	}))
}

func opDeleteMessage(ctx plugin.Ctx, in *DeleteMessageIn) (*DeleteMessageOut, error) {
	return okOut[DeleteMessageOut](ctx, "deleteMessage", map[string]any{"chat_id": in.ChatID, "message_id": in.MessageID})
}

func opForwardMessage(ctx plugin.Ctx, in *ForwardMessageIn) (*ForwardMessageOut, error) {
	return messageOut[ForwardMessageOut](ctx, "forwardMessage", map[string]any{
		"chat_id": in.ChatID, "from_chat_id": in.FromChatID, "message_id": in.MessageID,
	})
}

func opAnswerCallback(ctx plugin.Ctx, in *AnswerCallbackQueryIn) (*AnswerCallbackQueryOut, error) {
	return okOut[AnswerCallbackQueryOut](ctx, "answerCallbackQuery", compact(map[string]any{
		"callback_query_id": in.CallbackQueryID, "text": in.Text, "show_alert": in.ShowAlert,
	}))
}

func opGetChat(ctx plugin.Ctx, in *GetChatIn) (*GetChatOut, error) {
	res, err := callAPI(ctx, "getChat", map[string]any{"chat_id": in.ChatID})
	if err != nil {
		return nil, err
	}
	return remap[GetChatOut](map[string]any{"ok": true, "result": res})
}

type EmptyIn struct{}

func opGetMe(ctx plugin.Ctx, in *GetMeIn) (*GetMeOut, error) {
	res, err := callAPI(ctx, "getMe", nil)
	if err != nil {
		return nil, err
	}
	return remap[GetMeOut](map[string]any{"ok": true, "result": res})
}

// opHealthCheck：凭证体检 —— 打一次 getMe。
//
// getMe 不发消息、不碰任何对话，是 Bot API 里唯一「只验 token」的调用；
// 它回的 username 还能戳穿「token 复制成了另一个 bot 的」——测试 bot 与正式 bot
// 的两串 token 长得一模一样，只报「通了」看不出配错了哪一个。
//
// token 不对时返回 ok=false + message 而**不是** error：平台把 error 当「这个插件没法体检」，
// 把 ok=false 当「体检结论是不可用」，而 Telegram 那句 401 Unauthorized 得让人看见。
func opHealthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	res, err := callAPI(ctx, "getMe", nil)
	if err != nil {
		return &HealthCheckOut{Message: err.Error()}, nil
	}
	m, _ := res.(map[string]any)
	username, _ := m["username"].(string)
	out := &HealthCheckOut{OK: true, Username: username, Message: "token 有效"}
	if username != "" {
		out.Message = fmt.Sprintf("token 有效（bot @%s）", username)
	}
	return out, nil
}

func opDownloadFile(ctx plugin.Ctx, in *DownloadFileIn) (*DownloadFileOut, error) {
	res, err := callAPI(ctx, "getFile", map[string]any{"file_id": in.FileID})
	if err != nil {
		return nil, err
	}
	fm, _ := res.(map[string]any)
	filePath, _ := fm["file_path"].(string)
	if filePath == "" {
		return nil, fmt.Errorf("getFile 未返回 file_path")
	}
	url := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", botToken(ctx), filePath)
	reqCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载文件失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return nil, fmt.Errorf("读取文件字节失败: %w", err)
	}
	return remap[DownloadFileOut](map[string]any{"file_path": filePath, "size": int64(len(body)), "base64": base64.StdEncoding.EncodeToString(body)})
}

// —— 共用 ——

// messageOut / okOut：几个操作的出参形状相同，但**生成物是每操作一个类型**
// （契约名相同不等于 Go 类型相同）。所以 helper 回一个 map，各 handler 再 remap 到
// 自己的类型——比给每种出参写一遍 helper 省事，也不必引入泛型。
func messageOut[T any](ctx plugin.Ctx, method string, params map[string]any) (*T, error) {
	res, err := callAPI(ctx, method, params)
	if err != nil {
		return nil, err
	}
	var mid int64
	if m, ok := res.(map[string]any); ok {
		if v, ok := m["message_id"].(float64); ok {
			mid = int64(v)
		}
	}
	return remap[T](map[string]any{"ok": true, "message_id": mid, "result": res})
}

func okOut[T any](ctx plugin.Ctx, method string, params map[string]any) (*T, error) {
	res, err := callAPI(ctx, method, params)
	if err != nil {
		return nil, err
	}
	return remap[T](map[string]any{"ok": true, "result": res})
}

// remap：map → 生成的 typed 出参。用 contract.BindInput 而非 json.Unmarshal——
// 后者只认 json tag，契约名不是 snake_case 时会静默绑空。
func remap[T any](m map[string]any) (*T, error) {
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	var out T
	if err := contract.BindInput(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// botToken：操作侧取 bot token——优先平台注入的凭证（可多 bot：每个工作流配不同凭证），
// 缺省回退环境变量 TELEGRAM_BOT_TOKEN（单 bot 部署 / 与事件源同一个）。
func botToken(ctx sokel.Ctx) string {
	if c := sokel.CredentialAs[Cred](ctx); c.BotToken != "" {
		return c.BotToken
	}
	return os.Getenv("TELEGRAM_BOT_TOKEN")
}

// callAPI：操作侧调用（token 从凭证/env 取）。
func callAPI(ctx sokel.Ctx, method string, params map[string]any) (any, error) {
	tok := botToken(ctx)
	if tok == "" {
		return nil, fmt.Errorf("缺少 bot_token（配置插件凭证，或设 TELEGRAM_BOT_TOKEN 环境变量）")
	}
	return callTelegram(ctx, tok, method, params)
}

// callTelegram：POST https://api.telegram.org/bot<token>/<method>，JSON body=params，解析 {ok,result,description}。
// token 只进 URL 路径，不出现在任何返回值里。事件源与操作共用此底层。
func callTelegram(ctx context.Context, token, method string, params map[string]any) (any, error) {
	if params == nil {
		params = map[string]any{}
	}
	buf, _ := json.Marshal(params)
	url := fmt.Sprintf("https://api.telegram.org/bot%s/%s", token, method)
	reqCtx, cancel := context.WithTimeout(ctx, 40*time.Second) // getUpdates 长轮询 timeout=25s，留余量
	defer cancel()
	req, _ := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("调用 %s 失败: %w", method, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	var out struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		Description string          `json:"description"`
		ErrorCode   int             `json:"error_code"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("%s 响应非法 JSON: %s", method, truncate(string(body), 200))
	}
	if !out.OK {
		return nil, fmt.Errorf("Telegram 拒绝 %s（%d）：%s", method, out.ErrorCode, out.Description)
	}
	var result any
	_ = json.Unmarshal(out.Result, &result)
	return result, nil
}

// compact：去掉零值键（Telegram 对空串 parse_mode / 0 reply_to 等敏感，不传即用默认）。
func compact(m map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		switch vv := v.(type) {
		case nil:
			continue
		case string:
			if vv == "" {
				continue
			}
		case int64:
			if vv == 0 {
				continue
			}
		case bool:
			if !vv {
				continue
			}
		default:
			// typed nil（如未填的 map[string]any 字段）不命中 case nil——
			// 保留会 marshal 成 null，Telegram 报 "object expected as reply markup"。
			if rv := reflect.ValueOf(v); (rv.Kind() == reflect.Map || rv.Kind() == reflect.Slice ||
				rv.Kind() == reflect.Ptr || rv.Kind() == reflect.Interface) && rv.IsNil() {
				continue
			}
		}
		out[k] = v
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
