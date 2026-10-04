package main

import (
	"strings"
	"testing"

	"github.com/sokel-dev/sokel-official-plugins/youtube-transcript/schema"
)

// 用户是**从地址栏直接粘**的。只认 watch?v= 的话，手机分享出来的 youtu.be、Shorts、
// 直播回放全都会得到一句「视频 id 不合法」，而他看着自己粘的明明是个好链接。
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

// 样本按 YouTube **真实的双重转义**写：`<b>` 在文件里是 `&amp;lt;b&amp;gt;`，
// 语音里的 `&` 是 `&amp;amp;`。encoding/xml 解掉第一层，cleanText 解第二层。
//
// 第一版我把顺序写反了（先去标签再解实体），断言也照着错的写，于是「测试通过」
// 只证明了实现与我的误解一致。是这几条撞出来的：先去标签时标签还是 `&lt;b&gt;`，
// 一个都删不掉，preserve_formatting 两档产出完全一样。
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
	// 只有换行的占位条目要丢掉：留着会让 count 虚高、拼出来的全文多一堆空格。
	if len(got) != 3 {
		t.Fatalf("应剩 3 句（空白句被丢），实际 %d：%+v", len(got), got)
	}
	if got[0].Text != "Hello & welcome" {
		t.Errorf("两层转义要解干净，实际 %q", got[0].Text)
	}
	if got[0].Start != 0.5 || got[0].Duration != 1.2 {
		t.Errorf("时间轴要照抄，实际 start=%v dur=%v", got[0].Start, got[0].Duration)
	}
	// 双重转义的 <b> 是**格式标签**，默认档要删掉（不是当字面量留着）。
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
	// 白名单里的 <b> 留下来 —— 这正是 preserve_formatting 与默认档的唯一区别。
	// 两档产出相同就说明解转义那步没生效（第一版就是这样）。
	if got[1].Text != "line with <b>bold</b> markup" {
		t.Errorf("保留格式时白名单标签应留下，实际 %q", got[1].Text)
	}
	if got[0].Text != "Hello & welcome" {
		t.Errorf("实体照样要解，实际 %q", got[0].Text)
	}
}

// 不在白名单里的标签，两档都得删。
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

// 空轨是正常结果（直播刚开始时常见），不该报错。
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

// 选轨的判据顺序是**语言优先于类型**：用户写 `zh-Hans,en` 的意思是「中文比英文重要」，
// 有中文机翻时就不该因为「英文有人工字幕」而跳去英文。
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

	// manual 是硬过滤：en 只有自动的 → 跳过 en，继续找下一个语言。
	got, err := pickTrack(tracks, []string{"en", "ja"}, "manual")
	if err != nil {
		t.Fatal(err)
	}
	if got.Info.LanguageCode != "ja" {
		t.Errorf("限定人工时应跳过只有自动字幕的 en，实际选了 %s", got.Info.LanguageCode)
	}

	// 一个都不满足 → 报错，且必须把现有的列出来（否则用户不知道该改成什么）。
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
	// 人工在前，且**同类内保持原顺序**（YouTube 给的顺序有含义：默认轨在前）。
	if strings.Join(order, ",") != "ja,fr,en,de" {
		t.Errorf("want ja,fr,en,de got %s", strings.Join(order, ","))
	}
}

func TestJoinText(t *testing.T) {
	// 用空格而不是换行：timedtext 是按显示时长切的，不是按语义，
	// 换行会让下游 LLM 以为那是段落边界。
	got := JoinText([]schema.Snippet{{Text: " a "}, {Text: "b"}, {Text: "c "}})
	if got != "a b c" {
		t.Errorf("want %q got %q", "a b c", got)
	}
}

// **句内**的换行也要压平。真实样本（冒烟测试里取到的）就长这样：
// 播放器一行显示不下就断行，与语义无关。
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

// snippets 那一份不受影响：换行是原始数据，压掉就还原不回去了。
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

// 语言匹配必须认「地区变体」。
//
// 实报（2026-08-26，用户第一次用就撞上）：
//
//	插件执行失败: 没有匹配的字幕（要的是 en）。这个视频现有：en-US(人工)
//
// 而 `en` 恰恰是默认值 —— 也就是说**只要视频的轨是 en-US，默认配置就必挂**，
// 而用户没做错任何事。参考项目（Python 版）是精确匹配 dict 查找，同样的坑；
// 这一条我们不照抄。
//
// 判据：请求语言与轨语言按**主子标签**（`-` 之前那段）同族即可命中；
// 同族之内，先按人工/自动排（prefer=any 时人工优先），再拿「代码完全相同」当同分时的胜负手。
func TestPickTrackMatchesRegionalVariants(t *testing.T) {
	cases := []struct {
		name   string
		tracks []track
		want   []string // 请求语言
		prefer string
		expect string
	}{
		{"要 en，只有 en-US（实报）", []track{tr("en-US", false)}, []string{"en"}, "any", "en-US"},
		{"要 en-US，只有 en", []track{tr("en", false)}, []string{"en-US"}, "any", "en"},
		{"要 en，只有 en-GB 自动", []track{tr("en-GB", true)}, []string{"en"}, "any", "en-GB"},
		// 同族之内仍是人工优先——这是 prefer=any 的全部意义。
		{"同族内人工优先于精确", []track{tr("en", true), tr("en-US", false)}, []string{"en"}, "any", "en-US"},
		// 类型相同时，精确代码赢：用户写 en 就是更想要 en。
		{"同为人工时精确码胜出", []track{tr("en-GB", false), tr("en", false)}, []string{"en"}, "any", "en"},
		// 跨语言的优先级不受影响：zh 排在 en 前面，就不该因为 en 有人工字幕而跳过去。
		{"语言优先级仍高于一切", []track{tr("en", false), tr("zh-Hans", true)}, []string{"zh", "en"}, "any", "zh-Hans"},
		// 硬过滤照旧：en 族只有自动的 → 跳过整族，去下一个语言。
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

// 同族也没有时才算没有，且报错仍要列出现有的。
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
