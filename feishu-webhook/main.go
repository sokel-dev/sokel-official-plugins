// feishu-webhook —— 飞书群自定义机器人（发消息到一个群的最短路径）。
//
// 与 feishu 主插件（自建应用）是两个插件：凭证形态与授权范围完全不同，
// 不混在一个凭证池里。设计说明见 schema/schema.go 顶注。
//
// 运行：SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./feishu-webhook
package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

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
		Name:     "feishu-webhook",
	})
	RegisterCredential(p)
	p.SetDoc(usageDoc, "")
	OnWebhookSend(p, opSend)
	OnHealthCheck(p, opHealthCheck)
	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// sign 飞书自定义机器人的签名：HmacSHA256(key=timestamp+"\n"+secret, data=空) → base64。
// **注意 key 与 data 的位置和直觉相反**——secret 在 key 里、被签的数据是空串，
// 这是飞书文档定的，别按常规 HMAC 直觉「修好它」。
func sign(secret string, ts int64) string {
	mac := hmac.New(sha256.New, []byte(fmt.Sprintf("%d\n%s", ts, secret)))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// hookResp 应答：新版 {code,msg}，老版 {StatusCode,StatusMessage}，两种都认。
type hookResp struct {
	Code          int    `json:"code"`
	Msg           string `json:"msg"`
	StatusCode    int    `json:"StatusCode"`
	StatusMessage string `json:"StatusMessage"`
	httpStatus    int    // HTTP 层状态（不入 JSON）
}

// postRaw 发一次 webhook 请求，返回**结构化**应答（连接失败才走 error）。
// 健康检查要按 code/msg 精确分类，不能在拼好的错误字符串上做子串匹配——
// 那正是「恒绿」的病根（issue #13）。
func postRaw(ctx plugin.Ctx, body map[string]any) (hookResp, error) {
	cred := credOf(ctx)
	hook := strings.TrimSpace(cred.WebhookURL)
	if !strings.Contains(hook, "/open-apis/bot/") {
		return hookResp{}, fmt.Errorf("webhook 地址不像飞书群机器人的（应含 /open-apis/bot/v2/hook/…），检查是否粘错")
	}
	if sec := strings.TrimSpace(cred.Secret); sec != "" {
		ts := time.Now().Unix()
		body["timestamp"] = fmt.Sprintf("%d", ts)
		body["sign"] = sign(sec, ts)
	}
	buf, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hook, bytes.NewReader(buf))
	if err != nil {
		return hookResp{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return hookResp{}, fmt.Errorf("连接飞书失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var r hookResp
	_ = json.Unmarshal(raw, &r)
	r.httpStatus = resp.StatusCode
	return r, nil
}

// post 发一次 webhook 请求并解读应答（发送面用；健康检查走 postRaw 自行分类）。
func post(ctx plugin.Ctx, body map[string]any) error {
	r, err := postRaw(ctx, body)
	if err != nil {
		return err
	}
	switch {
	case r.Code == 0 && r.StatusCode == 0 && r.httpStatus == http.StatusOK:
		return nil
	case r.Code == 19021:
		return fmt.Errorf("签名校验失败——机器人开了「签名校验」但凭证里的密钥不对（或没填）")
	case r.Code == 9499:
		return fmt.Errorf("触发频控——自定义机器人限 100 条/分钟，降低发送频率")
	}
	return fmt.Errorf("飞书拒绝了消息（code %d/%d）：%s%s", r.Code, r.StatusCode, r.Msg, r.StatusMessage)
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

func opSend(ctx plugin.Ctx, in *WebhookSendIn) (*WebhookSendOut, error) {
	var body map[string]any
	switch {
	case len(in.Card) > 0:
		body = map[string]any{"msg_type": "interactive", "card": in.Card}
	case strings.TrimSpace(in.Markdown) != "":
		card := map[string]any{
			"config":   map[string]any{"wide_screen_mode": true},
			"elements": []any{map[string]any{"tag": "markdown", "content": in.Markdown}},
		}
		if t := strings.TrimSpace(in.Title); t != "" {
			card["header"] = map[string]any{
				"title": map[string]any{"tag": "plain_text", "content": t}, "template": "blue"}
		}
		body = map[string]any{"msg_type": "interactive", "card": card}
	case strings.TrimSpace(in.Text) != "":
		text := in.Text
		if in.AtAll {
			text += ` <at user_id="all">所有人</at>`
		}
		body = map[string]any{"msg_type": "text", "content": map[string]any{"text": text}}
	default:
		return nil, fmt.Errorf("text / markdown / card 至少填一个")
	}
	if err := post(ctx, body); err != nil {
		return nil, err
	}
	return &WebhookSendOut{OK: true}, nil
}

// opHealthCheck 发一个**空 content** 的校验请求，按结构化 code/msg 分类，不在群里发出消息。
//
// 判定次序有讲究：飞书的 **token 检查先于内容检查**（实测：伪 token 无论 body 什么形状
// 一律回 19001 + "param invalid: incoming webhook access token invalid"）。所以：
//   - 19001 且 msg 提到 access token → URL 死（机器人被删/token 粘错）——曾经的子串
//     白名单（"19001"/"param"）恰好把这条判成绿，正是「恒绿」实锤（issue #13）；
//   - 19021 → 签名密钥不对，凭证坏；
//   - 其余非 0 的参数类错误 → token 已过、轮到内容检查了 = URL 活。
//     这个分支不依赖空内容应答的具体形状：能走到内容检查本身就是活的证据。
func opHealthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	r, err := postRaw(ctx, map[string]any{"msg_type": "text", "content": map[string]any{}})
	if err != nil {
		return &HealthCheckOut{OK: false, Message: err.Error()}, nil
	}
	code := r.Code
	if code == 0 {
		code = r.StatusCode // 老版应答字段
	}
	msg := r.Msg + r.StatusMessage
	switch {
	case code == 0 && r.httpStatus == http.StatusOK:
		// 空文本竟然发成功了不该发生；按可用处理。
		return &HealthCheckOut{OK: true, Message: "webhook 可用"}, nil
	case code == 19001 && strings.Contains(strings.ToLower(msg), "access token"):
		return &HealthCheckOut{OK: false, Message: "webhook 无效——机器人可能已被移除，或 URL 粘错（飞书：" + msg + "）"}, nil
	case code == 19021:
		return &HealthCheckOut{OK: false, Message: "签名校验失败——机器人开了「签名校验」但凭证里的密钥不对（或没填）"}, nil
	default:
		return &HealthCheckOut{OK: true, Message: fmt.Sprintf("webhook 可用（校验请求按预期被参数检查挡下：code %d）", code)}, nil
	}
}

func credOf(ctx plugin.Ctx) Cred {
	var c Cred
	sokel.BindCredential(ctx, &c)
	return c
}
