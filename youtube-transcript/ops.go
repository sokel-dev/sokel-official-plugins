package main

import (
	"fmt"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

// Implementation of the three operations. Fetching details live in youtube.go, pure logic in
// parse.go — this file is deliberately kept thin, responsible only for "normalize inputs → call → fill
// outputs".

// clientFor builds the HTTP facade from the credential. The credential is **optional** — this plugin
// doesn't need a YouTube account, so everything works fine with no credential configured (it's just that
// a datacenter IP will most likely get blocked by anti-bot measures, which is when configuring a proxy
// becomes necessary).
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
	// Coverage duration is taken from the end time of the last line. **This is not the video's total
	// length** (the closing credits often have no captions), and the output field's description says
	// so, to keep downstream nodes from mistaking it for the video duration.
	var dur float64
	if n := len(snippets); n > 0 {
		dur = snippets[n-1].Start + snippets[n-1].Duration
	}
	lang, code := picked.Info.Language, picked.Info.LanguageCode
	if in.TranslateTo != "" {
		// Once translated, the language is the target language — copying the original track's
		// language code would mislead downstream into thinking it got the untranslated original.
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

// opHealthCheck: this plugin needs no account, so the check verifies **whether outbound network access
// works and whether it's being blocked by anti-bot measures**. Per platform convention, unavailability
// is returned as ok=false rather than an error (see dev-playbook §4).
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
