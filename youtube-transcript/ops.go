package main

import (
	"fmt"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

// 三个操作的实现。取数细节在 youtube.go，纯逻辑在 parse.go——这里只负责
// 「把入参归一 → 调 → 把出参填齐」，刻意保持薄。

// clientFor：按凭证造 HTTP 门面。凭证是**可选**的——本插件不需要 YouTube 账号，
// 没配凭证时一切照跑（只是机房 IP 大概率会被风控挡下，那时才需要去配代理）。
func clientFor(ctx plugin.Ctx) (*client, error) {
	c := sokel.CredentialAs[Cred](ctx)
	return newClient(c.Proxy, c.UserAgent)
}

func opFetch(ctx plugin.Ctx, in *TranscriptFetchIn) (*TranscriptFetchOut, error) {
	videoID, err := ExtractVideoID(in.Video)
	if err != nil {
		return nil, err
	}
	cl, err := clientFor(ctx)
	if err != nil {
		return nil, err
	}
	tracks, err := cl.listTracks(ctx, videoID)
	if err != nil {
		return nil, err
	}
	prefer := in.Prefer
	if prefer == "" {
		prefer = "any"
	}
	picked, err := pickTrack(tracks, parseLanguages(in.Languages), prefer)
	if err != nil {
		return nil, err
	}
	snippets, err := cl.fetchTrack(ctx, picked, in.TranslateTo, in.PreserveFormatting)
	if err != nil {
		return nil, err
	}
	// 覆盖时长取最后一句的结束时刻。**不等于视频总长**（片尾常常没有字幕），
	// 出参说明里写清楚了，免得下游拿它当视频时长用。
	var dur float64
	if n := len(snippets); n > 0 {
		dur = snippets[n-1].Start + snippets[n-1].Duration
	}
	lang, code := picked.Info.Language, picked.Info.LanguageCode
	if in.TranslateTo != "" {
		// 翻译后语言就是目标语言了——照抄原轨的语言会让下游以为拿到的是中文原文。
		code = in.TranslateTo
		lang = fmt.Sprintf("%s（由 %s 机器翻译）", in.TranslateTo, picked.Info.LanguageCode)
	}
	return &TranscriptFetchOut{
		VideoID:      videoID,
		Text:         JoinText(snippets),
		Snippets:     snippets,
		Count:        len(snippets),
		Language:     lang,
		LanguageCode: code,
		IsGenerated:  picked.Info.IsGenerated,
		Translated:   in.TranslateTo != "",
		DurationSec:  dur,
	}, nil
}

func opList(ctx plugin.Ctx, in *TranscriptListIn) (*TranscriptListOut, error) {
	videoID, err := ExtractVideoID(in.Video)
	if err != nil {
		return nil, err
	}
	cl, err := clientFor(ctx)
	if err != nil {
		return nil, err
	}
	tracks, err := cl.listTracks(ctx, videoID)
	if err != nil {
		return nil, err
	}
	infos := sortTracks(tracks)
	hasManual := false
	for _, t := range infos {
		if !t.IsGenerated {
			hasManual = true
			break
		}
	}
	return &TranscriptListOut{
		VideoID:     videoID,
		Transcripts: infos,
		Count:       len(infos),
		HasManual:   hasManual,
	}, nil
}

// opHealthCheck：本插件不需要账号，所以体检验的是**出站网络通不通、有没有被风控**。
// 按平台约定，不可用返回 ok=false 而不是 error（见 dev-playbook §4）。
func opHealthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	cl, err := clientFor(ctx)
	if err != nil {
		return &HealthCheckOut{OK: false, Message: err.Error()}, nil
	}
	tracks, err := cl.listTracks(ctx, healthVideoID)
	if err != nil {
		return &HealthCheckOut{OK: false, Message: err.Error()}, nil
	}
	return &HealthCheckOut{
		OK:      true,
		Message: fmt.Sprintf("出站正常，未被风控；试取的公开视频有 %d 条字幕轨", len(tracks)),
	}, nil
}
