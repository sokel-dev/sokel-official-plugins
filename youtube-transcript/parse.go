package main

import (
	"encoding/xml"
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/sokel-dev/sokel-official-plugins/youtube-transcript/schema"
)

// Everything in this file is a **pure function**: it computes from its inputs alone, never touches the
// network. It's split this way because the parts of this plugin that are actually easy to get wrong
// (what an id looks like, how to parse the XML, in what order to pick a track) are exactly the parts
// that can be tested offline; the other half (HTTP + anti-bot handling) can't be tested and has to rely
// on clear error mapping instead.

// videoIDRe is the character set for an 11-character YouTube video id.
var videoIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// urlIDRe extracts the id from the many shapes a YouTube link can take.
//
// YouTube link formats are absurdly varied, and users **paste straight from the address bar** — if we
// only recognized watch?v=, the youtu.be links shared from phones, Shorts links, and live-replay links
// would all get a "video id is invalid" error, while the user is staring at a link they're sure is
// perfectly valid.
var urlIDRe = regexp.MustCompile(`(?:youtu\.be/|/shorts/|/embed/|/live/|/v/|[?&]v=)([A-Za-z0-9_-]{11})`)

// ExtractVideoID normalizes whatever the user typed into an 11-character id.
func ExtractVideoID(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("没填视频：给个 YouTube 链接或 11 位视频 id")
	}
	if videoIDRe.MatchString(s) {
		return s, nil
	}
	if m := urlIDRe.FindStringSubmatch(s); m != nil {
		return m[1], nil
	}
	return "", fmt.Errorf("认不出这是哪个视频：%q。支持 watch?v=… / youtu.be/… / shorts/… / embed/… / live/…，或直接填 11 位 id", s)
}

// formattingTags is the allowlist of inline formatting tags (matches the reference project). When
// preserve_formatting is on, these are kept and everything else is stripped.
var formattingTags = []string{"strong", "em", "b", "i", "mark", "small", "del", "ins", "sub", "sup"}

var (
	stripAllTags = regexp.MustCompile(`<[^>]*>`)
	// When preserving the allowlist: strip tags that are "not in the allowlist". Go's regexp doesn't
	// support lookahead, so the reference project's single regex approach isn't possible here — instead
	// each match is checked against the tag name individually (equivalent, and more readable).
	anyTag = regexp.MustCompile(`</?([A-Za-z0-9]+)[^>]*>`)
)

// cleanText unescapes HTML entities and strips tags as needed. raw is chardata that encoding/xml has
// already unescaped one level.
//
// **The order is: unescape entities first, then strip tags.** This was forced out by a test (my first
// version had it backwards): inline tags in timedtext are **double-escaped** — `<b>` is written in the
// file as `&amp;lt;b&amp;gt;`, the XML layer unescapes one level into `&lt;b&gt;`, and only the HTML
// layer's unescape reveals `<b>`. Stripping tags first would hit it while it's still `&lt;b&gt;`, and the
// regex wouldn't match any of it, so the two preserve_formatting settings would produce identical output
// (both still full of literal <b> text).
//
// The same applies to `&` in speech: it's `&amp;amp;` in the file, and only becomes a single `&` after
// both unescape passes.
func cleanText(raw string, preserveFormatting bool) string {
	raw = html.UnescapeString(raw)
	var stripped string
	if preserveFormatting {
		keep := make(map[string]bool, len(formattingTags))
		for _, t := range formattingTags {
			keep[t] = true
		}
		stripped = anyTag.ReplaceAllStringFunc(raw, func(m string) string {
			name := strings.ToLower(anyTag.FindStringSubmatch(m)[1])
			if keep[name] {
				return m
			}
			return ""
		})
	} else {
		stripped = stripAllTags.ReplaceAllString(raw, "")
	}
	return stripped
}

// timedTextXML is the XML shape of the YouTube timedtext endpoint.
type timedTextXML struct {
	Texts []struct {
		Start float64 `xml:"start,attr"`
		Dur   float64 `xml:"dur,attr"`
		Body  string  `xml:",chardata"`
	} `xml:"text"`
}

// ParseTimedText parses timedtext XML into lines.
//
// An empty string / empty <transcript/> is a **normal** result (the track exists but has no content yet,
// common right after a livestream starts) and returns an empty slice without an error; only a genuine
// parse failure is an error.
func ParseTimedText(raw string, preserveFormatting bool) ([]schema.Snippet, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []schema.Snippet{}, nil
	}
	var doc timedTextXML
	if err := xml.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, fmt.Errorf("字幕内容解析失败（YouTube 返回的不是预期的 XML，可能是被风控挡了）: %w", err)
	}
	out := make([]schema.Snippet, 0, len(doc.Texts))
	for _, t := range doc.Texts {
		text := cleanText(t.Body, preserveFormatting)
		// Drop lines that are entirely blank: timedtext contains placeholder entries that are just a
		// newline, and keeping them would inflate the count and leave extra spaces scattered through
		// the joined text.
		if strings.TrimSpace(text) == "" {
			continue
		}
		out = append(out, schema.Snippet{Text: text, Start: t.Start, Duration: t.Dur})
	}
	return out, nil
}

// wsRun matches a run of whitespace (including newlines) → collapsed to one space.
var wsRun = regexp.MustCompile(`\s+`)

// JoinText joins the lines into full text.
//
// Two things happen: joining with spaces, and flattening newlines **within a line** too. timedtext's
// line breaks are cut by how much text fits on one player display line, unrelated to meaning — a real
// sample line looks like: "♪ A full commitment's\n        what I'm thinking of ♪". Leaving the newline
// in would make a downstream LLM treat it as a paragraph boundary.
//
// Only the joined text is flattened this way, **snippets are left untouched**: that field is meant for
// building a timeline/subtitle file, the newline is part of the original data, and flattening it there
// couldn't be undone.
func JoinText(ss []schema.Snippet) string {
	parts := make([]string, 0, len(ss))
	for _, s := range ss {
		if t := strings.TrimSpace(wsRun.ReplaceAllString(s.Text, " ")); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, " ")
}

// track is one transcript track (internal shape, with the URL used to fetch its content).
type track struct {
	Info schema.TrackInfo
	URL  string
}

// baseLang returns the primary subtag of a language code (the part before `-`), lowercased.
// en-US → en, zh-Hans → zh.
func baseLang(code string) string {
	if i := strings.IndexByte(code, '-'); i >= 0 {
		code = code[:i]
	}
	return strings.ToLower(code)
}

// pickTrack picks one track by "language priority × transcript kind".
//
// The criteria are ordered **language before kind**: when a user writes `zh-Hans,en`, they mean "Chinese
// matters more than English", so when a Chinese machine translation exists, it shouldn't be skipped in
// favor of English just because "English has a manual transcript". The reference project uses this same
// ordering.
//
// **Matching the base language family counts as a hit** (en accepts en-US / en-GB, and vice versa). The
// reference project does an exact dict lookup; this is deliberately made looser than that — reported in
// practice (2026-08-26, a user hit this on their very first use):
//
//	插件执行失败: 没有匹配的字幕（要的是 en）。这个视频现有：en-US(人工)
//	("plugin execution failed: no matching transcript (wanted en). This video has: en-US (manual)")
//
// And `en` is exactly the default value: as soon as a video's track carries a regional suffix, the
// default config would always fail, through no fault of the user's.
//
// Ordering within the same language family: kind is checked first (when prefer=any, manual wins —
// that's the entire point of this tier), and when the kind is the same, an exact code match breaks the
// tie (a user writing en wants en specifically, not en-GB). prefer=manual/generated is a hard filter: if
// the whole family fails it, move on to the next requested language.
func pickTrack(tracks []track, languages []string, prefer string) (track, error) {
	if len(tracks) == 0 {
		return track{}, fmt.Errorf("这个视频没有开放任何字幕")
	}
	allowed := func(t track) bool {
		switch prefer {
		case "manual":
			return !t.Info.IsGenerated
		case "generated":
			return t.Info.IsGenerated
		}
		return true
	}
	// Lower is better: manual (0) beats auto-generated (2); within the same kind, an exact code match
	// (-1) beats same-family (0).
	score := func(t track, lang string) int {
		s := 0
		if t.Info.IsGenerated {
			s += 2
		}
		if !strings.EqualFold(t.Info.LanguageCode, lang) {
			s++
		}
		return s
	}
	for _, lang := range languages {
		best, bestScore := -1, 0
		for i := range tracks {
			if baseLang(tracks[i].Info.LanguageCode) != baseLang(lang) || !allowed(tracks[i]) {
				continue
			}
			if sc := score(tracks[i], lang); best < 0 || sc < bestScore {
				best, bestScore = i, sc
			}
		}
		if best >= 0 {
			return tracks[best], nil
		}
	}
	// The error must **list what's actually available**: just saying "zh-Hans not found" leaves the
	// user with no idea what to change it to.
	have := make([]string, 0, len(tracks))
	for _, t := range tracks {
		kind := "人工"
		if t.Info.IsGenerated {
			kind = "自动"
		}
		have = append(have, fmt.Sprintf("%s(%s)", t.Info.LanguageCode, kind))
	}
	want := strings.Join(languages, ", ")
	if prefer != "any" {
		want += fmt.Sprintf("；且限定%s字幕", map[string]string{"manual": "人工", "generated": "自动生成的"}[prefer])
	}
	return track{}, fmt.Errorf("没有匹配的字幕（要的是 %s）。这个视频现有：%s", want, strings.Join(have, "、"))
}

// parseLanguages splits a comma-separated priority string into a list; falls back to en when it's
// entirely empty.
func parseLanguages(s string) []string {
	out := []string{}
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return []string{"en"}
	}
	return out
}

// sortTracks puts manual transcripts ahead of auto-generated ones (used when listing).
// Stable: within each kind, YouTube's original order is preserved, and that order itself carries meaning
// (the default track comes first).
func sortTracks(tracks []track) []schema.TrackInfo {
	out := make([]schema.TrackInfo, 0, len(tracks))
	for _, t := range tracks {
		if !t.Info.IsGenerated {
			out = append(out, t.Info)
		}
	}
	for _, t := range tracks {
		if t.Info.IsGenerated {
			out = append(out, t.Info)
		}
	}
	return out
}
