// submail —— Sokel 第一方插件：SUBMAIL（赛邮）国内短信 + 国际短信。
//
// 纯 HTTP 表单接口（api.mysubmail.com），无 SDK。设计判断见 schema/schema.go 顶注。
// 鉴权用明文 appkey 模式（signature = appkey）：平台到 SUBMAIL 全程 HTTPS，
// 摘要签名模式防的「传输中窥视」在这里不成立，而它换来的是时间戳对表的脆弱性。
//
// 运行：SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./submail
package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

var (
	apiBase   = "https://api.mysubmail.com"    // 发送/余额（测试替换）
	apiBaseV4 = "https://api-v4.mysubmail.com" // 发送状态查询只在 v4 网关有（实测老网关 Unknown method）
)

func main() {
	token := sokel.Env("TOKEN")
	if token == "" && sokel.Env("DEPLOY_KEY") == "" {
		log.Fatal("请设置 SOKEL_TOKEN(接入组「接入命令」里复制);随部署托管的容器可改设 SOKEL_DEPLOY_KEY 自动注册")
	}
	p := sokel.New(sokel.Config{
		Endpoint: env("SOKEL_ENDPOINT", "http://localhost:8088"),
		Token:    token,
		Name:     "submail",
	})
	RegisterCredential(p)
	p.SetDoc(usageDoc, "")

	OnSmsSend(p, opSmsSend)
	OnSmsXsend(p, opSmsXsend)
	OnIntlSend(p, opIntlSend)
	OnIntlXsend(p, opIntlXsend)
	OnBalance(p, opBalance)
	OnSmsLog(p, opSmsLog)
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

// appOf 取国内/国际对应的那对钥匙；缺的那组给指路报错。
func appOf(cred Cred, intl bool) (appid, appkey string, err error) {
	if intl {
		appid, appkey = strings.TrimSpace(cred.IntlAppid), strings.TrimSpace(cred.IntlAppkey)
		if appid == "" || appkey == "" {
			return "", "", fmt.Errorf("凭证没配国际短信应用（intl_appid/intl_appkey）——SUBMAIL 控制台里「国际短信」是单独的应用，创建后把两个值填进凭证")
		}
		return appid, appkey, nil
	}
	appid, appkey = strings.TrimSpace(cred.SmsAppid), strings.TrimSpace(cred.SmsAppkey)
	if appid == "" || appkey == "" {
		return "", "", fmt.Errorf("凭证没配国内短信应用（sms_appid/sms_appkey）——SUBMAIL 控制台创建「短信」应用后把两个值填进凭证")
	}
	return appid, appkey, nil
}

// subResp SUBMAIL 统一应答。
type subResp struct {
	Status string `json:"status"` // success / error
	Code   int    `json:"code"`
	Msg    string `json:"msg"`
	SendID string `json:"send_id"`
	Fee    int    `json:"fee"`
	// balance 接口
	Balance       json.Number `json:"balance"`
	Transactional json.Number `json:"transactional_balance"`
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

// post 表单调用。明文鉴权：signature = appkey。
func post(ctx plugin.Ctx, path string, form url.Values) (*subResp, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+path, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连接 SUBMAIL 失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var r subResp
	if json.Unmarshal(raw, &r) != nil {
		return nil, fmt.Errorf("SUBMAIL 应答解不开（HTTP %d，前 200 字：%.200s）", resp.StatusCode, string(raw))
	}
	if r.Status != "success" {
		return nil, subErr(r.Code, r.Msg)
	}
	return &r, nil
}

// subErr 高频错误码 → 下一步做什么。
func subErr(code int, msg string) error {
	switch code {
	case 101, 102, 103, 104, 109, 110:
		// 109/110 是签名/appkey 校验失败——实测假钥匙回 109 且 msg 为空。
		return fmt.Errorf("SUBMAIL 不认这对 appid/appkey（code %d: %s）——控制台「应用集成」核对，注意国内与国际是两个应用", code, msg)
	case 105, 119:
		return fmt.Errorf("应用被禁用或 IP 不在白名单（code %d: %s）——控制台该应用的设置里检查", code, msg)
	case 152:
		return fmt.Errorf("余额不足（code %d）——去 SUBMAIL 充值", code)
	case 254, 255:
		return fmt.Errorf("内容触发风控或缺已报备的签名（code %d: %s）——国内短信内容要以【已报备签名】开头", code, msg)
	case 402:
		return fmt.Errorf("手机号格式不对（code %d: %s）——国内 11 位裸号，国际带 + 国家码", code, msg)
	case 406:
		return fmt.Errorf("模板不存在或未过审（code %d: %s）——核对模板 ID，模板要在控制台审核通过后才能用", code, msg)
	}
	return fmt.Errorf("SUBMAIL 返回错误 %d：%s", code, msg)
}

// —— 操作 ——

func opSmsSend(ctx plugin.Ctx, in *SmsSendIn) (*SmsSendOut, error) {
	to, content := strings.TrimSpace(in.To), strings.TrimSpace(in.Content)
	if to == "" || content == "" {
		return nil, fmt.Errorf("手机号与内容都要填")
	}
	// 没带【签名】九成会被运营商拒收，而 SUBMAIL 的报错要等回执才知道——提前拦。
	if !strings.Contains(content, "【") || !strings.Contains(content, "】") {
		return nil, fmt.Errorf("内容缺短信签名——国内短信必须带已报备的【签名】（通常放开头），否则运营商直接拒收")
	}
	appid, appkey, err := appOf(credOf(ctx), false)
	if err != nil {
		return nil, err
	}
	r, err := post(ctx, "/sms/send", url.Values{
		"appid": {appid}, "signature": {appkey}, "to": {to}, "content": {content},
	})
	if err != nil {
		return nil, err
	}
	return &SmsSendOut{SendID: r.SendID, Fee: r.Fee}, nil
}

func opSmsXsend(ctx plugin.Ctx, in *SmsXsendIn) (*SmsXsendOut, error) {
	to, project := strings.TrimSpace(in.To), strings.TrimSpace(in.Project)
	if to == "" || project == "" {
		return nil, fmt.Errorf("手机号与模板 ID 都要填")
	}
	appid, appkey, err := appOf(credOf(ctx), false)
	if err != nil {
		return nil, err
	}
	form := url.Values{"appid": {appid}, "signature": {appkey}, "to": {to}, "project": {project}}
	if len(in.Vars) > 0 {
		b, _ := json.Marshal(in.Vars)
		form.Set("vars", string(b))
	}
	r, err := post(ctx, "/sms/xsend", form)
	if err != nil {
		return nil, err
	}
	return &SmsXsendOut{SendID: r.SendID, Fee: r.Fee}, nil
}

func opIntlSend(ctx plugin.Ctx, in *IntlSendIn) (*IntlSendOut, error) {
	to, content := strings.TrimSpace(in.To), strings.TrimSpace(in.Content)
	if to == "" || content == "" {
		return nil, fmt.Errorf("手机号与内容都要填")
	}
	if !strings.HasPrefix(to, "+") {
		return nil, fmt.Errorf("国际短信号码要带 + 国家码（如 +818012345678）；发国内号码用「国内短信·发送」")
	}
	appid, appkey, err := appOf(credOf(ctx), true)
	if err != nil {
		return nil, err
	}
	r, err := post(ctx, "/internationalsms/send", url.Values{
		"appid": {appid}, "signature": {appkey}, "to": {to}, "content": {content},
	})
	if err != nil {
		return nil, err
	}
	return &IntlSendOut{SendID: r.SendID, Fee: r.Fee}, nil
}

func opIntlXsend(ctx plugin.Ctx, in *IntlXsendIn) (*IntlXsendOut, error) {
	to, project := strings.TrimSpace(in.To), strings.TrimSpace(in.Project)
	if to == "" || project == "" {
		return nil, fmt.Errorf("手机号与模板 ID 都要填")
	}
	if !strings.HasPrefix(to, "+") {
		return nil, fmt.Errorf("国际短信号码要带 + 国家码")
	}
	appid, appkey, err := appOf(credOf(ctx), true)
	if err != nil {
		return nil, err
	}
	form := url.Values{"appid": {appid}, "signature": {appkey}, "to": {to}, "project": {project}}
	if len(in.Vars) > 0 {
		b, _ := json.Marshal(in.Vars)
		form.Set("vars", string(b))
	}
	r, err := post(ctx, "/internationalsms/xsend", form)
	if err != nil {
		return nil, err
	}
	return &IntlXsendOut{SendID: r.SendID, Fee: r.Fee}, nil
}

// opBalance 国内与国际各查各的端点（**端点不同、计量也不同**：国内按条、国际按金额）。
// 配了哪组查哪边；一组都没配才报错。
func opBalance(ctx plugin.Ctx, _ *BalanceIn) (*BalanceOut, error) {
	cred := credOf(ctx)
	out := &BalanceOut{}
	queried := false
	if appid, appkey, err := appOf(cred, false); err == nil {
		queried = true
		r, err := post(ctx, "/balance/sms", url.Values{"appid": {appid}, "signature": {appkey}})
		if err != nil {
			return nil, fmt.Errorf("国内余额: %w", err)
		}
		b, _ := r.Balance.Int64()
		t, _ := r.Transactional.Int64()
		out.Balance, out.Transactional = int(b), int(t)
	}
	if appid, appkey, err := appOf(cred, true); err == nil {
		queried = true
		r, err := post(ctx, "/balance/internationalsms", url.Values{"appid": {appid}, "signature": {appkey}})
		if err != nil {
			return nil, fmt.Errorf("国际余额: %w", err)
		}
		out.IntlBalance, _ = r.Balance.Float64()
	}
	if !queried {
		return nil, fmt.Errorf("凭证一组应用都没配——至少填国内或国际一组")
	}
	return out, nil
}

// logResp /sms/log 的应答：results 数组原样透传（形状由 SUBMAIL 定义）。
type logResp struct {
	Status  string           `json:"status"`
	Code    int              `json:"code"`
	Msg     string           `json:"msg"`
	Results []map[string]any `json:"results"`
}

// opSmsLog 查下发状态。**收单成功 ≠ 到手机**：签名未报备/内容风控/空号都在
// 这里的 dropped + report 里体现，同步应答看不到。
func opSmsLog(ctx plugin.Ctx, in *SmsLogIn) (*SmsLogOut, error) {
	sendID, to := strings.TrimSpace(in.SendID), strings.TrimSpace(in.To)
	if sendID == "" && to == "" {
		return nil, fmt.Errorf("send_id 与手机号至少填一个（send_id 是发送操作的产出）")
	}
	appid, appkey, err := appOf(credOf(ctx), false)
	if err != nil {
		return nil, err
	}
	days := in.Days
	if days <= 0 {
		days = 1
	}
	now := time.Now()
	form := url.Values{
		"appid": {appid}, "signature": {appkey},
		"start_date": {now.AddDate(0, 0, -days+1).Format("2006-01-02")},
		"end_date":   {now.Format("2006-01-02")},
	}
	if sendID != "" {
		form.Set("send_id", sendID)
	}
	if to != "" {
		form.Set("to", to)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBaseV4+"/sms/log", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连接 SUBMAIL 失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	var r logResp
	if json.Unmarshal(raw, &r) != nil {
		return nil, fmt.Errorf("SUBMAIL 应答解不开（HTTP %d，前 200 字：%.200s）", resp.StatusCode, string(raw))
	}
	if r.Status != "success" {
		return nil, subErr(r.Code, r.Msg)
	}
	return &SmsLogOut{Logs: toAny(r.Results), Count: len(r.Results)}, nil
}

func toAny(in []map[string]any) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}

// opHealthCheck 配了哪组就查哪组，两组都配就都查——一组坏了要说清是哪组。
func opHealthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	cred := credOf(ctx)
	var parts []string
	checked := false
	if strings.TrimSpace(cred.SmsAppid) != "" {
		checked = true
		if _, err := post(ctx, "/balance/sms", url.Values{
			"appid": {strings.TrimSpace(cred.SmsAppid)}, "signature": {strings.TrimSpace(cred.SmsAppkey)},
		}); err != nil {
			return &HealthCheckOut{OK: false, Message: "国内短信应用：" + err.Error()}, nil
		}
		parts = append(parts, "国内短信可用")
	}
	if strings.TrimSpace(cred.IntlAppid) != "" {
		checked = true
		// 国际应用要打国际余额端点——拿国际钥匙打 /balance/sms 会把有效凭证误判成坏的。
		if _, err := post(ctx, "/balance/internationalsms", url.Values{
			"appid": {strings.TrimSpace(cred.IntlAppid)}, "signature": {strings.TrimSpace(cred.IntlAppkey)},
		}); err != nil {
			return &HealthCheckOut{OK: false, Message: "国际短信应用：" + err.Error()}, nil
		}
		parts = append(parts, "国际短信可用")
	}
	if !checked {
		return &HealthCheckOut{OK: false, Message: "凭证一组应用都没配——至少填国内（sms_appid/appkey）或国际（intl_appid/appkey）一组"}, nil
	}
	return &HealthCheckOut{OK: true, Message: strings.Join(parts, "；")}, nil
}
