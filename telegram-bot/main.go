// telegram-bot — a Sokel first-party deployable plugin: the "send/operate" side of the Telegram
// Bot API.
//
// Scope (see docs/telegram-integration.md): a "full-featured" TG bot splits into two halves:
//   - Send/operate (this plugin): sendMessage/sendPhoto/answerCallbackQuery/getFile… are pure
//     outbound calls to api.telegram.org, need no new platform feature, and can be built as an SDK
//     plugin (same shape as http-egress — outbound goes through the broker, can run on a public
//     server).
//   - Receive/trigger (not in this plugin): receiving a message → kicking off a workflow needs the
//     platform's "event trigger" runtime or a publicly reachable webhook; see doc §3, still pending.
//
// Design: one generic call covers the [entire] Bot API (method + params, zero code change for new
// methods) plus a set of typed convenience operations (friendly for canvas fields).
// Credential = bot_token (secret); the token is only spliced into the URL path inside the plugin,
// never into any node input/output (to avoid leaking it into the canvas or logs).
//
// Run: SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx SOKEL_NATS_TOKEN=xxx ./telegram-bot
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
	RegisterCredential(p)  // credential contract (generated from the schema declaration; Cred lives in zz_credential.go)
	p.SetDoc(usageDoc, "") // usage doc (docs/*.md): how to get credentials, what the gotchas are; reported to the platform with the handshake

	// Generic catch-all: covers the entire Bot API (any method + any JSON params).
	OnCall(p, opCall)

	// —— High-frequency typed convenience operations (friendly for canvas fields; all go through the same callAPI internally) ——
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
	OnHealthCheck(p, opHealthCheck) // the "check" button on the credential page calls this (id must be health_check)
	OnDownloadFile(p, opDownloadFile)

	// —— Bot configuration operations (paving the way for webhook mode + command menu) ——
	OnSetWebhook(p, opSetWebhook)
	OnDeleteWebhook(p, opDeleteWebhook)
	OnGetWebhookInfo(p, opGetWebhookInfo)
	OnSetMyCommands(p, opSetMyCommands)

	// —— Event contracts (receiving side): declares the event types this bot produces + each one's payload (the frontend's trigger_event node derives its outgoing handles from this) ——
	DeclareEvents(p)
	// Shared field: every event payload has chat_id → flattened to the top level of the input when
	// the platform triggers ({{node.chat_id}}), so every event branch shares the same variable
	// instead of having to drill into its own event payload. The SDK validates strictly (a missing
	// field or type mismatch is an error).

	// Standing event source: long-polls getUpdates → pushes each update to the platform via Trigger,
	// by type (see updates.go). Long polling needs no setWebhook and doesn't require the platform to
	// be publicly reachable (works behind NAT); recommended to run a single replica (the platform
	// dedupes by update_id as a backstop).
	sokel.RegisterSource(p, sokel.Source{ID: "updates", Label: "TG 更新长轮询"}, runUpdatesSource)

	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}

// —— Generic call ——

func opCall(ctx plugin.Ctx, in *CallIn) (*CallOut, error) {
	res, err := callAPI(ctx, in.Method, in.Params)
	if err != nil {
		return nil, err
	}
	return remap[CallOut](map[string]any{"ok": true, "result": res})
}

// —— Typed convenience operations ——

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

// opHealthCheck checks the credential — fires a single getMe.
//
// getMe sends no message and touches no conversation; it's the only call in the Bot API that
// "only verifies the token". The username it returns can also catch a "token copied from the
// wrong bot" mistake — a test bot's token and a production bot's token look identical, and just
// reporting "reachable" wouldn't reveal which one is misconfigured.
//
// When the token is wrong, this returns ok=false + message, **not** an error: the platform treats
// an error as "this plugin can't run its health check" and ok=false as "the check concluded the
// plugin is unavailable" — and Telegram's 401 Unauthorized needs to actually be visible.
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

// —— Shared ——

// messageOut / okOut: several operations share the same output shape, but **the generated code
// gives each operation its own type** (same contract name doesn't mean same Go type). So the
// helper returns a map, and each handler remaps it to its own type — cheaper than writing a
// per-output-type helper, and no need to bring in generics either.
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

// remap: map → the generated typed output. Uses contract.BindInput rather than json.Unmarshal —
// the latter only honors json tags, and silently binds nothing when the contract name isn't
// snake_case.
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

// botToken fetches the bot token on the operation side — first the credential injected by the
// platform (supports multiple bots: configure a different credential per workflow), falling back
// to the TELEGRAM_BOT_TOKEN environment variable (single-bot deployment / shared with the event source).
func botToken(ctx sokel.Ctx) string {
	if c := sokel.CredentialAs[Cred](ctx); c.BotToken != "" {
		return c.BotToken
	}
	return os.Getenv("TELEGRAM_BOT_TOKEN")
}

// callAPI is the operation-side call (token taken from the credential / env).
func callAPI(ctx sokel.Ctx, method string, params map[string]any) (any, error) {
	tok := botToken(ctx)
	if tok == "" {
		return nil, fmt.Errorf("缺少 bot_token（配置插件凭证，或设 TELEGRAM_BOT_TOKEN 环境变量）")
	}
	return callTelegram(ctx, tok, method, params)
}

// callTelegram: POST https://api.telegram.org/bot<token>/<method>, JSON body=params, parses
// {ok,result,description}. The token only ever goes into the URL path, never into any return
// value. Shared low-level plumbing for both the event source and the operations.
func callTelegram(ctx context.Context, token, method string, params map[string]any) (any, error) {
	if params == nil {
		params = map[string]any{}
	}
	buf, _ := json.Marshal(params)
	url := fmt.Sprintf("https://api.telegram.org/bot%s/%s", token, method)
	reqCtx, cancel := context.WithTimeout(ctx, 40*time.Second) // getUpdates long-polls with timeout=25s, leave headroom
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

// compact drops zero-value keys (Telegram is picky about empty-string parse_mode / 0 reply_to and
// the like; omitting them falls back to the default).
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
			// A typed nil (e.g. an unfilled map[string]any field) doesn't match case nil —
			// keeping it would marshal to null, and Telegram reports "object expected as reply markup".
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
