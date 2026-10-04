package main

// 阿里云调用层。两条通道：
//
//   - **泛化调用**（callACS）：darabonba-openapi 的统一签名网关，{Endpoint, Action,
//     Version, 参数} 调任意 RPC 产品——DNS/RDS/ACK 管控/云监控/STS 全走这一条。
//     不引 per-product 生成 SDK（每个产品一个巨包，而我们只用每家两三个接口）。
//   - **SLS**（slsClientOf）：日志服务是独立协议独立签名，走官方 aliyun-log-go-sdk。
//
// client 按 (AK, endpoint) 缓存：每次新建虽然不至于重新握手（AK 签名无状态），
// 但 darabonba 的 client 构造并不便宜，也没理由重复做。

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

// regionOf 操作级 region 覆盖凭证默认；都没有则用杭州——
// 报错时会带上「哪个 region」的信息，不至于静默查错地方。
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

// callACS 泛化调用一个 RPC Action。params 全走 query（阿里云 RPC 风格的规范位置）。
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
		// 有些网关把 body 平铺在顶层
		body = res
	}
	return body, nil
}

// acsErr 高频错误码翻译成「下一步做什么」。阿里云的报错是给开发者的英文 + RequestId，
// 看到它的是画布上配节点的人。
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

// slsErr SLS 独立协议的错误翻译。
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

// dig 从 map 应答里逐层取值（阿里云应答嵌套深，且大小写驼峰）。
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

// digList 取数组（阿里云的列表都包在 {"Items":{"Item":[...]}} 这类双层壳里，
// path 直接写到最里层的数组）。
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
