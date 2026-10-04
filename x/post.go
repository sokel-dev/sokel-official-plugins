package main

// Publishing: post, thread, delete.

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

// createResp is the response from POST /2/tweets.
type createResp struct {
	Data struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	} `json:"data"`
}

// postBody is the request body for a single post. Pulled out as its own type because a thread
// uses it N times in a row.
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
	// reply_settings is only included when it's not everyone: X's own default for this field is
	// already "everyone", and explicitly sending everyone gets rejected on some tiers.
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
			// A mid-thread failure **doesn't roll back**: the earlier tweets are already public,
			// and deleting them would just be a second act of damage. Carry the ids already
			// posted in the error so a person can continue the thread or wrap it up by hand.
			return nil, fmt.Errorf("推串发到第 %d 条失败（前 %d 条已发出：%s）: %w",
				i+1, len(ids), strings.Join(ids, ","), err)
		}
		ids = append(ids, resp.Data.ID)
		prev = resp.Data.ID
		// No need to wait after the last one is posted
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
