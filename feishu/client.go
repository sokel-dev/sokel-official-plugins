package main

// SDK client 管理。**这是全站第一个引厂商 SDK 的插件**，理由要说清楚：
//
// 此前所有插件都是裸 HTTP——够用、依赖面小。飞书破例，因为它有两样裸 HTTP 不划算的东西：
//
//   - **长连接事件订阅（larkws）**：事件不走公网 webhook，插件主动向飞书建一条
//     WebSocket，事件从这条连接推下来。不要公网 IP、不要解密验签——与本平台
//     「插件出站接 broker」的哲学完全同构。但帧协议是飞书私有的（protobuf），
//     自己实现又脆又不值。
//   - **tenant_access_token 生命周期**：获取/缓存/过期刷新，SDK 内置。
//
// 用法上仍然克制：REST 一律走 raw `client.Do`（路径/请求体自己拼，风格与其他插件
// 一致，call 保底操作天然同一条通道）；typed 模块只用在 multipart 上传
// （im 图片/文件、drive 上传）——那正是 SDK 处理得最值的地方。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

// Cred 由 sokel-gen 从 schema.Credential 生成（zz_credential.go）。

func baseURL(domain string) string {
	switch {
	case domain == "lark":
		return lark.LarkBaseUrl
	case strings.HasPrefix(domain, "http://"), strings.HasPrefix(domain, "https://"):
		// 完整地址原样透传：测试指向 httptest 假飞书全靠它；生产上凭证是下拉框，
		// 不会出现第三种值，但私有化部署的飞书（KA 专属域名）将来也走这条。
		return strings.TrimRight(domain, "/")
	}
	return lark.FeishuBaseUrl
}

// clientCache：按 app_id 缓存 SDK client。client 内部持有 token 缓存，
// 每次调用都新建等于每次都重新换 token——那是给飞书的频控白送子弹。
var (
	cliMu    sync.Mutex
	cliCache = map[string]*lark.Client{}
)

func clientOf(cred Cred) (*lark.Client, error) {
	appID, secret := strings.TrimSpace(cred.AppID), strings.TrimSpace(cred.AppSecret)
	if appID == "" || secret == "" {
		return nil, fmt.Errorf("凭证缺 app_id/app_secret（开放平台「凭证与基础信息」页）")
	}
	// key 纳入 secret 摘要（issue #13 ③）：曾按 app_id|domain 缓存，用户改对了
	// 密钥后拿到的还是旧 secret 的 client——体检恒红直到进程重启。反向（密钥在
	// 飞书侧被吊销）SDK 的 token 缓存仍可能撑约 2 小时，那是 token 缓存的固有
	// 窗口；健康检查已改走直换 token 不受它影响（misc.go opHealthCheck）。
	sum := sha256.Sum256([]byte(secret))
	key := appID + "|" + cred.Domain + "|" + hex.EncodeToString(sum[:8])
	cliMu.Lock()
	defer cliMu.Unlock()
	if c, ok := cliCache[key]; ok {
		return c, nil
	}
	c := lark.NewClient(appID, secret,
		lark.WithOpenBaseUrl(baseURL(cred.Domain)),
		lark.WithEnableTokenCache(true),
	)
	cliCache[key] = c
	return c, nil
}

// feishuResp 开放平台统一应答壳。
type feishuResp struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

// callRaw 直调开放平台任意接口。token 注入/刷新由 SDK 管；
// 返回时**业务码非 0 一律转成错误**——飞书的失败大多是 HTTP 200 + code!=0，
// 不在这里拦的话每个操作都要各查一遍，漏一处就是静默失败。
func callRaw(ctx context.Context, cred Cred, method, path string, body any) (json.RawMessage, error) {
	c, err := clientOf(cred)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(ctx, &larkcore.ApiReq{
		HttpMethod:                method,
		ApiPath:                   path,
		Body:                      body,
		SupportedAccessTokenTypes: []larkcore.AccessTokenType{larkcore.AccessTokenTypeTenant},
	})
	if err != nil {
		return nil, connErr(err)
	}
	var r feishuResp
	if uerr := json.Unmarshal(resp.RawBody, &r); uerr != nil {
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("飞书返回 HTTP %d：%s", resp.StatusCode, clip(resp.RawBody, 200))
		}
		return nil, fmt.Errorf("飞书应答解不开（前 200 字：%s）", clip(resp.RawBody, 200))
	}
	if r.Code != 0 {
		return nil, feishuErr(r.Code, r.Msg)
	}
	if len(r.Data) == 0 {
		// 少数历史接口（bot/v3/info）不带 data 壳，字段直接在顶层——原样给回去。
		return resp.RawBody, nil
	}
	return r.Data, nil
}

// callRawFull 同 callRaw，但**不把业务码转错误**——call 保底操作用：
// 直调文档接口的人要原样 code/msg 对照排错，包装反而碍事。
func callRawFull(ctx context.Context, cred Cred, method, path string, body any) (*feishuResp, error) {
	c, err := clientOf(cred)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(ctx, &larkcore.ApiReq{
		HttpMethod: method, ApiPath: path, Body: body,
		SupportedAccessTokenTypes: []larkcore.AccessTokenType{larkcore.AccessTokenTypeTenant},
	})
	if err != nil {
		return nil, connErr(err)
	}
	var r feishuResp
	if uerr := json.Unmarshal(resp.RawBody, &r); uerr != nil {
		return nil, fmt.Errorf("飞书返回 HTTP %d，应答不是 JSON（前 200 字：%s）", resp.StatusCode, clip(resp.RawBody, 200))
	}
	return &r, nil
}

// connErr SDK 层错误（token 换取失败在 c.Do **之前**发生，不经 feishuErr）。
// 换 token 的高频失败就是凭证错，把 SDK 的原文翻译成下一步。
func connErr(err error) error {
	msg := err.Error()
	if strings.Contains(msg, "code:10003") || strings.Contains(msg, "code:10014") ||
		strings.Contains(msg, "app not found") || strings.Contains(msg, "app secret invalid") {
		return fmt.Errorf("换 tenant_access_token 失败——app_id/app_secret 有误或应用被停用，"+
			"去开放平台「凭证与基础信息」核对（原文：%s）", msg)
	}
	return fmt.Errorf("连接飞书失败: %w", err)
}

// feishuErr 把高频业务码翻译成「下一步该做什么」。飞书的 msg 是给开发者看的英文，
// 而看到这条报错的多半是在画布上配节点的人。
func feishuErr(code int, msg string) error {
	switch code {
	case 99991663, 99991661, 99991668:
		return fmt.Errorf("飞书拒绝了应用身份（code %d: %s）——app_id/app_secret 有误或应用被停用，去开放平台「凭证与基础信息」核对", code, msg)
	case 99991672, 99991679:
		return fmt.Errorf("应用缺权限（code %d: %s）——去开放平台「权限管理」开通对应权限并**重新发布版本**", code, msg)
	case 230002:
		return fmt.Errorf("bot 不在这个会话里（code %d）——先把机器人拉进群，或确认 chat_id 没填错", code)
	case 230013:
		return fmt.Errorf("bot 能力未启用（code %d）——去开放平台「应用能力」添加「机器人」并发布版本", code)
	case 232009:
		return fmt.Errorf("对方关闭了机器人消息（code %d）", code)
	case 1254050, 1254005:
		return fmt.Errorf("多维表格拒绝访问（code %d: %s）——把表格分享给应用：表格右上角「…」→ 添加文档应用", code, msg)
	}
	return fmt.Errorf("飞书返回业务码 %d：%s", code, msg)
}

func clip(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// credOf / credOfSource：操作侧与事件源侧各取一次凭证（SDK 的两种 ctx 形态）。
func credOf(ctx plugin.Ctx) Cred {
	var c Cred
	sokel.BindCredential(ctx, &c)
	return c
}
