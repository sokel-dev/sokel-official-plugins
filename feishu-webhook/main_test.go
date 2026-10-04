package main

// 契约级测试。网络面（真发飞书）在联调时用 operation:test 验，这里钉的是
// 纯逻辑与「装错凭证/漏填」的报错质量——按「验齿五坑」的教训，每条断言
// 都断具体值，不断「有没有报错」这种空话。

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 签名算法必须逐字节对上飞书文档的样例形态：**secret 在 key 里、被签数据是空串**，
// 与常规 HMAC 直觉相反。有人按直觉「修好它」的话，所有开了签名校验的机器人
// 全部 19021——这条测试就是防那次好心。
func TestSignShape(t *testing.T) {
	got := sign("mysecret", 1700000000)
	// 与独立实现比对过的定值：
	//   python: hmac.new(b"1700000000\nmysecret", b"", sha256) → base64
	if got != "Jp33/xXhCipDEpjyHvEyc7mRSyXWHbNz6J8+C3qQKNo=" {
		t.Errorf("签名 = %q", got)
	}
	if sign("a", 1) == sign("a", 2) {
		t.Error("不同时间戳的签名不该相同")
	}
	if sign("a", 1) == sign("b", 1) {
		t.Error("不同密钥的签名不该相同")
	}
}

func TestSendPicksMessageShape(t *testing.T) {
	// text/markdown/card 三选一的优先级：card > markdown > text。
	// 都不填要报错，而不是发一条空消息进群。
	_, err := opSend(newFake(map[string]string{"webhook_url": "https://open.feishu.cn/open-apis/bot/v2/hook/x"}), &WebhookSendIn{})
	if err == nil || !strings.Contains(err.Error(), "至少填一个") {
		t.Errorf("全空要报「至少填一个」，got %v", err)
	}
}

func TestWebhookURLValidation(t *testing.T) {
	_, err := opSend(newFake(map[string]string{"webhook_url": "https://example.com/x"}), &WebhookSendIn{Text: "hi"})
	if err == nil || !strings.Contains(err.Error(), "不像飞书群机器人") {
		t.Errorf("粘错 URL 要有能读懂的报错，got %v", err)
	}
}

// 打穿一次真实 POST：开签名时 body 里要有 timestamp+sign，且签名可用密钥重算验证。
func TestPostSignsAndParses(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = io.WriteString(w, `{"code":0,"msg":"success"}`)
	}))
	defer srv.Close()
	// URL 校验要求含 /open-apis/bot/，假服务器路径带上它。
	ctx := newFake(map[string]string{"webhook_url": srv.URL + "/open-apis/bot/v2/hook/x", "secret": "s3"})
	if _, err := opSend(ctx, &WebhookSendIn{Text: "hi", AtAll: true}); err != nil {
		t.Fatal(err)
	}
	ts, _ := got["timestamp"].(string)
	sg, _ := got["sign"].(string)
	if ts == "" || sg == "" {
		t.Fatalf("开签名时要带 timestamp+sign: %v", got)
	}
	var tsn int64
	_, _ = fmt.Sscanf(ts, "%d", &tsn)
	if sign("s3", tsn) != sg {
		t.Error("body 里的签名与密钥重算的不一致")
	}
	if txt := got["content"].(map[string]any)["text"].(string); !strings.Contains(txt, `<at user_id="all">`) {
		t.Errorf("at_all 要拼 @所有人: %q", txt)
	}
}

// 签名错误码要翻译成「去核对密钥」，频控要说「降低频率」。
func TestErrorCodes(t *testing.T) {
	for code, want := range map[int]string{19021: "签名校验失败", 9499: "频控"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, fmt.Sprintf(`{"code":%d,"msg":"x"}`, code))
		}))
		ctx := newFake(map[string]string{"webhook_url": srv.URL + "/open-apis/bot/v2/hook/x"})
		_, err := opSend(ctx, &WebhookSendIn{Text: "hi"})
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("code %d 要说「%s」, got %v", code, want, err)
		}
	}
}

// 健康检查判码（issue #13 实锤回归钉）：曾经的子串白名单把「token 无效」
// （19001 + "access token invalid"，机器人被删/URL 粘错的真实应答，实测形状）
// 恰好判成绿——只要能连上飞书域名几乎必绿。改结构化判码后逐形态钉死。
func TestHealthCheckClassifiesByCode(t *testing.T) {
	cases := []struct {
		name string
		resp string
		ok   bool
	}{
		// 实测：伪 token 无论 body 什么形状都回这条——必须红。
		{"token 无效=红", `{"code":19001,"data":{},"msg":"param invalid: incoming webhook access token invalid"}`, false},
		{"签名密钥错=红", `{"code":19021,"msg":"sign match fail or timestamp is not within one hour from current time"}`, false},
		// token 已过、轮到内容检查 = URL 活：不依赖空内容应答的具体形状。
		{"内容参数错=绿", `{"code":9499,"msg":"Bad Request: fail to parse content"}`, true},
		{"其他参数错=绿", `{"code":19002,"msg":"param invalid: msg_type"}`, true},
		{"老版字段参数错=绿", `{"StatusCode":9499,"StatusMessage":"fail to parse content"}`, true},
		{"竟然发成功=绿", `{"code":0,"msg":"success"}`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, c.resp)
			}))
			defer srv.Close()
			ctx := newFake(map[string]string{"webhook_url": srv.URL + "/open-apis/bot/v2/hook/x"})
			out, err := opHealthCheck(ctx, &HealthCheckIn{})
			if err != nil {
				t.Fatal(err)
			}
			if out.OK != c.ok {
				t.Fatalf("应答 %s 该判 ok=%v，得到 ok=%v（%s）", c.resp, c.ok, out.OK, out.Message)
			}
		})
	}
	// 连不上 = 红。
	t.Run("连接失败=红", func(t *testing.T) {
		ctx := newFake(map[string]string{"webhook_url": "http://127.0.0.1:1/open-apis/bot/v2/hook/x"})
		out, err := opHealthCheck(ctx, &HealthCheckIn{})
		if err != nil || out.OK {
			t.Fatalf("连接失败该 ok=false 且不抛错: ok=%v err=%v", out.OK, err)
		}
	})
}
