package main

// Rich text: facets for links / #hashtags / @mentions.
//
// **Indices are UTF-8 byte offsets, not character indices.** This is the easiest mistake
// to make with AT Protocol: a post like "A股放量 https://example.com 走强" would have its
// link offset entirely wrong if indexed by character count — each CJK character takes 3
// bytes, so every one before the link shifts the position by 2. The symptom is the link
// landing in the middle of the text when clicked, or an outright InvalidRequest error. So
// everything here operates on []byte.
//
// Another rule: **a link with no facet is not clickable on Bluesky**. Unlike some other
// platforms, it doesn't auto-detect URLs — without a facet it's just plain text.

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

// facet: one rich-text annotation.
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

// Link: excludes whitespace and common trailing punctuation. Trailing CJK/ASCII
// punctuation must be stripped — the period in "see https://example.com/a。" is not part
// of the link.
var linkRe = regexp.MustCompile(`https?://[^\s<>"'）)】\]}，。；！？]+`)

// Hashtag: # followed by non-whitespace, non-punctuation. Chinese hashtags are common
// (#A股), so this can't accept ASCII only.
var tagRe = regexp.MustCompile(`#([^\s#.,;:!?"'（）()【】\[\]]+)`)

// Mention: @handle, with at least one dot (Bluesky handles are domain-shaped, e.g. acme.bsky.social).
var mentionRe = regexp.MustCompile(`@([a-zA-Z0-9][a-zA-Z0-9-]*(?:\.[a-zA-Z0-9][a-zA-Z0-9-]*)+)`)

// resolver: handle → did. Pulled out as a function parameter so tests don't need to spin up a fake PDS.
type resolver func(handle string) (string, error)

// buildFacets: scans the text once, producing facets plus "the first link in the text" (used for the card).
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
		if overlaps(out, m[0], m[1]) { // a # anchor inside a URL is not a hashtag
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
			continue // an unresolvable @ is treated as plain text; one mistyped handle shouldn't block the whole post
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

// resolverFor: the resolver that actually asks the PDS (with a per-call cache — the same @ can appear many times in a thread).
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

// graphemes: Bluesky's 300 limit counts **grapheme clusters**. This approximates with
// code points — the difference only shows up in emoji ZWJ sequences (👨‍👩‍👧 is 1
// grapheme but 3 code points), and the approximation errs on the side of counting more,
// i.e. we'd rather block early than let the user get rejected after hitting send.
func graphemes(s string) int { return utf8.RuneCountInString(s) }

// trimTrailingPunct: trailing punctuation on a card URL (picked up when pasted into the text).
func trimTrailingPunct(s string) string {
	return strings.TrimRight(s, "。，、；：！？）)】]}»\"'")
}
