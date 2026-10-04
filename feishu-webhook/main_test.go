package main

// Contract-level tests. The network side (actually sending to Feishu) is verified with
// operation:test during integration testing; what's pinned down here is pure logic and the
// quality of error messages for "wrong credential / missing field" — per the lesson from "the five
// pitfalls of mutation-testing self-deception", every assertion checks a specific value, never a
// vague "did it error or not".

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The signature algorithm must match Feishu's documented example byte for byte: **the secret goes
// in the key, and the data being signed is an empty string**, the opposite of normal HMAC
// intuition. If someone "fixes" it based on intuition, every bot with signature verification
// turned on starts getting 19021 — this test exists to guard against that well-meaning mistake.
func TestSignShape(t *testing.T) {
	got := sign("mysecret", 1700000000)
	// A fixed value cross-checked against an independent implementation:
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
	// Priority among the three choices text/markdown/card: card > markdown > text.
	// Leaving all of them empty must error, rather than posting an empty message into the group.
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

// Drives one real POST end-to-end: with signing on, the body must carry timestamp+sign, and the
// signature must be verifiable by recomputing it with the secret.
func TestPostSignsAndParses(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = io.WriteString(w, `{"code":0,"msg":"success"}`)
	}))
	defer srv.Close()
	// URL validation requires it to contain /open-apis/bot/, so the fake server's path carries it.
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

// The signature error code must be translated to "go check the secret", and the rate-limit error
// must say "lower the send rate".
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

// Health check code classification (a regression test pinning down the smoking gun from issue
// #13): the old substring allowlist classified "invalid token" (19001 + "access token invalid",
// the real observed response when a bot is removed / a URL is pasted wrong) as green by pure
// coincidence — effectively always green as long as the Feishu domain is reachable at all. After
// switching to structured code classification, every shape is pinned down one by one.
func TestHealthCheckClassifiesByCode(t *testing.T) {
	cases := []struct {
		name string
		resp string
		ok   bool
	}{
		// Observed: an invalid token returns this exact response no matter what shape the body is —
		// must be red.
		{"token 无效=红", `{"code":19001,"data":{},"msg":"param invalid: incoming webhook access token invalid"}`, false},
		{"签名密钥错=红", `{"code":19021,"msg":"sign match fail or timestamp is not within one hour from current time"}`, false},
		// The token check already passed and it's on to the content check = the URL is alive:
		// doesn't depend on the specific shape of the empty-content response.
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
	// Connection failure = red.
	t.Run("连接失败=红", func(t *testing.T) {
		ctx := newFake(map[string]string{"webhook_url": "http://127.0.0.1:1/open-apis/bot/v2/hook/x"})
		out, err := opHealthCheck(ctx, &HealthCheckIn{})
		if err != nil || out.OK {
			t.Fatalf("连接失败该 ok=false 且不抛错: ok=%v err=%v", out.OK, err)
		}
	})
}
