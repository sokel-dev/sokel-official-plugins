package main

// 契约级测试：纯逻辑 + httptest 假飞书打穿发送链路。
// 真上游联调走 operation:test（见 docs）。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

type fakeCtx struct {
	context.Context
	cred map[string]string
}

func newFake(cred map[string]string) *fakeCtx {
	return &fakeCtx{Context: context.Background(), cred: cred}
}

func (f *fakeCtx) Credential() map[string]string { return f.cred }
func (f *fakeCtx) Upload(name, mime string, data []byte) (*plugin.File, error) {
	return &plugin.File{ID: "f_t", Name: name, Mime: mime}, nil
}
func (f *fakeCtx) UploadReader(name, mime string, r io.Reader) (*plugin.File, error) {
	return &plugin.File{ID: "f_t", Name: name, Mime: mime}, nil
}
func (f *fakeCtx) Fetch(*plugin.File) ([]byte, error) { return []byte("img-bytes"), nil }

// fakeFeishu 起一个假开放平台：token 端点 + 收消息端点。
// 返回 (server, 收到的请求体指针)。
func fakeFeishu(t *testing.T, handler func(w http.ResponseWriter, r *http.Request) bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "tenant_access_token") {
			_, _ = io.WriteString(w, `{"code":0,"msg":"ok","tenant_access_token":"t-fake","expire":7200}`)
			return
		}
		if handler(w, r) {
			return
		}
		_, _ = io.WriteString(w, `{"code":0,"msg":"success","data":{}}`)
	}))
}

// credFor 指向假服务器的凭证。**每个用例独立 app_id**：client 按 app_id 缓存，
// 撞名会把上一个用例的 baseURL 带过来——那是一类查不出来的串扰。
var appSeq int

func credFor(srv *httptest.Server) map[string]string {
	appSeq++
	return map[string]string{
		"app_id":     fmt.Sprintf("cli_test_%d", appSeq),
		"app_secret": "sec",
		"domain":     srv.URL, // 完整地址透传（baseURL 的第三条分支），请求打进假飞书
	}
}

// baseURL 契约：feishu/lark 之外的值原样透传（测试指向 httptest 全靠它；
// 生产上凭证是下拉框，不会出现第三种值）。
func TestBaseURL(t *testing.T) {
	if baseURL("feishu") != "https://open.feishu.cn" {
		t.Error("feishu 域名错了")
	}
	if baseURL("lark") != "https://open.larksuite.com" {
		t.Error("lark 域名错了")
	}
	if baseURL("") != "https://open.feishu.cn" {
		t.Error("默认要落到飞书国内")
	}
	if baseURL("http://127.0.0.1:1234/") != "http://127.0.0.1:1234" {
		t.Error("完整地址要原样透传（测试与私有化部署靠它）")
	}
}

// 发文本：content 必须是**双重编码**的 JSON 串（飞书铁律），
// receive_id_type 进 query 而不是 body。
func TestSendTextWireShape(t *testing.T) {
	var got struct {
		ReceiveID string `json:"receive_id"`
		MsgType   string `json:"msg_type"`
		Content   string `json:"content"`
	}
	var query string
	srv := fakeFeishu(t, func(w http.ResponseWriter, r *http.Request) bool {
		if strings.HasSuffix(r.URL.Path, "/im/v1/messages") {
			query = r.URL.RawQuery
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &got)
			_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{"message_id":"om_1","chat_id":"oc_1"}}`)
			return true
		}
		return false
	})
	defer srv.Close()

	out, err := opSendText(newFake(credFor(srv)), &SendTextIn{ReceiveID: "oc_1", ReceiveIDType: "chat_id", Text: "你好"})
	if err != nil {
		t.Fatal(err)
	}
	if out.MessageID != "om_1" {
		t.Errorf("message_id = %q", out.MessageID)
	}
	if !strings.Contains(query, "receive_id_type=chat_id") {
		t.Errorf("receive_id_type 要进 query: %q", query)
	}
	if got.MsgType != "text" || got.ReceiveID != "oc_1" {
		t.Errorf("请求体不对: %+v", got)
	}
	// content 是 JSON 串：能再解一层且里面是 text 字段。
	var inner struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(got.Content), &inner); err != nil || inner.Text != "你好" {
		t.Errorf("content 要是双重编码的 JSON 串, got %q", got.Content)
	}
}

// 业务码非 0 必须转成能读懂的错误，并且高频码要带「下一步做什么」。
func TestErrorTranslation(t *testing.T) {
	srv := fakeFeishu(t, func(w http.ResponseWriter, r *http.Request) bool {
		if strings.HasSuffix(r.URL.Path, "/im/v1/messages") {
			_, _ = io.WriteString(w, `{"code":230002,"msg":"Bot is NOT in the chat"}`)
			return true
		}
		return false
	})
	defer srv.Close()
	_, err := opSendText(newFake(credFor(srv)), &SendTextIn{ReceiveID: "oc_x", Text: "hi"})
	if err == nil || !strings.Contains(err.Error(), "拉进群") {
		t.Errorf("230002 要告诉用户去拉 bot 进群, got %v", err)
	}
}

func TestGuessIDType(t *testing.T) {
	for id, want := range map[string]string{
		"oc_abc": "chat_id", "ou_abc": "open_id", "on_abc": "union_id",
		"a@b.com": "email", "someuser": "chat_id",
	} {
		if got := guessIDType(id); got != want {
			t.Errorf("guessIDType(%q) = %q, want %q", id, got, want)
		}
	}
}

// markdown → 卡片：有标题带 header，没标题不带（带个空 header 飞书会渲染出一条空杠）。
func TestMdCard(t *testing.T) {
	c := mdCard("日报", "green", "**hi**")
	if c["header"] == nil {
		t.Error("有标题要有 header")
	}
	if mdCard("", "", "x")["header"] != nil {
		t.Error("没标题不该有 header——空 header 会渲染出一条空杠")
	}
	el := c["elements"].([]any)[0].(map[string]any)
	if el["tag"] != "markdown" || el["content"] != "**hi**" {
		t.Errorf("markdown 元素不对: %+v", el)
	}
}

// Markdown → docx 块：行级转换的每一类都钉一条；未闭合代码块不丢内容。
func TestMdToBlocks(t *testing.T) {
	md := "# 标题\n## 二级\n正文段落\n- 列表项\n1. 有序项\n> 引用\n---\n```\ncode line\n```"
	blocks := mdToBlocks(md)
	types := make([]int, len(blocks))
	for i, b := range blocks {
		types[i] = b["block_type"].(int)
	}
	want := []int{3, 4, 2, 12, 13, 15, 22, 14}
	if len(types) != len(want) {
		t.Fatalf("块数 = %d (%v), want %d", len(types), types, len(want))
	}
	for i := range want {
		if types[i] != want[i] {
			t.Errorf("第 %d 块类型 = %d, want %d", i, types[i], want[i])
		}
	}
	// 未闭合的代码块不丢
	tail := mdToBlocks("```\nabc")
	if len(tail) != 1 || tail[0]["block_type"].(int) != 14 {
		t.Errorf("未闭合代码块要保住内容: %+v", tail)
	}
}

// docx 追加分批：120 块要打 3 次（50/50/20），不分批飞书直接 invalid param。
func TestAppendBlocksBatches(t *testing.T) {
	var calls []int
	srv := fakeFeishu(t, func(w http.ResponseWriter, r *http.Request) bool {
		if strings.Contains(r.URL.Path, "/blocks/") {
			var body struct {
				Children []any `json:"children"`
			}
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &body)
			calls = append(calls, len(body.Children))
			_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{}}`)
			return true
		}
		return false
	})
	defer srv.Close()
	blocks := make([]map[string]any, 120)
	for i := range blocks {
		blocks[i] = docxText(2, "x")
	}
	n, err := appendBlocks(newFake(credFor(srv)), "doxcn123", blocks)
	if err != nil || n != 120 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if len(calls) != 3 || calls[0] != 50 || calls[1] != 50 || calls[2] != 20 {
		t.Errorf("分批 = %v, want [50 50 20]", calls)
	}
}

// 允许粘 URL 的三个入口。
func TestTokenFromURL(t *testing.T) {
	if got := docIDFromAny("https://x.feishu.cn/docx/doxcnAbC?from=1"); got != "doxcnAbC" {
		t.Errorf("docx URL 解析 = %q", got)
	}
	p, err := btPath("https://x.feishu.cn/base/bascnXYZ?table=tblfoo", "tblfoo")
	if err != nil || !strings.Contains(p, "/apps/bascnXYZ/tables/tblfoo/") {
		t.Errorf("bitable URL 解析 = %q err=%v", p, err)
	}
}

// 缺凭证的报错要指路，不是裸的 unauthorized。
func TestMissingCredential(t *testing.T) {
	_, err := opSendText(newFake(map[string]string{}), &SendTextIn{ReceiveID: "oc_1", Text: "hi"})
	if err == nil || !strings.Contains(err.Error(), "app_id") {
		t.Errorf("缺凭证要指路, got %v", err)
	}
}

// 健康检查判据（issue #13 ④ 回归钉）：健康 = 能换出 tenant_access_token，
// **与机器人能力无关**——没开「机器人」的应用（只读多维表格那类）bot/v3/info
// 必失败，曾被当成凭证坏；现在 bot 名只是尽力富化，拿不到不扣分。
func TestHealthCheckJudgesByTokenNotBot(t *testing.T) {
	cases := []struct {
		name    string
		tokenRe string
		botRe   string
		ok      bool
	}{
		{"token 换得出+无机器人能力=绿", `{"code":0,"tenant_access_token":"t-x","expire":7200}`, `{"code":230013,"msg":"bot not activated"}`, true},
		{"token 换得出+有机器人=绿带名", `{"code":0,"tenant_access_token":"t-x","expire":7200}`, `{"code":0,"bot":{"bot_name":"小助手"}}`, true},
		{"密钥错=红", `{"code":10003,"msg":"invalid app_secret"}`, ``, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.Contains(r.URL.Path, "tenant_access_token") {
					_, _ = io.WriteString(w, c.tokenRe)
					return
				}
				_, _ = io.WriteString(w, c.botRe)
			}))
			defer srv.Close()
			ctx := newFake(map[string]string{"app_id": "cli_x", "app_secret": "s-" + c.name, "domain": srv.URL})
			out, err := opHealthCheck(ctx, &HealthCheckIn{})
			if err != nil {
				t.Fatal(err)
			}
			if out.OK != c.ok {
				t.Fatalf("该判 ok=%v，得到 ok=%v（%s）", c.ok, out.OK, out.Message)
			}
			if c.name == "token 换得出+有机器人=绿带名" && out.BotName != "小助手" {
				t.Errorf("bot 名富化没生效: %q", out.BotName)
			}
		})
	}
}
