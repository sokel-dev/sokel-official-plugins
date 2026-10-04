// feishu-webhook — a Feishu group custom bot (the shortest path to sending a message to one group).
//
// A separate plugin from the feishu main plugin (self-built app): the credential shape and
// authorization scope are completely different, and shouldn't be mixed into one credential pool.
// See the top comment in schema/schema.go for the design rationale.
//
// Run: SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./feishu-webhook
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

// sign computes Feishu's custom-bot signature: HmacSHA256(key=timestamp+"\n"+secret, data=empty)
// → base64. **Note that key and data are swapped from what intuition suggests** — the secret goes
// in the key, and the data being signed is an empty string. This is what Feishu's docs specify;
// don't "fix" it based on the usual HMAC intuition.
func sign(secret string, ts int64) string {
	mac := hmac.New(sha256.New, []byte(fmt.Sprintf("%d\n%s", ts, secret)))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// hookResp is the response shape: the new version uses {code,msg}, the old version uses
// {StatusCode,StatusMessage}, and both are recognized.
type hookResp struct {
	Code          int    `json:"code"`
	Msg           string `json:"msg"`
	StatusCode    int    `json:"StatusCode"`
	StatusMessage string `json:"StatusMessage"`
	httpStatus    int    // the HTTP-layer status (not part of the JSON)
}

// postRaw fires a single webhook request and returns a **structured** response (only a connection
// failure goes through error). The health check needs to classify precisely by code/msg, not do
// substring matching on an already-assembled error string — that's exactly the root cause of the
// "always green" bug (issue #13).
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

// post fires a single webhook request and interprets the response (used by the send side; the
// health check uses postRaw and classifies the response itself).
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

// opHealthCheck sends a validation request with **empty content**, classifies it by structured
// code/msg, and never actually posts a message into the group.
//
// The order of checks matters here: Feishu **checks the token before it checks the content**
// (observed: an invalid token returns 19001 + "param invalid: incoming webhook access token
// invalid" no matter what shape the body is). So:
//   - 19001 with msg mentioning access token → the URL is dead (bot removed / token pasted wrong)
//     — the old substring allowlist ("19001"/"param") happened to classify exactly this case as
//     green, which is the smoking gun for the "always green" bug (issue #13);
//   - 19021 → the signature secret is wrong, the credential is broken;
//   - any other non-zero parameter-type error → the token check already passed and it's on to the
//     content check = the URL is alive. This branch doesn't depend on the specific shape of the
//     empty-content response: reaching the content check at all is itself the evidence of being alive.
func opHealthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	r, err := postRaw(ctx, map[string]any{"msg_type": "text", "content": map[string]any{}})
	if err != nil {
		return &HealthCheckOut{OK: false, Message: err.Error()}, nil
	}
	code := r.Code
	if code == 0 {
		code = r.StatusCode // old-version response field
	}
	msg := r.Msg + r.StatusMessage
	switch {
	case code == 0 && r.httpStatus == http.StatusOK:
		// An empty text message succeeding shouldn't happen; treat it as available.
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
