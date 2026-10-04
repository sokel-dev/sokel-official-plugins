package main

// 富文本：链接 / #话题 / @提及 的 facets。
//
// **下标是 UTF-8 字节偏移，不是字符下标**。这是 AT Protocol 最容易踩的一脚：
// 一条「A股放量 https://example.com 走强」按字符数算下标，链接会整体错位——
// 中文一个字占 3 字节，前面每有一个中文就偏 2 个位置。表现是点开链接落在正文中间，
// 或者干脆报 InvalidRequest。所以这里全程按 []byte 走。
//
// 另一条：**没有 facets 的链接在 Bluesky 上不可点**。它不像别家会自动认 URL，
// 你不给 facets 就是一串纯文本。

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

// facet：一段富文本标注。
type facet struct {
	Index    facetIndex    `json:"index"`
	Features []facetFeatur `json:"features"`
}

type facetIndex struct {
	ByteStart int `json:"byteStart"`
	ByteEnd   int `json:"byteEnd"`
}

type facetFeatur struct {
	Type string `json:"$type"`
	URI  string `json:"uri,omitempty"` // link
	Tag  string `json:"tag,omitempty"` // hashtag
	DID  string `json:"did,omitempty"` // mention
}

// 链接：不含空白与常见收尾标点。末尾的中英文标点要剔掉——
// 「详见 https://example.com/a。」里的句号不是链接的一部分。
var linkRe = regexp.MustCompile(`https?://[^\s<>"'）)】\]}，。；！？]+`)

// 话题：#后面跟非空白非标点。中文话题很常见（#A股），所以不能只认 ASCII。
var tagRe = regexp.MustCompile(`#([^\s#.,;:!?"'（）()【】\[\]]+)`)

// 提及：@handle，至少带一个点（Bluesky 的 handle 是域名形态，如 acme.bsky.social）。
var mentionRe = regexp.MustCompile(`@([a-zA-Z0-9][a-zA-Z0-9-]*(?:\.[a-zA-Z0-9][a-zA-Z0-9-]*)+)`)

// resolver：handle → did。抽成函数参数是为了测试不必起假 PDS。
type resolver func(handle string) (string, error)

// buildFacets：扫一遍正文，产出 facets 与「正文里的第一个链接」（拿去做卡片）。
func buildFacets(text string, resolve resolver) ([]facet, string) {
	b := []byte(text)
	var out []facet
	firstLink := ""

	for _, m := range linkRe.FindAllIndex(b, -1) {
		uri := string(b[m[0]:m[1]])
		if firstLink == "" {
			firstLink = uri
		}
		out = append(out, facet{
			Index:    facetIndex{ByteStart: m[0], ByteEnd: m[1]},
			Features: []facetFeatur{{Type: "app.bsky.richtext.facet#link", URI: uri}},
		})
	}
	for _, m := range tagRe.FindAllSubmatchIndex(b, -1) {
		if overlaps(out, m[0], m[1]) { // URL 里的 # 锚点不是话题
			continue
		}
		out = append(out, facet{
			Index:    facetIndex{ByteStart: m[0], ByteEnd: m[1]},
			Features: []facetFeatur{{Type: "app.bsky.richtext.facet#tag", Tag: string(b[m[2]:m[3]])}},
		})
	}
	for _, m := range mentionRe.FindAllSubmatchIndex(b, -1) {
		if overlaps(out, m[0], m[1]) {
			continue
		}
		handle := string(b[m[2]:m[3]])
		did, err := resolve(handle)
		if err != nil || did == "" {
			continue // 认不出的 @ 就当普通文本，别让一个手滑的用户名挡住整条发布
		}
		out = append(out, facet{
			Index:    facetIndex{ByteStart: m[0], ByteEnd: m[1]},
			Features: []facetFeatur{{Type: "app.bsky.richtext.facet#mention", DID: did}},
		})
	}
	return out, firstLink
}

func overlaps(fs []facet, start, end int) bool {
	for _, f := range fs {
		if start < f.Index.ByteEnd && f.Index.ByteStart < end {
			return true
		}
	}
	return false
}

// resolverFor：真正去问 PDS 的解析器（带一次调用内的缓存——同一个 @ 在帖串里会出现很多次）。
func resolverFor(ctx plugin.Ctx) resolver {
	cache := map[string]string{}
	return func(handle string) (string, error) {
		if did, ok := cache[handle]; ok {
			return did, nil
		}
		var out struct {
			DID string `json:"did"`
		}
		if err := call(ctx, http.MethodGet, "com.atproto.identity.resolveHandle",
			url.Values{"handle": {handle}}, nil, &out); err != nil {
			return "", err
		}
		cache[handle] = out.DID
		return out.DID, nil
	}
}

// graphemes：Bluesky 的 300 上限按**字形**算。这里用码点近似——
// 差异只出现在 emoji 组合序列上（👨‍👩‍👧 算 1 个字形但 3 个码点），
// 近似的方向是「我们数得更多」，即宁可提前拦下，不会让用户发出去才被拒。
func graphemes(s string) int { return utf8.RuneCountInString(s) }

// trimTrailingPunct：卡片 URL 末尾的标点（正文里粘出来的）。
func trimTrailingPunct(s string) string {
	return strings.TrimRight(s, "。，、；：！？）)】]}»\"'")
}
