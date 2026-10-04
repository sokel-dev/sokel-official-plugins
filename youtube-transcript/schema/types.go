package schema

// Snippet is one transcript line. start/duration are in seconds (with fractional part), matching
// YouTube's original timedtext.
//
// Deliberately **not just merged into a single plain-text blob**: when downstream needs to "jump to a
// given minute:second" or "slice by a time window to feed an LLM", having no timeline would mean fetching
// it all over again. The full text is available separately as the text output, so both coexist.
type Snippet struct {
	Text     string  `sokel:"text" label:"文本"`
	Start    float64 `sokel:"start" label:"开始（秒）"`
	Duration float64 `sokel:"duration" label:"时长（秒）"`
}

// TrackInfo describes one **available** transcript track (content not yet fetched).
//
// The point of "listing available transcripts": the same video often only has one language the author
// actually uploaded plus a pile of machine translations, and machine translation quality is noticeably
// worse. Listing them first lets the canvas branch on is_generated.
type TrackInfo struct {
	Language             string   `sokel:"language" label:"语言名" desc:"YouTube 给的显示名，如「English (auto-generated)」"`
	LanguageCode         string   `sokel:"language_code" label:"语言代码" desc:"如 en / zh-Hans / ja"`
	IsGenerated          bool     `sokel:"is_generated" label:"是机器生成的" desc:"true = ASR 自动识别；false = 人工上传/校对过"`
	IsTranslatable       bool     `sokel:"is_translatable" label:"可被翻译" desc:"为真时可用「翻译成」把它转成下面任一语言"`
	TranslationLanguages []string `sokel:"translation_languages,optional" label:"可翻译成" desc:"语言代码列表"`
}
