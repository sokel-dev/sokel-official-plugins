package main

// Alibaba Cloud call layer. Two channels:
//
//   - **Generic call** (callACS): darabonba-openapi's unified signing gateway, {Endpoint, Action,
//     Version, params} dials any RPC product — DNS/RDS/ACK control plane/CloudMonitor/STS all go
//     through this one. We avoid pulling in per-product generated SDKs (one giant package per
//     product, when we only use two or three endpoints per vendor).
//   - **SLS** (slsClientOf): the log service has its own protocol and signing, via the official
//     aliyun-log-go-sdk.
//
// Clients are cached by (AK, endpoint): creating a new one wouldn't force a fresh handshake
// (AK signing is stateless), but building a darabonba client isn't cheap, and there's no reason
// to redo it.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"
	sls "github.com/aliyun/aliyun-log-go-sdk"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

func credOf(ctx plugin.Ctx) Cred {
	var c Cred
	sokel.BindCredential(ctx, &c)
	return c
}

// regionOf lets an operation-level region override the credential default; falls back to
// Hangzhou if neither is set — errors still carry "which region" info, so lookups don't
// silently hit the wrong place.
func regionOf(cred Cred, override string) string {
	if r := strings.TrimSpace(override); r != "" {
		return r
	}
	if r := strings.TrimSpace(cred.Region); r != "" {
		return r
	}
	return "cn-hangzhou"
}

var (
	acsMu    sync.Mutex
	acsCache = map[string]*openapi.Client{}
)

func acsClientOf(cred Cred, endpoint string) (*openapi.Client, error) {
	ak, sk := strings.TrimSpace(cred.AccessKeyID), strings.TrimSpace(cred.AccessKeySecret)
	if ak == "" || sk == "" {
		return nil, fmt.Errorf("凭证缺 access_key_id/access_key_secret（RAM 控制台创建，策略样例见使用说明）")
	}
	key := ak + "|" + endpoint
	acsMu.Lock()
	defer acsMu.Unlock()
	if c, ok := acsCache[key]; ok {
		return c, nil
	}
	c, err := openapi.NewClient(&openapi.Config{
		AccessKeyId: tea.String(ak), AccessKeySecret: tea.String(sk), Endpoint: tea.String(endpoint),
	})
	if err != nil {
		return nil, fmt.Errorf("构造阿里云 client 失败: %w", err)
	}
	acsCache[key] = c
	return c, nil
}

// callACS makes a generic call to an RPC Action. params always go in the query (the canonical
// place for Alibaba Cloud's RPC style).
func callACS(ctx context.Context, cred Cred, endpoint, action, version string, params map[string]any) (map[string]any, error) {
	c, err := acsClientOf(cred, endpoint)
	if err != nil {
		return nil, err
	}
	q := map[string]*string{}
	for k, v := range params {
		if v == nil {
			continue
		}
		switch t := v.(type) {
		case string:
			if t != "" {
				q[k] = tea.String(t)
			}
		default:
			q[k] = tea.String(fmt.Sprintf("%v", t))
		}
	}
	res, err := c.CallApi(&openapi.Params{
		Action: tea.String(action), Version: tea.String(version),
		Protocol: tea.String("HTTPS"), Method: tea.String("POST"), AuthType: tea.String("AK"),
		Style: tea.String("RPC"), Pathname: tea.String("/"),
		ReqBodyType: tea.String("formData"), BodyType: tea.String("json"),
	}, &openapi.OpenApiRequest{Query: q}, &dara.RuntimeOptions{})
	if err != nil {
		return nil, acsErr(action, err)
	}
	body, _ := res["body"].(map[string]any)
	if body == nil {
		// Some gateways flatten the body into the top level.
		body = res
	}
	return body, nil
}

// acsErr translates high-frequency error codes into "what to do next". Alibaba Cloud's raw
// errors are developer-facing English plus a RequestId, but the person seeing them is
// configuring a node on the canvas.
func acsErr(action string, err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "InvalidAccessKeyId"):
		return fmt.Errorf("阿里云不认这个 AccessKey（%s）——AK 填错或已删除，去 RAM 控制台核对", action)
	case strings.Contains(msg, "SignatureDoesNotMatch"), strings.Contains(msg, "IncompleteSignature"):
		return fmt.Errorf("签名校验失败（%s）——多半是 AccessKey Secret 粘错了（有多余空格/少了字符）", action)
	case strings.Contains(msg, "Forbidden.RAM"), strings.Contains(msg, "NoPermission"),
		strings.Contains(msg, "Forbidden.Unauthorized"):
		return fmt.Errorf("RAM 用户没有 %s 的权限——去 RAM 控制台给该用户加对应授权（最小权限策略样例见使用说明）", action)
	case strings.Contains(msg, "Throttling"):
		return fmt.Errorf("触发了阿里云频控（%s），稍后重试", action)
	case strings.Contains(msg, "InvalidDomainName.NoExist"):
		return fmt.Errorf("这个域名不在当前账号的云解析里（%s）——核对域名与账号", action)
	case strings.Contains(msg, "InvalidAppKey"):
		return fmt.Errorf("EMAS 不认这个 AppKey（%s）——用 EMAS 控制台该 App 的**数字 AppKey**，且 App 要属于当前账号", action)
	case strings.Contains(msg, "InvalidTarget"), strings.Contains(msg, "InvalidTargetValue"):
		return fmt.Errorf("推送目标不对（%s）——目标值与目标类型要匹配（设备 ID/账号/别名/标签），多个逗号分隔上限 1000", action)
	}
	return fmt.Errorf("阿里云 %s 失败: %w", action, err)
}

// —— SLS ——

var (
	slsMu    sync.Mutex
	slsCache = map[string]sls.ClientInterface{}
)

func slsClientOf(cred Cred, region string) (sls.ClientInterface, error) {
	ak, sk := strings.TrimSpace(cred.AccessKeyID), strings.TrimSpace(cred.AccessKeySecret)
	if ak == "" || sk == "" {
		return nil, fmt.Errorf("凭证缺 access_key_id/access_key_secret")
	}
	ep := region + ".log.aliyuncs.com"
	key := ak + "|" + ep
	slsMu.Lock()
	defer slsMu.Unlock()
	if c, ok := slsCache[key]; ok {
		return c, nil
	}
	c := sls.CreateNormalInterface(ep, ak, sk, "")
	slsCache[key] = c
	return c, nil
}

// slsErr translates errors for SLS's independent protocol.
func slsErr(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "ProjectNotExist"):
		return fmt.Errorf("SLS project 不存在——核对 project 名与 region（project 是 region 级资源，region 错了也报这个）")
	case strings.Contains(msg, "LogStoreNotExist"):
		return fmt.Errorf("logstore 不存在——核对名字（区分大小写）")
	case strings.Contains(msg, "Unauthorized"), strings.Contains(msg, "InvalidAccessKeyId"):
		return fmt.Errorf("SLS 拒绝了 AccessKey——AK 无效或 RAM 用户缺 log:Get* 权限（策略样例见使用说明）")
	}
	return fmt.Errorf("SLS 调用失败: %w", err)
}

// dig walks a map response level by level (Alibaba Cloud responses nest deeply and use
// CamelCase keys).
func dig(m map[string]any, path ...string) any {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[p]
	}
	return cur
}

func digStr(m map[string]any, path ...string) string {
	if v := dig(m, path...); v != nil {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

func digInt(m map[string]any, path ...string) int {
	switch v := dig(m, path...).(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		i, _ := v.Int64()
		return int(i)
	case string:
		var i int
		_, _ = fmt.Sscanf(v, "%d", &i)
		return i
	}
	return 0
}

func digFloat(m map[string]any, path ...string) float64 {
	switch v := dig(m, path...).(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case string:
		var f float64
		_, _ = fmt.Sscanf(v, "%f", &f)
		return f
	}
	return 0
}

// digList extracts an array (Alibaba Cloud lists are always wrapped in a double shell like
// {"Items":{"Item":[...]}}; path should point straight at the innermost array).
func digList(m map[string]any, path ...string) []map[string]any {
	v := dig(m, path...)
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(arr))
	for _, it := range arr {
		if mm, ok := it.(map[string]any); ok {
			out = append(out, mm)
		}
	}
	return out
}
