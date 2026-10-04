package schema

// Snippet：一条字幕。start/duration 是秒（带小数），与 YouTube 原始 timedtext 一致。
//
// 刻意**不合并成一段纯文本就完事**：下游要做「跳到第几分几秒」「按时间窗切片喂给 LLM」
// 时，没有时间轴就只能重新去要一次。全文另有 text 出参，两者并存。
type Snippet struct {
	Text     string  `sokel:"text" label:"文本"`
	Start    float64 `sokel:"start" label:"开始（秒）"`
	Duration float64 `sokel:"duration" label:"时长（秒）"`
}

// TrackInfo：一条**可用**的字幕轨（还没取内容）。
//
// 「列出可用字幕」的意义在于：同一个视频常常只有作者上传的某一种语言 + 一堆机器翻译，
// 而机翻质量差很多。先列一遍，就能在画布上按 is_generated 分流。
type TrackInfo struct {
	Language             string   `sokel:"language" label:"语言名" desc:"YouTube 给的显示名，如「English (auto-generated)」"`
	LanguageCode         string   `sokel:"language_code" label:"语言代码" desc:"如 en / zh-Hans / ja"`
	IsGenerated          bool     `sokel:"is_generated" label:"是机器生成的" desc:"true = ASR 自动识别；false = 人工上传/校对过"`
	IsTranslatable       bool     `sokel:"is_translatable" label:"可被翻译" desc:"为真时可用「翻译成」把它转成下面任一语言"`
	TranslationLanguages []string `sokel:"translation_languages,optional" label:"可翻译成" desc:"语言代码列表"`
}
