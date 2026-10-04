package main

// 假 PDS。钉的是四件「不测就一定错、错了还不容易发现」的事：
//   - facets 的下标是 **UTF-8 字节偏移**（中英混排时按字符算会整体错位）；
//   - 帖串的 root/parent 引用（只给 parent 的话整串会散成互不相干的帖子）；
//   - 会话过期后自动续期（accessJwt 只活几分钟，工作流停在人工节点上一等就过期）；
//   - 链接卡片抓不到时**不能让发布失败**。

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

// —— 假上下文 ——

type fakeCtx struct {
	context.Context
	cred map[string]string
	file []byte
}

func (c fakeCtx) Credential() map[string]string { return c.cred }
func (c *fakeCtx) Upload(name, mime string, data []byte) (*plugin.File, error) {
	return &plugin.File{ID: "f_1", Name: name, Mime: mime}, nil
}
func (c *fakeCtx) UploadReader(string, string, io.Reader) (*plugin.File, error) {
	panic("本插件不用流式上传")
}
func (c *fakeCtx) Fetch(*plugin.File) ([]byte, error) { return c.file, nil }

func ctxTo(t *testing.T, pds string) *fakeCtx {
	t.Helper()
	sessMu.Lock()
	sessCache = map[string]*session{}
	sessMu.Unlock()
	return &fakeCtx{Context: context.Background(), cred: map[string]string{
		"identifier": "acme.bsky.social", "app_password": "pw-pw-pw-pw", "pds_url": pds,
	}}
}

// 记下每个 nsid 收到的请求体。
type capture struct {
	nsids  []string
	bodies map[string][]string
}

func newCapture() *capture { return &capture{bodies: map[string][]string{}} }

func (c *capture) last(nsid string) map[string]any {
	b := c.bodies[nsid]
	if len(b) == 0 {
		return nil
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(b[len(b)-1]), &m)
	return m
}

// fakePDS：createSession 恒成功；createRecord 按调用次序回不同 uri/cid。
func fakePDS(t *testing.T, cap *capture, extra map[string]func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nsid := strings.TrimPrefix(r.URL.Path, "/xrpc/")
		body, _ := io.ReadAll(r.Body)
		cap.nsids = append(cap.nsids, nsid)
		cap.bodies[nsid] = append(cap.bodies[nsid], string(body))
		if h, ok := extra[nsid]; ok {
			h(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch nsid {
		case "com.atproto.server.createSession", "com.atproto.server.refreshSession":
			io.WriteString(w, `{"accessJwt":"at1","refreshJwt":"rt1","did":"did:plc:acme","handle":"acme.bsky.social"}`)
		case "com.atproto.repo.createRecord":
			n++
			io.WriteString(w, `{"uri":"at://did:plc:acme/app.bsky.feed.post/rk`+itoa(n)+`","cid":"cid`+itoa(n)+`"}`)
		case "com.atproto.repo.deleteRecord":
			io.WriteString(w, `{}`)
		case "com.atproto.identity.resolveHandle":
			io.WriteString(w, `{"did":"did:plc:friend"}`)
		case "com.atproto.repo.uploadBlob":
			io.WriteString(w, `{"blob":{"$type":"blob","ref":{"$link":"bafy"},"mimeType":"image/jpeg","size":10}}`)
		default:
			w.WriteHeader(404)
			io.WriteString(w, `{"error":"MethodNotImplemented","message":"`+nsid+`"}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// —— facets：字节偏移 ——

// 中英混排时按字符下标算，链接会整体错位（每个中文差 2）。这条是本插件存在的主要理由之一。
func TestFacetsUseByteOffsets(t *testing.T) {
	text := "A股放量 https://example.com/a 走强 #复盘"
	fs, first := buildFacets(text, func(string) (string, error) { return "", nil })

	if first != "https://example.com/a" {
		t.Fatalf("第一个链接认错了: %q", first)
	}
	var link, tag *facet
	for i := range fs {
		switch fs[i].Features[0].Type {
		case "app.bsky.richtext.facet#link":
			link = &fs[i]
		case "app.bsky.richtext.facet#tag":
			tag = &fs[i]
		}
	}
	if link == nil || tag == nil {
		t.Fatalf("链接与话题都该认出来: %+v", fs)
	}
	// 用字节切片验证：切出来必须正好是那段原文
	b := []byte(text)
	if got := string(b[link.Index.ByteStart:link.Index.ByteEnd]); got != "https://example.com/a" {
		t.Errorf("链接下标切出来是 %q——按字符算的经典症状", got)
	}
	if got := string(b[tag.Index.ByteStart:tag.Index.ByteEnd]); got != "#复盘" {
		t.Errorf("话题下标切出来是 %q", got)
	}
	if tag.Features[0].Tag != "复盘" {
		t.Errorf("话题内容不该带 #: %q", tag.Features[0].Tag)
	}
}

// 链接末尾的中文标点不属于链接；URL 里的 # 锚点不是话题。
func TestFacetsDoNotOverreach(t *testing.T) {
	fs, first := buildFacets("详见 https://example.com/a#section。", func(string) (string, error) { return "", nil })
	if first != "https://example.com/a#section" {
		t.Errorf("句号被吃进链接了: %q", first)
	}
	for _, f := range fs {
		if f.Features[0].Type == "app.bsky.richtext.facet#tag" {
			t.Errorf("URL 里的 #section 被当成了话题: %+v", f)
		}
	}
}

// @ 认不出来就当普通文本——一个手滑的用户名不该挡住整条发布。
func TestUnresolvableMentionIsPlainText(t *testing.T) {
	fs, _ := buildFacets("问问 @nobody.bsky.social 怎么看", func(string) (string, error) {
		return "", context.DeadlineExceeded
	})
	if len(fs) != 0 {
		t.Errorf("解析不了的 @ 不该产出 facet: %+v", fs)
	}
}

// —— 发布 ——

func TestPublishCarriesFacetsAndLangs(t *testing.T) {
	cap := newCapture()
	srv := fakePDS(t, cap, nil)

	out, err := opPostCreate(ctxTo(t, srv.URL), &BskyPostCreateIn{
		Text: "看这个 https://example.com/x #A股", Langs: "zh,en", LinkCardURL: "-",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.URL != "https://bsky.app/profile/acme.bsky.social/post/rk1" {
		t.Errorf("发完要给网页链接，得到 %q", out.URL)
	}
	rec, _ := cap.last("com.atproto.repo.createRecord")["record"].(map[string]any)
	if rec == nil {
		t.Fatal("没发出记录")
	}
	if fs, _ := rec["facets"].([]any); len(fs) != 2 {
		t.Errorf("链接 + 话题应当各一条 facet，得到 %v", rec["facets"])
	}
	if langs, _ := rec["langs"].([]any); len(langs) != 2 {
		t.Errorf("langs 没带上: %v", rec["langs"])
	}
	if rec["createdAt"] == "" || rec["$type"] != "app.bsky.feed.post" {
		t.Errorf("记录形状不对: %v", rec)
	}
	// "-" 明确表示不要卡片
	if rec["embed"] != nil {
		t.Errorf("填了 - 还带卡片: %v", rec["embed"])
	}
}

// 超长要**在发出去之前**拦下：发到一半被拒的话，帖串会断在中间。
func TestPublishRejectsTooLongBeforeSending(t *testing.T) {
	cap := newCapture()
	srv := fakePDS(t, cap, nil)

	_, err := opPostCreate(ctxTo(t, srv.URL), &BskyPostCreateIn{Text: strings.Repeat("字", 301)})
	if err == nil {
		t.Fatal("301 个字应当被拦下")
	}
	if len(cap.bodies["com.atproto.repo.createRecord"]) != 0 {
		t.Error("拦下了却还是发了请求")
	}
}

// 抓不到 OG 信息不能让发布失败——退回纯文本链接照样发得出去。
func TestLinkCardFailureDoesNotBlockPost(t *testing.T) {
	cap := newCapture()
	srv := fakePDS(t, cap, nil)

	// link_card_url 指向一个必然连不上的地址
	out, err := opPostCreate(ctxTo(t, srv.URL), &BskyPostCreateIn{
		Text: "看这个", LinkCardURL: "http://127.0.0.1:1/nope",
	})
	if err != nil {
		t.Fatalf("卡片抓不到不该让发布失败: %v", err)
	}
	if out.URI == "" {
		t.Error("没拿到帖子地址")
	}
}

// —— 帖串 ——

// 第二条起 parent 是前一条，**root 恒为第一条**。搞错的话整串会散成互不相干的帖子。
func TestThreadChainsRootAndParent(t *testing.T) {
	cap := newCapture()
	srv := fakePDS(t, cap, nil)

	out, err := opPostThread(ctxTo(t, srv.URL), &BskyPostThreadIn{
		Texts: []string{"一", "二", "三"}, IntervalMs: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Count != 3 || len(out.Uris) != 3 {
		t.Fatalf("应当发出 3 条: %+v", out)
	}
	bodies := cap.bodies["com.atproto.repo.createRecord"]
	if len(bodies) != 3 {
		t.Fatalf("发了 %d 次", len(bodies))
	}
	get := func(i int) map[string]any {
		var m map[string]any
		_ = json.Unmarshal([]byte(bodies[i]), &m)
		rec, _ := m["record"].(map[string]any)
		return rec
	}
	if get(0)["reply"] != nil {
		t.Error("第一条不该有 reply")
	}
	for i := 1; i < 3; i++ {
		reply, _ := get(i)["reply"].(map[string]any)
		if reply == nil {
			t.Fatalf("第 %d 条没有 reply", i+1)
		}
		root, _ := reply["root"].(map[string]any)
		parent, _ := reply["parent"].(map[string]any)
		if root["uri"] != out.Uris[0] {
			t.Errorf("第 %d 条的 root 应当恒为首条，得到 %v", i+1, root["uri"])
		}
		if parent["uri"] != out.Uris[i-1] {
			t.Errorf("第 %d 条的 parent 应当是前一条，得到 %v", i+1, parent["uri"])
		}
		if root["cid"] == nil || parent["cid"] == nil {
			t.Errorf("强引用要带 cid: %v", reply)
		}
	}
}

// —— 会话 ——

// accessJwt 只活几分钟：工作流停在人工节点上等半天，恢复后第一条请求必然 401。
// 不自动续期的话，那看起来像密码错了。
func TestExpiredSessionIsRenewed(t *testing.T) {
	cap := newCapture()
	first := true
	srv := fakePDS(t, cap, map[string]func(http.ResponseWriter, *http.Request){
		"com.atproto.repo.createRecord": func(w http.ResponseWriter, r *http.Request) {
			if first {
				first = false
				w.WriteHeader(http.StatusUnauthorized)
				io.WriteString(w, `{"error":"ExpiredToken","message":"Token has expired"}`)
				return
			}
			io.WriteString(w, `{"uri":"at://did:plc:acme/app.bsky.feed.post/rk9","cid":"cid9"}`)
		},
	})

	out, err := opPostCreate(ctxTo(t, srv.URL), &BskyPostCreateIn{Text: "你好"})
	if err != nil {
		t.Fatalf("过期会话应当自动续期后重试: %v", err)
	}
	if out.Cid != "cid9" {
		t.Errorf("重试后的结果没带出来: %+v", out)
	}
	var refreshed bool
	for _, n := range cap.nsids {
		if n == "com.atproto.server.refreshSession" {
			refreshed = true
		}
	}
	if !refreshed {
		t.Error("没走 refreshSession——应当先拿 refreshJwt 换，而不是每次重新登录")
	}
}

// 健康检查：**不可用要返回 ok=false 而不是 error**，平台拿它去写凭证状态。
func TestHealthCheckReportsFailureAsResult(t *testing.T) {
	cap := newCapture()
	srv := fakePDS(t, cap, map[string]func(http.ResponseWriter, *http.Request){
		"com.atproto.server.createSession": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"error":"AuthenticationRequired","message":"Invalid identifier or password"}`)
		},
	})

	out, err := opHealthCheck(ctxTo(t, srv.URL), &HealthCheckIn{})
	if err != nil {
		t.Fatalf("密码不对是结论不是故障，不该报错: %v", err)
	}
	if out.OK {
		t.Error("密码不对却说可用")
	}
	if !strings.Contains(out.Message, "撤销") {
		t.Errorf("要给人话原因: %q", out.Message)
	}
}

// —— 地址 ——

func TestAtURIFromWebLink(t *testing.T) {
	cap := newCapture()
	srv := fakePDS(t, cap, nil)
	ctx := ctxTo(t, srv.URL)

	got, err := atURI(ctx, "https://bsky.app/profile/friend.bsky.social/post/abc123")
	if err != nil {
		t.Fatal(err)
	}
	if got != "at://did:plc:friend/app.bsky.feed.post/abc123" {
		t.Errorf("网页链接没换成 at:// 地址: %q", got)
	}
	// 已经是 at:// 的原样返回，不必多问一次
	if got, _ := atURI(ctx, "at://did:plc:x/app.bsky.feed.post/y"); got != "at://did:plc:x/app.bsky.feed.post/y" {
		t.Errorf("at:// 地址被改动了: %q", got)
	}
}

func TestWebURL(t *testing.T) {
	if got := webURL("acme.bsky.social", "at://did:plc:acme/app.bsky.feed.post/rk1"); got !=
		"https://bsky.app/profile/acme.bsky.social/post/rk1" {
		t.Errorf("网页链接拼错了: %q", got)
	}
}
