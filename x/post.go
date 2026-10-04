package main

// 发布：发推、推串、删推。

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

// createResp：POST /2/tweets 的应答。
type createResp struct {
	Data struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	} `json:"data"`
}

// postBody：一次发推的请求体。抽出来是因为推串要连着用它 N 次。
type postBody struct {
	Text          string
	ReplyToID     string
	QuoteID       string
	MediaIDs      []string
	PollOptions   []string
	PollMinutes   int
	ReplySettings string
}

func (b postBody) build() (map[string]any, error) {
	body := map[string]any{}
	if t := strings.TrimSpace(b.Text); t != "" {
		body["text"] = t
	}
	if id := strings.TrimSpace(b.ReplyToID); id != "" {
		body["reply"] = map[string]any{"in_reply_to_tweet_id": id}
	}
	if id := strings.TrimSpace(b.QuoteID); id != "" {
		body["quote_tweet_id"] = id
	}
	media := nonEmpty(b.MediaIDs)
	polls := nonEmpty(b.PollOptions)
	if len(media) > 0 && len(polls) > 0 {
		return nil, fmt.Errorf("投票和媒体不能同时带（X 的限制）")
	}
	if len(media) > 4 {
		return nil, fmt.Errorf("一条推文最多 4 个媒体，给了 %d 个", len(media))
	}
	if len(media) > 0 {
		body["media"] = map[string]any{"media_ids": media}
	}
	if len(polls) > 0 {
		if len(polls) < 2 || len(polls) > 4 {
			return nil, fmt.Errorf("投票要 2-4 个选项，给了 %d 个", len(polls))
		}
		mins := b.PollMinutes
		if mins <= 0 {
			mins = 1440
		}
		body["poll"] = map[string]any{"options": polls, "duration_minutes": mins}
	}
	// reply_settings 只在非 everyone 时带：X 对这个字段的默认值就是「所有人」，
	// 显式传 everyone 反而会被某些档位拒掉。
	if rs := strings.TrimSpace(b.ReplySettings); rs != "" && rs != "everyone" {
		body["reply_settings"] = rs
	}
	if body["text"] == nil && len(media) == 0 && len(polls) == 0 {
		return nil, fmt.Errorf("正文、媒体、投票至少要有一样")
	}
	return body, nil
}

func opPostCreate(ctx plugin.Ctx, in *XPostCreateIn) (*XPostCreateOut, error) {
	body, err := postBody{
		Text: in.Text, ReplyToID: in.ReplyToID, QuoteID: in.QuoteID,
		MediaIDs: in.MediaIDs, PollOptions: in.PollOptions,
		PollMinutes: in.PollDurationMinutes, ReplySettings: in.ReplySettings,
	}.build()
	if err != nil {
		return nil, err
	}
	var resp createResp
	if err := callAPI(ctx, reqOpts{method: http.MethodPost, path: "/tweets", body: body}, &resp); err != nil {
		return nil, err
	}
	return &XPostCreateOut{
		ID: resp.Data.ID, Text: resp.Data.Text, URL: postURL(meUsername(ctx), resp.Data.ID),
	}, nil
}

func opPostThread(ctx plugin.Ctx, in *XPostThreadIn) (*XPostThreadOut, error) {
	texts := nonEmpty(in.Texts)
	if len(texts) == 0 {
		return nil, fmt.Errorf("一条正文都没有")
	}
	gap := time.Duration(in.IntervalMs) * time.Millisecond
	if in.IntervalMs <= 0 {
		gap = time.Second
	}

	ids := make([]string, 0, len(texts))
	prev := strings.TrimSpace(in.ReplyToID)
	for i, t := range texts {
		b := postBody{Text: t, ReplyToID: prev, ReplySettings: "everyone"}
		if i == 0 {
			b.MediaIDs = in.MediaIDs
		}
		body, err := b.build()
		if err != nil {
			return nil, fmt.Errorf("第 %d 条: %w", i+1, err)
		}
		var resp createResp
		if err := callAPI(ctx, reqOpts{method: http.MethodPost, path: "/tweets", body: body}, &resp); err != nil {
			// 中途失败**不回滚**：前面几条已经在公开时间线上了，删掉反而是二次破坏。
			// 把已发出的 id 带在错误里，人能接着串或手工收尾。
			return nil, fmt.Errorf("推串发到第 %d 条失败（前 %d 条已发出：%s）: %w",
				i+1, len(ids), strings.Join(ids, ","), err)
		}
		ids = append(ids, resp.Data.ID)
		prev = resp.Data.ID
		// 最后一条发完不必再等
		if i < len(texts)-1 && gap > 0 {
			t := time.NewTimer(gap)
			select {
			case <-t.C:
			case <-ctx.Done():
				t.Stop()
				return nil, fmt.Errorf("推串被中断（已发出 %d 条：%s）", len(ids), strings.Join(ids, ","))
			}
		}
	}
	return &XPostThreadOut{
		IDs: ids, RootID: ids[0], RootURL: postURL(meUsername(ctx), ids[0]), Count: len(ids),
	}, nil
}

func opPostDelete(ctx plugin.Ctx, in *XPostDeleteIn) (*XPostDeleteOut, error) {
	id := strings.TrimSpace(in.PostID)
	if id == "" {
		return nil, fmt.Errorf("要删哪条？post_id 是空的")
	}
	var resp struct {
		Data struct {
			Deleted bool `json:"deleted"`
		} `json:"data"`
	}
	if err := callAPI(ctx, reqOpts{method: http.MethodDelete, path: "/tweets/" + id}, &resp); err != nil {
		return nil, err
	}
	return &XPostDeleteOut{Deleted: resp.Data.Deleted}, nil
}

func nonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
