package main

import (
	"encoding/xml"
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/sokel-dev/sokel-official-plugins/youtube-transcript/schema"
)

// 这个文件里全是**纯函数**：给什么算什么，不碰网络。
// 这么切是因为本插件真正容易错的地方（id 长什么样、XML 怎么解、按什么顺序选轨）
// 恰恰都能脱网测；而剩下那半（HTTP + 风控）测不了，只能靠错误映射说清楚。

// videoIDRe：11 位的 YouTube 视频 id 字母表。
var videoIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// urlIDRe：从各种形态的链接里抠 id。
//
// YouTube 的链接形态多得离谱，而用户是**从地址栏直接粘**的——只认 watch?v= 的话，
// 手机分享出来的 youtu.be、Shorts、直播回放全都会得到一句「视频 id 不合法」，
// 而用户看着自己粘的明明是个好链接。
var urlIDRe = regexp.MustCompile(`(?:youtu\.be/|/shorts/|/embed/|/live/|/v/|[?&]v=)([A-Za-z0-9_-]{11})`)

// ExtractVideoID 把用户填的东西归一成 11 位 id。
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

// 内联格式标签白名单（与参考项目一致）。preserve_formatting 时保留这些，其余照删。
var formattingTags = []string{"strong", "em", "b", "i", "mark", "small", "del", "ins", "sub", "sup"}

var (
	stripAllTags = regexp.MustCompile(`<[^>]*>`)
	// 保留白名单时：删掉「不在白名单里」的标签。Go 的 regexp 不支持前瞻，
	// 所以做不成参考项目那条正则——改为逐个匹配后按标签名判断（等价且更好读）。
	anyTag = regexp.MustCompile(`</?([A-Za-z0-9]+)[^>]*>`)
)

// cleanText：解 HTML 实体 + 按需去标签。raw 是 encoding/xml 已解过一层的 chardata。
//
// **顺序是先解实体、再去标签**，这一点是被测试逼出来的（我第一版写反了）：
// timedtext 里的内联标签是**双重转义**的——`<b>` 在文件里写作 `&amp;lt;b&amp;gt;`，
// XML 层解掉一层变成 `&lt;b&gt;`，HTML 层再解一层才露出 `<b>`。
// 先去标签的话，那时它还是 `&lt;b&gt;`，正则一个都匹配不上，
// 结果是 preserve_formatting 两档产出完全一样（而且都带着一堆 <b>）。
//
// 同理，语音里的 `&` 在文件里是 `&amp;amp;`，两层解完才是一个 `&`。
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

// timedTextXML：YouTube timedtext 接口的 XML 形状。
type timedTextXML struct {
	Texts []struct {
		Start float64 `xml:"start,attr"`
		Dur   float64 `xml:"dur,attr"`
		Body  string  `xml:",chardata"`
	} `xml:"text"`
}

// ParseTimedText 把 timedtext XML 解析成分句。
//
// 空串/空 <transcript/> 是**正常**结果（有轨但没内容，直播刚开始时常见），返回空切片不报错；
// 解不动才是错。
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
		// 全空的句子丢掉：timedtext 里存在只有换行的占位条目，留着会让 count 虚高、
		// 拼出来的全文多出一堆空格。
		if strings.TrimSpace(text) == "" {
			continue
		}
		out = append(out, schema.Snippet{Text: text, Start: t.Start, Duration: t.Dur})
	}
	return out, nil
}

// wsRun：连续空白（含换行）→ 一个空格。
var wsRun = regexp.MustCompile(`\s+`)

// JoinText 把分句拼成全文。
//
// 两件事：用空格拼、把**句内**的换行也压平。
// timedtext 的断行是按播放器一行显示得下多少字切的，与语义无关——真实样本里
// 一句就长这样："♪ A full commitment's\n        what I'm thinking of ♪"。
// 留着换行，下游 LLM 会把它当成段落边界。
//
// 只压 text 这一份，**snippets 保持原样**：那份是给做时间轴/字幕文件用的，
// 换行是原始数据的一部分，压掉就还原不回去了。
func JoinText(ss []schema.Snippet) string {
	parts := make([]string, 0, len(ss))
	for _, s := range ss {
		if t := strings.TrimSpace(wsRun.ReplaceAllString(s.Text, " ")); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, " ")
}

// track：一条字幕轨（内部形态，带取内容用的 URL）。
type track struct {
	Info schema.TrackInfo
	URL  string
}

// baseLang：语言代码的主子标签（`-` 之前那段），小写。en-US → en，zh-Hans → zh。
func baseLang(code string) string {
	if i := strings.IndexByte(code, '-'); i >= 0 {
		code = code[:i]
	}
	return strings.ToLower(code)
}

// pickTrack 按「语言优先级 × 字幕类型」选一条轨。
//
// 判据的顺序是**语言优先于类型**：用户写 `zh-Hans,en` 的意思是「中文比英文重要」，
// 那么有中文机翻时就不该因为「英文有人工字幕」而跳去英文。参考项目也是这个顺序。
//
// **同族即命中**（en 认 en-US / en-GB，反之亦然）。参考项目是精确的 dict 查找，
// 这一条我们刻意做得比它宽——实报（2026-08-26，用户第一次用就撞上）：
//
//	插件执行失败: 没有匹配的字幕（要的是 en）。这个视频现有：en-US(人工)
//
// 而 `en` 正是默认值：只要视频的轨带地区后缀，默认配置就必挂，而用户没做错任何事。
//
// 同族之内的排序：先看类型（prefer=any 时人工优先——那是这一档的全部意义），
// 类型相同再拿「代码完全一致」当胜负手（用户写 en 就是更想要 en，而不是 en-GB）。
// prefer=manual/generated 是硬过滤：整族都不满足就跳到下一个请求语言。
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
	// 越小越好：人工(0) 优于自动(2)；同类里精确码(-1) 优于同族(0)。
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
	// 报错要**把现有的列出来**：只说「没找到 zh-Hans」的话，用户完全不知道该改成什么。
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

// parseLanguages 把逗号分隔的优先级串切成列表；全空时回落 en。
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

// sortTracks 把人工字幕排到自动生成的前面（列清单时用）。
// 稳定：同类内保持 YouTube 给的顺序，那个顺序本身有含义（默认轨在前）。
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
