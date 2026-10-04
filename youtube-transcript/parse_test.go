package main

import (
	"strings"
	"testing"

	"github.com/sokel-dev/sokel-official-plugins/youtube-transcript/schema"
)

// Users **paste straight from the address bar**. If only watch?v= were recognized, the youtu.be links
// shared from phones, Shorts links, and live-replay links would all get a "video id is invalid" error,
// while the user is staring at a link they're sure is perfectly valid.
func TestExtractVideoID(t *testing.T) {
	const want = "dQw4w9WgXcQ"
	ok := []struct{ name, in string }{
		{"裸 id", "dQw4w9WgXcQ"},
		{"标准 watch", "https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
		{"watch 带其它参数在前", "https://www.youtube.com/watch?app=desktop&v=dQw4w9WgXcQ"},
		{"watch 带时间戳", "https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=42s"},
		{"短链", "https://youtu.be/dQw4w9WgXcQ"},
		{"短链带参数", "https://youtu.be/dQw4w9WgXcQ?si=abcdef"},
		{"Shorts", "https://www.youtube.com/shorts/dQw4w9WgXcQ"},
		{"嵌入", "https://www.youtube.com/embed/dQw4w9WgXcQ"},
		{"直播回放", "https://www.youtube.com/live/dQw4w9WgXcQ"},
		{"手机站", "https://m.youtube.com/watch?v=dQw4w9WgXcQ"},
		{"前后有空格", "  https://youtu.be/dQw4w9WgXcQ  "},
	}
	for _, c := range ok {
		got, err := ExtractVideoID(c.in)
		if err != nil {
			t.Errorf("%s：应认出来，却报错 %v", c.name, err)
			continue
		}
		if got != want {
			t.Errorf("%s：want %s got %s", c.name, want, got)
		}
	}

	bad := []struct{ name, in string }{
		{"空", ""},
		{"只有空格", "   "},
		{"不是 YouTube", "https://vimeo.com/123456"},
		{"id 短了一位", "dQw4w9WgXc"},
		{"频道页没有视频 id", "https://www.youtube.com/@someone"},
	}
	for _, c := range bad {
		if _, err := ExtractVideoID(c.in); err == nil {
			t.Errorf("%s：应报错，却认成了合法输入", c.name)
		}
	}
}

// The sample is written to match YouTube's **actual double-escaping**: `<b>` is `&amp;lt;b&amp;gt;` in
// the file, and `&` in speech is `&amp;amp;`. encoding/xml unescapes the first layer, cleanText unescapes
// the second.
//
// In the first version I had the order backwards (strip tags, then unescape entities), and the
// assertions were written to match that mistake, so "tests pass" only proved the implementation matched
// my misunderstanding. These cases are what exposed it: stripping tags first leaves them as `&lt;b&gt;`,
// none of them get removed, and the two preserve_formatting settings produce identical output.
const sampleXML = `<?xml version="1.0" encoding="utf-8" ?><transcript>
<text start="0.5" dur="1.2">Hello &amp;amp; welcome</text>
<text start="2.0" dur="3.4">line with &amp;lt;b&amp;gt;bold&amp;lt;/b&amp;gt; markup</text>
<text start="6.0" dur="1.0">
</text>
<text start="7.0" dur="2.5">it&amp;#39;s fine</text>
</transcript>`

func TestParseTimedText(t *testing.T) {
	got, err := ParseTimedText(sampleXML, false)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	// Newline-only placeholder entries should be dropped: keeping them would inflate the count and
	// leave extra spaces scattered through the joined text.
	if len(got) != 3 {
		t.Fatalf("应剩 3 句（空白句被丢），实际 %d：%+v", len(got), got)
	}
	if got[0].Text != "Hello & welcome" {
		t.Errorf("两层转义要解干净，实际 %q", got[0].Text)
	}
	if got[0].Start != 0.5 || got[0].Duration != 1.2 {
		t.Errorf("时间轴要照抄，实际 start=%v dur=%v", got[0].Start, got[0].Duration)
	}
	// The double-escaped <b> is a **formatting tag**, and the default setting should strip it (not keep
	// it as a literal string).
	if got[1].Text != "line with bold markup" {
		t.Errorf("默认应去掉内联标签，实际 %q", got[1].Text)
	}
	if got[2].Text != "it's fine" {
		t.Errorf("数字实体也要解，实际 %q", got[2].Text)
	}
}

func TestParseTimedTextPreserveFormatting(t *testing.T) {
	got, err := ParseTimedText(sampleXML, true)
	if err != nil {
		t.Fatal(err)
	}
	// The allowlisted <b> is kept — this is exactly the only difference between preserve_formatting and
	// the default setting. If both settings produced the same output, it would mean the unescape step
	// wasn't taking effect (which is exactly what the first version did).
	if got[1].Text != "line with <b>bold</b> markup" {
		t.Errorf("保留格式时白名单标签应留下，实际 %q", got[1].Text)
	}
	if got[0].Text != "Hello & welcome" {
		t.Errorf("实体照样要解，实际 %q", got[0].Text)
	}
}

// A tag that's not in the allowlist must be stripped in both settings.
func TestParseTimedTextDropsNonWhitelistTags(t *testing.T) {
	const withFont = `<transcript><text start="0" dur="1">a &amp;lt;font color="#fff"&amp;gt;see&amp;lt;/font&amp;gt; b</text></transcript>`
	for _, preserve := range []bool{false, true} {
		got, err := ParseTimedText(withFont, preserve)
		if err != nil {
			t.Fatal(err)
		}
		if got[0].Text != "a see b" {
			t.Errorf("preserve=%v：<font> 不在白名单应被删，实际 %q", preserve, got[0].Text)
		}
	}
}

// An empty track is a normal result (common right after a livestream starts), and shouldn't be an error.
func TestParseTimedTextEmpty(t *testing.T) {
	for _, in := range []string{"", "   ", "<transcript></transcript>"} {
		got, err := ParseTimedText(in, false)
		if err != nil {
			t.Errorf("%q：空内容不该报错，实际 %v", in, err)
		}
		if len(got) != 0 {
			t.Errorf("%q：应是空切片，实际 %+v", in, got)
		}
	}
}

func tr(code string, generated bool) track {
	return track{Info: schema.TrackInfo{LanguageCode: code, IsGenerated: generated, Language: code}}
}

// Track-picking criteria are ordered **language before kind**: when a user writes `zh-Hans,en`, they mean
// "Chinese matters more than English", so when a Chinese machine translation exists, it shouldn't be
// skipped in favor of English just because "English has a manual transcript".
func TestPickTrackLanguageBeatsKind(t *testing.T) {
	tracks := []track{tr("en", false), tr("zh-Hans", true)}
	got, err := pickTrack(tracks, []string{"zh-Hans", "en"}, "any")
	if err != nil {
		t.Fatal(err)
	}
	if got.Info.LanguageCode != "zh-Hans" {
		t.Errorf("应选中文（语言优先级更高），实际 %s", got.Info.LanguageCode)
	}
}

func TestPickTrackManualFirstWithinLanguage(t *testing.T) {
	tracks := []track{tr("en", true), tr("en", false)}
	got, err := pickTrack(tracks, []string{"en"}, "any")
	if err != nil {
		t.Fatal(err)
	}
	if got.Info.IsGenerated {
		t.Error("同语言下应优先人工字幕")
	}
}

func TestPickTrackHardFilters(t *testing.T) {
	tracks := []track{tr("en", true), tr("ja", false)}

	// manual is a hard filter: en only has an auto-generated one → skip en, move on to the next language.
	got, err := pickTrack(tracks, []string{"en", "ja"}, "manual")
	if err != nil {
		t.Fatal(err)
	}
	if got.Info.LanguageCode != "ja" {
		t.Errorf("限定人工时应跳过只有自动字幕的 en，实际选了 %s", got.Info.LanguageCode)
	}

	// None of them satisfy it → error, and the existing ones must be listed (otherwise the user has no
	// idea what to change it to).
	_, err = pickTrack(tracks, []string{"de"}, "manual")
	if err == nil {
		t.Fatal("没有匹配应报错")
	}
	for _, want := range []string{"en", "ja", "人工"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("报错里应含 %q，实际 %q", want, err.Error())
		}
	}
}

func TestPickTrackNoTracks(t *testing.T) {
	if _, err := pickTrack(nil, []string{"en"}, "any"); err == nil {
		t.Error("没有任何字幕轨时应报错")
	}
}

func TestParseLanguages(t *testing.T) {
	cases := map[string][]string{
		"":                 {"en"},
		"  ":               {"en"},
		"zh-Hans":          {"zh-Hans"},
		"zh-Hans, zh , en": {"zh-Hans", "zh", "en"},
		",,en,,":           {"en"},
	}
	for in, want := range cases {
		got := parseLanguages(in)
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("%q：want %v got %v", in, want, got)
		}
	}
}

func TestSortTracksManualFirst(t *testing.T) {
	got := sortTracks([]track{tr("en", true), tr("ja", false), tr("de", true), tr("fr", false)})
	order := make([]string, len(got))
	for i, x := range got {
		order[i] = x.LanguageCode
	}
	// Manual ones come first, and **the original order within each kind is preserved** (YouTube's order
	// carries meaning: the default track comes first).
	if strings.Join(order, ",") != "ja,fr,en,de" {
		t.Errorf("want ja,fr,en,de got %s", strings.Join(order, ","))
	}
}

func TestJoinText(t *testing.T) {
	// Space, not newline: timedtext line breaks are cut by display width, not by meaning, and a newline
	// would make a downstream LLM think it's a paragraph boundary.
	got := JoinText([]schema.Snippet{{Text: " a "}, {Text: "b"}, {Text: "c "}})
	if got != "a b c" {
		t.Errorf("want %q got %q", "a b c", got)
	}
}

// Newlines **within a line** must be flattened too. A real sample (pulled from a smoke test) looks
// exactly like this: the player breaks the line when it doesn't fit on one display line, unrelated to
// meaning.
func TestJoinTextFlattensNewlinesInsideSnippet(t *testing.T) {
	in := []schema.Snippet{
		{Text: "♪ A full commitment's\n        what I'm thinking of ♪"},
		{Text: "next"},
	}
	want := "♪ A full commitment's what I'm thinking of ♪ next"
	if got := JoinText(in); got != want {
		t.Errorf("want %q\n got %q", want, got)
	}
}

// The snippets field is unaffected: the newline is part of the original data, and flattening it there
// couldn't be undone.
func TestSnippetsKeepRawWhitespace(t *testing.T) {
	const x = `<transcript><text start="0" dur="1">a
        b</text></transcript>`
	got, err := ParseTimedText(x, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got[0].Text, "\n") {
		t.Errorf("分句应保留原始换行，实际 %q", got[0].Text)
	}
}

// Language matching must accept "regional variants".
//
// Reported in practice (2026-08-26, a user hit this on their very first use):
//
//	插件执行失败: 没有匹配的字幕（要的是 en）。这个视频现有：en-US(人工)
//	("plugin execution failed: no matching transcript (wanted en). This video has: en-US (manual)")
//
// And `en` is exactly the default value — meaning **as soon as a video's track is en-US, the default
// config always fails**, through no fault of the user's. The reference project (Python version) does an
// exact dict lookup and hits the same pitfall; we deliberately don't copy that.
//
// Criteria: the requested language and a track's language match as long as they share the same **primary
// subtag** (the part before `-`); within the same family, manual/auto-generated is ranked first
// (prefer=any favors manual), then an exact code match breaks a tie.
func TestPickTrackMatchesRegionalVariants(t *testing.T) {
	cases := []struct {
		name   string
		tracks []track
		want   []string // requested languages
		prefer string
		expect string
	}{
		{"要 en，只有 en-US（实报）", []track{tr("en-US", false)}, []string{"en"}, "any", "en-US"},
		{"要 en-US，只有 en", []track{tr("en", false)}, []string{"en-US"}, "any", "en"},
		{"要 en，只有 en-GB 自动", []track{tr("en-GB", true)}, []string{"en"}, "any", "en-GB"},
		// Within the same family, manual still wins — that's the entire point of prefer=any.
		{"同族内人工优先于精确", []track{tr("en", true), tr("en-US", false)}, []string{"en"}, "any", "en-US"},
		// When the kind is the same, the exact code wins: a user writing en wants en specifically.
		{"同为人工时精确码胜出", []track{tr("en-GB", false), tr("en", false)}, []string{"en"}, "any", "en"},
		// Cross-language priority is unaffected: zh ranked ahead of en shouldn't be skipped just because
		// en has a manual transcript.
		{"语言优先级仍高于一切", []track{tr("en", false), tr("zh-Hans", true)}, []string{"zh", "en"}, "any", "zh-Hans"},
		// Hard filter still applies: the en family only has an auto-generated one → skip the whole
		// family, move to the next language.
		{"限定人工时同族没有就跳族", []track{tr("en-US", true), tr("ja", false)}, []string{"en", "ja"}, "manual", "ja"},
	}
	for _, c := range cases {
		got, err := pickTrack(c.tracks, c.want, c.prefer)
		if err != nil {
			t.Errorf("%s：应命中 %s，却报错 %v", c.name, c.expect, err)
			continue
		}
		if got.Info.LanguageCode != c.expect {
			t.Errorf("%s：want %s got %s", c.name, c.expect, got.Info.LanguageCode)
		}
	}
}

// Only counts as "no match" when not even the same family exists, and the error must still list what's
// available.
func TestPickTrackStillFailsOnDifferentLanguage(t *testing.T) {
	_, err := pickTrack([]track{tr("ja", false), tr("ko", false)}, []string{"en"}, "any")
	if err == nil {
		t.Fatal("完全不同的语言应报错")
	}
	for _, want := range []string{"ja", "ko"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("报错应列出现有语言 %q，实际 %q", want, err.Error())
		}
	}
}
