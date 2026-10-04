package main

// 发布：发帖、帖串、删帖、健康检查。
//
// 一条帖子 = 一条 app.bsky.feed.post 记录。除了正文，三样东西要插件替用户备齐：
// facets（不给就没有可点的链接，见 richtext.go）、embed（图片或链接卡片）、
// reply（回复要同时给 root 与 parent，只给 parent 的话整串会散在时间线上各自为政）。

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

const maxGraphemes = 300

// postRecord：app.bsky.feed.post 记录。
type postRecord struct {
	Type      string    `json:"$type"`
	Text      string    `json:"text"`
	CreatedAt string    `json:"createdAt"`
	Langs     []string  `json:"langs,omitempty"`
	Facets    []facet   `json:"facets,omitempty"`
	Embed     any       `json:"embed,omitempty"`
	Reply     *replyRef `json:"reply,omitempty"`
}

type strongRef struct {
	URI string `json:"uri"`
	CID string `json:"cid"`
}

// replyRef：**root 与 parent 都要**。只给 parent 的话，第三条起会被当成新串的开头，
// 时间线上看到的是一堆互不相干的帖子。
type replyRef struct {
	Root   strongRef `json:"root"`
	Parent strongRef `json:"parent"`
}

type createRecordOut struct {
	URI string `json:"uri"`
	CID string `json:"cid"`
}

// publishOpts：一次发布要的全部东西（帖串复用它）。
type publishOpts struct {
	Text    string
	Langs   string
	Images  []*plugin.File
	ImgAlts []string
	CardURL string    // "-" = 明确不要卡片；"" = 用正文第一个链接
	Reply   *replyRef // nil = 原创
}

// publish：发一条，返回强引用（uri+cid，回复下一条要用）。
func publish(ctx plugin.Ctx, o publishOpts) (strongRef, error) {
	text := strings.TrimSpace(o.Text)
	if text == "" && len(o.Images) == 0 {
		return strongRef{}, fmt.Errorf("正文与图片至少要有一样")
	}
	// 提前拦下超长：发出去被拒的话，帖串会断在中间，收拾起来比一句提示麻烦得多。
	if n := graphemes(text); n > maxGraphemes {
		return strongRef{}, fmt.Errorf("正文 %d 个字，超过 Bluesky 的 %d 上限（发帖串请用「发帖串」操作）", n, maxGraphemes)
	}
	s, _, _, err := sessionFor(ctx)
	if err != nil {
		return strongRef{}, err
	}

	facets, firstLink := buildFacets(text, resolverFor(ctx))
	rec := postRecord{
		Type: "app.bsky.feed.post", Text: text,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Facets:    facets, Reply: o.Reply,
	}
	for _, l := range strings.Split(o.Langs, ",") {
		if l = strings.TrimSpace(l); l != "" {
			rec.Langs = append(rec.Langs, l)
		}
	}

	switch {
	case len(o.Images) > 0:
		embed, err := imagesEmbed(ctx, o.Images, o.ImgAlts)
		if err != nil {
			return strongRef{}, err
		}
		rec.Embed = embed
	default:
		// 链接卡片：显式给的优先，其次用正文里的第一个链接；"-" 表示不要。
		card := strings.TrimSpace(o.CardURL)
		if card == "" {
			card = firstLink
		}
		if card != "" && card != "-" {
			// 抓不到 OG 信息**不算失败**：退回纯文本链接照样发得出去，
			// 为了一张缩略图把整条发布判死是本末倒置。
			if embed, err := externalEmbed(ctx, trimTrailingPunct(card)); err == nil {
				rec.Embed = embed
			}
		}
	}

	var out createRecordOut
	if err := call(ctx, http.MethodPost, "com.atproto.repo.createRecord", nil, map[string]any{
		"repo": s.DID, "collection": "app.bsky.feed.post", "record": rec,
	}, &out); err != nil {
		return strongRef{}, err
	}
	return strongRef{URI: out.URI, CID: out.CID}, nil
}

func opPostCreate(ctx plugin.Ctx, in *BskyPostCreateIn) (*BskyPostCreateOut, error) {
	var reply *replyRef
	if uri := strings.TrimSpace(in.ReplyToURI); uri != "" {
		r, err := replyTo(ctx, uri)
		if err != nil {
			return nil, err
		}
		reply = r
	}
	ref, err := publish(ctx, publishOpts{
		Text: in.Text, Langs: in.Langs, Images: in.Images, ImgAlts: in.ImageAlts,
		CardURL: in.LinkCardURL, Reply: reply,
	})
	if err != nil {
		return nil, err
	}
	s, _, _, _ := sessionFor(ctx)
	return &BskyPostCreateOut{URI: ref.URI, Cid: ref.CID, URL: webURL(handleOf(s), ref.URI)}, nil
}

func opPostThread(ctx plugin.Ctx, in *BskyPostThreadIn) (*BskyPostThreadOut, error) {
	texts := nonEmpty(in.Texts)
	if len(texts) == 0 {
		return nil, fmt.Errorf("一条正文都没有")
	}
	gap := time.Duration(in.IntervalMs) * time.Millisecond
	if in.IntervalMs <= 0 {
		gap = 500 * time.Millisecond
	}
	var reply *replyRef
	if uri := strings.TrimSpace(in.ReplyToURI); uri != "" {
		r, err := replyTo(ctx, uri)
		if err != nil {
			return nil, err
		}
		reply = r
	}

	uris := make([]string, 0, len(texts))
	var root strongRef
	for i, t := range texts {
		o := publishOpts{Text: t, Langs: in.Langs, Reply: reply}
		if i == 0 {
			o.Images = in.Images
		}
		ref, err := publish(ctx, o)
		if err != nil {
			// 中途失败**不回滚**：前面几条已经在公开时间线上了，删掉是二次破坏。
			return nil, fmt.Errorf("帖串发到第 %d 条失败（前 %d 条已发出：%s）: %w",
				i+1, len(uris), strings.Join(uris, ","), err)
		}
		uris = append(uris, ref.URI)
		if i == 0 && reply == nil {
			root = ref
		} else if i == 0 {
			root = reply.Root
		}
		reply = &replyRef{Root: root, Parent: ref}
		if i < len(texts)-1 && gap > 0 {
			t := time.NewTimer(gap)
			select {
			case <-t.C:
			case <-ctx.Done():
				t.Stop()
				return nil, fmt.Errorf("帖串被中断（已发出 %d 条：%s）", len(uris), strings.Join(uris, ","))
			}
		}
	}
	s, _, _, _ := sessionFor(ctx)
	return &BskyPostThreadOut{
		Uris: uris, RootURI: uris[0], RootURL: webURL(handleOf(s), uris[0]), Count: len(uris),
	}, nil
}

func opPostDelete(ctx plugin.Ctx, in *BskyPostDeleteIn) (*BskyPostDeleteOut, error) {
	uri, err := atURI(ctx, in.URI)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.TrimPrefix(uri, "at://"), "/")
	if len(parts) != 3 {
		return nil, fmt.Errorf("认不出这个地址: %s", in.URI)
	}
	if err := call(ctx, http.MethodPost, "com.atproto.repo.deleteRecord", nil, map[string]any{
		"repo": parts[0], "collection": parts[1], "rkey": parts[2],
	}, nil); err != nil {
		return nil, err
	}
	return &BskyPostDeleteOut{Deleted: true}, nil
}

func opHealthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	// 不用缓存的会话：健康检查的意义就是**真去换一次**。
	cred := credOf(ctx)
	s, err := login(ctx, clientFor(cred.Proxy), cred)
	if err != nil {
		return &HealthCheckOut{OK: false, Message: err.Error()}, nil
	}
	return &HealthCheckOut{OK: true, Handle: s.Handle, Message: "@" + s.Handle}, nil
}

// replyTo：把用户给的地址变成 reply 引用（要 root + parent）。
//
// root 取被回复帖子自己的 root——回复一条已经在串里的帖子时，新帖应当挂在**同一个串的根**下，
// 而不是把被回复的那条当根。搞错的话，Bluesky 会把它显示成一条新串。
func replyTo(ctx plugin.Ctx, raw string) (*replyRef, error) {
	uri, err := atURI(ctx, raw)
	if err != nil {
		return nil, err
	}
	var out struct {
		Thread struct {
			Post struct {
				URI    string `json:"uri"`
				CID    string `json:"cid"`
				Record struct {
					Reply *struct {
						Root strongRef `json:"root"`
					} `json:"reply"`
				} `json:"record"`
			} `json:"post"`
		} `json:"thread"`
	}
	if err := call(ctx, http.MethodGet, "app.bsky.feed.getPostThread",
		queryOf("uri", uri, "depth", "0", "parentHeight", "0"), nil, &out); err != nil {
		return nil, fmt.Errorf("取被回复的帖子失败: %w", err)
	}
	p := out.Thread.Post
	if p.URI == "" || p.CID == "" {
		return nil, fmt.Errorf("找不到这条帖子（可能已删除）: %s", raw)
	}
	parent := strongRef{URI: p.URI, CID: p.CID}
	root := parent
	if r := p.Record.Reply; r != nil && r.Root.URI != "" {
		root = r.Root
	}
	return &replyRef{Root: root, Parent: parent}, nil
}

func handleOf(s *session) string {
	if s == nil {
		return ""
	}
	return s.Handle
}

func credOf(ctx plugin.Ctx) Cred { return credFrom(ctx) }

func nonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
