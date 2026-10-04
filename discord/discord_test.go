package main

// 假 Discord。钉四件事：wait=true（不带就拿不到消息 id）、卡片字段顺序稳定、
// 超长提前拦下、健康检查不在群里留痕迹。

import (
	"context"
	"encoding/json"
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

func (c fakeCtx) Credential() map[string]string { return c.cred }
func (c *fakeCtx) Upload(name, mime string, data []byte) (*plugin.File, error) {
	return &plugin.File{ID: "f_1"}, nil
}
func (c *fakeCtx) UploadReader(string, string, io.Reader) (*plugin.File, error) { panic("不用") }
func (c *fakeCtx) Fetch(*plugin.File) ([]byte, error)                           { return []byte("data"), nil }

type capture struct {
	reqs   []string // METHOD path?query
	bodies []string
	ctypes []string
}

func fakeDiscord(t *testing.T, cap *capture, routes map[string]func(http.ResponseWriter, *http.Request)) *fakeCtx {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		cap.reqs = append(cap.reqs, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		cap.bodies = append(cap.bodies, string(body))
		cap.ctypes = append(cap.ctypes, r.Header.Get("Content-Type"))
		if h, ok := routes[r.Method]; ok {
			h(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodPost:
			io.WriteString(w, `{"id":"999","channel_id":"c1","guild_id":"g1"}`)
		case http.MethodGet:
			io.WriteString(w, `{"id":"wh1","name":"投研推送","channel_id":"c1"}`)
		case http.MethodDelete:
			w.WriteHeader(204)
		}
	}))
	t.Cleanup(srv.Close)
	return &fakeCtx{Context: context.Background(),
		cred: map[string]string{"webhook_url": srv.URL + "/api/webhooks/wh1/tok"}}
}

// 不带 wait=true 的话 Discord 回 204 空体，拿不到消息 id，下游改/删都做不到。
func TestSendAsksForMessageBack(t *testing.T) {
	cap := &capture{}
	ctx := fakeDiscord(t, cap, nil)

	out, err := opMessageSend(ctx, &DiscordMessageSendIn{Content: "研报已更新"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cap.reqs[0], "wait=true") {
		t.Errorf("没带 wait=true，拿不到消息 id: %s", cap.reqs[0])
	}
	if out.ID != "999" || out.ChannelID != "c1" {
		t.Errorf("回执不对: %+v", out)
	}
	if out.URL != "https://discord.com/channels/g1/c1/999" {
		t.Errorf("消息链接拼错了: %q", out.URL)
	}
}

// 卡片字段按键名排序：map 遍历是随机的，不排的话同样的输入每次排版都不同。
func TestEmbedFieldsAreStable(t *testing.T) {
	cap := &capture{}
	ctx := fakeDiscord(t, cap, nil)

	in := &DiscordMessageSendIn{
		Title: "贵州茅台", URL: "https://example.com/r/1", Color: "#2b6cb0", Footer: "来源：内部投研",
		Fields: map[string]string{"评级": "买入", "标的": "600519", "目标价": "1800"},
	}
	for i := 0; i < 3; i++ {
		if _, err := opMessageSend(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	var first string
	for i, b := range cap.bodies {
		var m map[string]any
		_ = json.Unmarshal([]byte(b), &m)
		embeds, _ := m["embeds"].([]any)
		if len(embeds) != 1 {
			t.Fatalf("第 %d 次没带卡片: %s", i+1, b)
		}
		e, _ := embeds[0].(map[string]any)
		fields, _ := e["fields"].([]any)
		var names []string
		for _, f := range fields {
			fm, _ := f.(map[string]any)
			names = append(names, fm["name"].(string))
		}
		got := strings.Join(names, ",")
		if i == 0 {
			first = got
			if e["color"] == nil || e["color"].(float64) != 2845872 { // 0x2b6cb0
				t.Errorf("色条没解析: %v", e["color"])
			}
			if e["url"] != "https://example.com/r/1" {
				t.Errorf("卡片链接丢了: %v", e["url"])
			}
		} else if got != first {
			t.Errorf("同样的输入排出了不同顺序: %q vs %q", first, got)
		}
	}
}

// 正文超 2000 要提前拦下，并指个出路（长内容放卡片正文，那儿能到 4096）。
func TestTooLongContentRejected(t *testing.T) {
	cap := &capture{}
	ctx := fakeDiscord(t, cap, nil)

	_, err := opMessageSend(ctx, &DiscordMessageSendIn{Content: strings.Repeat("字", 2001)})
	if err == nil {
		t.Fatal("超长应当被拦下")
	}
	if !strings.Contains(err.Error(), "4096") {
		t.Errorf("要指出路: %v", err)
	}
	if len(cap.reqs) != 0 {
		t.Error("拦下了却还是发了请求")
	}
}

// 什么都没给要当场说清楚，而不是发一条空消息出去。
func TestEmptyMessageRejected(t *testing.T) {
	cap := &capture{}
	ctx := fakeDiscord(t, cap, nil)

	if _, err := opMessageSend(ctx, &DiscordMessageSendIn{}); err == nil {
		t.Fatal("空消息应当被拦下")
	}
	if len(cap.reqs) != 0 {
		t.Error("拦下了却还是发了请求")
	}
}

// 健康检查**不发测试消息**——不该在群里留下痕迹。
func TestHealthCheckLeavesNoTrace(t *testing.T) {
	cap := &capture{}
	ctx := fakeDiscord(t, cap, nil)

	out, err := opHealthCheck(ctx, &HealthCheckIn{})
	if err != nil {
		t.Fatal(err)
	}
	if !out.OK || out.Channel != "c1" {
		t.Errorf("产出不对: %+v", out)
	}
	for _, r := range cap.reqs {
		if strings.HasPrefix(r, "POST") {
			t.Errorf("健康检查往群里发东西了: %s", r)
		}
	}
}

// Webhook 被删是结论不是故障：平台拿 ok=false 去写凭证状态。
func TestHealthCheckReportsDeletedWebhook(t *testing.T) {
	cap := &capture{}
	ctx := fakeDiscord(t, cap, map[string]func(http.ResponseWriter, *http.Request){
		http.MethodGet: func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(404)
			io.WriteString(w, `{"message":"Unknown Webhook","code":10015}`)
		},
	})

	out, err := opHealthCheck(ctx, &HealthCheckIn{})
	if err != nil {
		t.Fatalf("Webhook 被删不该报错: %v", err)
	}
	if out.OK {
		t.Error("Webhook 没了却说可用")
	}
	if !strings.Contains(out.Message, "删") {
		t.Errorf("要给人话原因: %q", out.Message)
	}
}

// 附件走 multipart，JSON 载荷要放在 payload_json 里（不是普通表单键）。
func TestAttachmentsUseMultipart(t *testing.T) {
	cap := &capture{}
	ctx := fakeDiscord(t, cap, nil)

	_, err := opMessageSend(ctx, &DiscordMessageSendIn{
		Content: "附一份 PDF", Files: []*plugin.File{{ID: "f1", Name: "r.pdf", Mime: "application/pdf"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(cap.ctypes[0], "multipart/form-data") {
		t.Errorf("附件应当走 multipart: %q", cap.ctypes[0])
	}
	if !strings.Contains(cap.bodies[0], "payload_json") {
		t.Error("JSON 载荷要放在 payload_json 字段里")
	}
	if !strings.Contains(cap.bodies[0], "r.pdf") {
		t.Error("附件文件名没带上")
	}
}

// 发进话题时，删除也要带同一个 thread_id，否则 Discord 找不到那条消息。
func TestThreadIDIsCarried(t *testing.T) {
	cap := &capture{}
	ctx := fakeDiscord(t, cap, nil)

	if _, err := opMessageSend(ctx, &DiscordMessageSendIn{Content: "x", ThreadID: "t9"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cap.reqs[0], "thread_id=t9") {
		t.Errorf("发送没带 thread_id: %s", cap.reqs[0])
	}
	if _, err := opMessageDelete(ctx, &DiscordMessageDeleteIn{MessageID: "999", ThreadID: "t9"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cap.reqs[1], "thread_id=t9") {
		t.Errorf("删除没带 thread_id: %s", cap.reqs[1])
	}
}

// 凭证里填了个不是 Webhook 的地址，要在发请求前说清楚。
func TestBadWebhookURLIsExplained(t *testing.T) {
	ctx := &fakeCtx{Context: context.Background(),
		cred: map[string]string{"webhook_url": "https://discord.com/channels/1/2"}}
	_, err := opMessageSend(ctx, &DiscordMessageSendIn{Content: "x"})
	if err == nil || !strings.Contains(err.Error(), "api/webhooks") {
		t.Errorf("要说清正确形状: %v", err)
	}
}
