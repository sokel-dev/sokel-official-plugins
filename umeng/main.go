// umeng is a first-party Sokel plugin: Umeng push (U-Push), the Umeng half of the
// dual-channel app push setup.
//
// The interface details are modeled on a push service already running in production
// (only the interface knowledge is borrowed, no code is copied):
//   - Signature: MD5("POST" + full URL + body + master_secret), appended as ?sign=
//   - /api/send (unicast/listcast/broadcast), /api/status, /api/cancel
//   - Android and iOS payload shapes are completely different (Android's own format
//     vs. iOS's APNs aps structure)
//
// Run: SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./umeng
package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
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

var apiBase = "https://msgapi.umeng.com" // overridden in tests

func main() {
	token := sokel.Env("TOKEN")
	if token == "" && sokel.Env("DEPLOY_KEY") == "" {
		log.Fatal("请设置 SOKEL_TOKEN(接入组「接入命令」里复制);随部署托管的容器可改设 SOKEL_DEPLOY_KEY 自动注册")
	}
	p := sokel.New(sokel.Config{
		Endpoint: env("SOKEL_ENDPOINT", "http://localhost:8088"),
		Token:    token,
		Name:     "umeng",
	})
	RegisterCredential(p)
	p.SetDoc(usageDoc, "")

	OnPush(p, opPush)
	OnTaskStatus(p, opTaskStatus)
	OnCancel(p, opCancel)
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

func credOf(ctx plugin.Ctx) Cred {
	var c Cred
	sokel.BindCredential(ctx, &c)
	return c
}

// appOf returns the key pair for the given platform (Android and iOS are two separate
// apps in Umeng).
func appOf(cred Cred, platform string) (appKey, secret string, err error) {
	if platform == "ios" {
		appKey, secret = strings.TrimSpace(cred.IosAppKey), strings.TrimSpace(cred.IosMasterSecret)
		if appKey == "" || secret == "" {
			return "", "", fmt.Errorf("凭证没配 iOS 应用（ios_app_key/master_secret）——友盟里 iOS 是单独的应用")
		}
		return appKey, secret, nil
	}
	appKey, secret = strings.TrimSpace(cred.AndroidAppKey), strings.TrimSpace(cred.AndroidMasterSecret)
	if appKey == "" || secret == "" {
		return "", "", fmt.Errorf("凭证没配 Android 应用（android_app_key/master_secret）——友盟控制台该应用的信息页")
	}
	return appKey, secret, nil
}

// sign computes the Umeng signature: MD5("POST" + full URL + body + master_secret).
func sign(fullURL, body, secret string) string {
	h := md5.New()
	_, _ = io.WriteString(h, "POST"+fullURL+body+secret)
	return hex.EncodeToString(h.Sum(nil))
}

// umResp is Umeng's unified response envelope.
type umResp struct {
	Ret  string `json:"ret"` // SUCCESS / FAIL
	Data struct {
		MsgID     string `json:"msg_id"`
		TaskID    string `json:"task_id"`
		Status    int    `json:"status"`
		SentCount int    `json:"sent_count"`
		OpenCount int    `json:"open_count"`
		ErrorCode string `json:"error_code"`
		ErrorMsg  string `json:"error_msg"`
	} `json:"data"`
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

func post(ctx plugin.Ctx, path, secret string, body map[string]any) (*umResp, error) {
	b, _ := json.Marshal(body)
	full := apiBase + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		full+"?sign="+sign(full, string(b), secret), bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连接友盟失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var r umResp
	if json.Unmarshal(raw, &r) != nil {
		return nil, fmt.Errorf("友盟应答解不开（HTTP %d，前 200 字：%.200s）", resp.StatusCode, string(raw))
	}
	if r.Ret != "SUCCESS" {
		return &r, umErr(r.Data.ErrorCode, r.Data.ErrorMsg)
	}
	return &r, nil
}

// umErr maps frequent error codes to actionable next steps.
func umErr(code, msg string) error {
	switch code {
	case "1001", "1002", "1003":
		return fmt.Errorf("友盟不认这个请求（code %s: %s）——appkey/master secret 核对，注意 Android 与 iOS 是两个应用", code, msg)
	case "1007":
		return fmt.Errorf("device_token 不合法（code %s）——Android 是 44 位，iOS 是 64 位十六进制", code)
	case "2000":
		// 2000 is Umeng's catch-all code: task not found, app disabled, and invalid
		// appkey all return it, so **the only way to tell them apart is error_msg**
		// (observed in practice: a fake appKey returns 2000 + "该应用已被禁用").
		return fmt.Errorf("友盟拒绝了请求（code 2000：%s）——task_id 或 appkey/secret 核对；单播消息类没有任务统计", msg)
	}
	return fmt.Errorf("友盟返回错误（code %s）：%s", code, msg)
}

// —— Operations ——

func opPush(ctx plugin.Ctx, in *PushIn) (*PushOut, error) {
	title, body := strings.TrimSpace(in.Title), strings.TrimSpace(in.Body)
	if title == "" || body == "" {
		return nil, fmt.Errorf("标题与内容都要填")
	}
	platform := strings.ToLower(strings.TrimSpace(in.Platform))
	if platform == "" {
		platform = "android"
	}
	appKey, secret, err := appOf(credOf(ctx), platform)
	if err != nil {
		return nil, err
	}
	tokens := splitTokens(in.DeviceTokens)
	if len(tokens) > 500 {
		return nil, fmt.Errorf("device_token 一次最多 500 个（给了 %d）——分批发", len(tokens))
	}
	castType := "broadcast"
	switch {
	case len(tokens) == 1:
		castType = "unicast"
	case len(tokens) > 1:
		castType = "listcast"
	}
	req := map[string]any{
		"appKey": appKey, "timestamp": time.Now().Unix(),
		"type": castType, "production_mode": in.Production,
		"description": title,
	}
	if len(tokens) > 0 {
		req["device_tokens"] = strings.Join(tokens, ",")
	}
	// The payload shape is completely different between the two platforms: Android
	// uses its own format, iOS uses APNs' aps structure.
	if platform == "ios" {
		aps := map[string]any{"alert": map[string]any{"title": title, "body": body}}
		pl := map[string]any{"aps": aps}
		for k, v := range in.Extras {
			pl[k] = v // on iOS, custom keys sit alongside aps at the top level
		}
		req["payload"] = pl
	} else {
		payload := map[string]any{
			"display_type": "notification",
			"body":         map[string]any{"title": title, "text": body, "ticker": title, "after_open": "go_app"},
		}
		if len(in.Extras) > 0 {
			payload["extra"] = in.Extras
		}
		req["payload"] = payload
	}
	r, err := post(ctx, "/api/send", secret, req)
	if err != nil {
		return nil, err
	}
	id := r.Data.TaskID
	if id == "" {
		id = r.Data.MsgID
	}
	return &PushOut{TaskID: id}, nil
}

func splitTokens(s string) []string {
	var out []string
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

var statusText = map[int]string{
	0: "排队中", 1: "发送中", 2: "发送完成", 3: "发送失败",
	4: "已撤销", 5: "已过期", 6: "筛选结果为空", 7: "定时任务未开始",
}

func opTaskStatus(ctx plugin.Ctx, in *TaskStatusIn) (*TaskStatusOut, error) {
	taskID := strings.TrimSpace(in.TaskID)
	if taskID == "" {
		return nil, fmt.Errorf("任务 ID 是空的（推送操作的产出；注意单播消息类没有任务统计）")
	}
	appKey, secret, err := appOf(credOf(ctx), strings.ToLower(in.Platform))
	if err != nil {
		return nil, err
	}
	r, err := post(ctx, "/api/status", secret, map[string]any{
		"appKey": appKey, "timestamp": time.Now().Unix(), "task_id": taskID,
	})
	if err != nil {
		return nil, err
	}
	return &TaskStatusOut{Status: r.Data.Status, StatusText: statusText[r.Data.Status],
		SentCount: r.Data.SentCount, OpenCount: r.Data.OpenCount}, nil
}

func opCancel(ctx plugin.Ctx, in *CancelIn) (*CancelOut, error) {
	taskID := strings.TrimSpace(in.TaskID)
	if taskID == "" {
		return nil, fmt.Errorf("任务 ID 是空的")
	}
	appKey, secret, err := appOf(credOf(ctx), strings.ToLower(in.Platform))
	if err != nil {
		return nil, err
	}
	if _, err := post(ctx, "/api/cancel", secret, map[string]any{
		"appKey": appKey, "timestamp": time.Now().Unix(), "task_id": taskID,
	}); err != nil {
		return nil, err
	}
	return &CancelOut{OK: true}, nil
}

// opHealthCheck queries the status of a nonexistent task ID: wrong credentials return
// 1002/1003 (exposing the problem immediately), correct credentials return "task not
// found" (= healthy). No real push is sent.
func opHealthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	cred := credOf(ctx)
	var parts []string
	checked := false
	for _, pf := range []string{"android", "ios"} {
		appKey, secret, err := appOf(cred, pf)
		if err != nil {
			continue
		}
		checked = true
		r, perr := post(ctx, "/api/status", secret, map[string]any{
			"appKey": appKey, "timestamp": time.Now().Unix(), "task_id": "healthcheck-nonexistent",
		})
		// 2000 is a catch-all code, so **the judgment must use Umeng's raw error_msg**:
		// containing "任务"/"task" = credentials are valid (just no such task ID);
		// "disabled"/"invalid"/"auth" = credentials are wrong. Both past failures
		// happened here: (1) judging by error_code let a fake appKey through, since it
		// also returns 2000; (2) judging by a translated error string let it through
		// too, since the translated text happens to contain "task_id". The judgment
		// must only trust the upstream's raw text.
		if perr != nil {
			raw := ""
			if r != nil {
				raw = r.Data.ErrorMsg
			}
			ok := strings.Contains(raw, "任务") || strings.Contains(strings.ToLower(raw), "task")
			if !ok {
				return &HealthCheckOut{OK: false, Message: pf + "：" + perr.Error()}, nil
			}
		}
		parts = append(parts, pf+" 可用")
	}
	if !checked {
		return &HealthCheckOut{OK: false, Message: "凭证一个平台都没配——Android 或 iOS 至少一组"}, nil
	}
	return &HealthCheckOut{OK: true, Message: strings.Join(parts, "；")}, nil
}
