package main

// 私信：发一条、拉一批。
//
// 发信有两个入口（按人 / 按会话），对应 X 的两个端点。**给会话 id 时必须走会话那个**：
// 拿会话 id 当 participant_id 发过去，X 会去建一个与「那个 id 代表的用户」的新会话，
// 而那个用户根本不存在——错误是 404，看不出错在哪。

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/sokel-dev/sokel-official-plugins/x/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

func opDMSend(ctx plugin.Ctx, in *XDmSendIn) (*XDmSendOut, error) {
	text := strings.TrimSpace(in.Text)
	media := nonEmpty(in.MediaIDs)
	if text == "" && len(media) == 0 {
		return nil, fmt.Errorf("正文与媒体至少要有一样")
	}
	body := map[string]any{}
	if text != "" {
		body["text"] = text
	}
	if len(media) > 0 {
		atts := make([]map[string]any, 0, len(media))
		for _, id := range media {
			atts = append(atts, map[string]any{"media_id": id})
		}
		body["attachments"] = atts
	}

	var path string
	switch conv, uid := strings.TrimSpace(in.ConversationID), strings.TrimSpace(in.UserID); {
	case conv != "":
		path = "/dm_conversations/" + conv + "/messages"
	case uid != "":
		path = "/dm_conversations/with/" + uid + "/messages"
	default:
		return nil, fmt.Errorf("收信人 id 与会话 id 至少要给一个")
	}

	var resp struct {
		Data struct {
			ConversationID string `json:"dm_conversation_id"`
			EventID        string `json:"dm_event_id"`
		} `json:"data"`
	}
	if err := callAPI(ctx, reqOpts{method: http.MethodPost, path: path, body: body}, &resp); err != nil {
		return nil, err
	}
	return &XDmSendOut{ConversationID: resp.Data.ConversationID, EventID: resp.Data.EventID}, nil
}

func opDMEvents(ctx plugin.Ctx, in *XDmEventsIn) (*XDmEventsOut, error) {
	max := in.MaxItems
	if max <= 0 {
		max = 50
	}
	path := "/dm_events"
	if c := strings.TrimSpace(in.ConversationID); c != "" {
		path = "/dm_conversations/" + c + "/dm_events"
	}
	q := url.Values{
		"dm_event.fields": {"id,text,event_type,created_at,dm_conversation_id,sender_id"},
		"max_results":     {strconv.Itoa(min(max, maxPageSize))},
	}
	var resp struct {
		Data []struct {
			ID             string `json:"id"`
			EventType      string `json:"event_type"`
			Text           string `json:"text"`
			ConversationID string `json:"dm_conversation_id"`
			SenderID       string `json:"sender_id"`
			CreatedAt      string `json:"created_at"`
		} `json:"data"`
		Meta struct {
			NextToken string `json:"next_token"`
		} `json:"meta"`
	}
	if err := callAPI(ctx, reqOpts{method: http.MethodGet, path: path, query: q}, &resp); err != nil {
		return nil, err
	}

	// X 的私信没有 since_id，只能拉回来自己按游标裁。**裁在插件里**，
	// 否则每一条流程都要自己写一遍「这条我处理过没有」。
	cursor := strings.TrimSpace(in.Cursor)
	items := make([]schema.DMEvent, 0, len(resp.Data))
	newest := cursor
	for _, e := range resp.Data {
		if cursor != "" && !idLess(cursor, e.ID) {
			continue
		}
		items = append(items, schema.DMEvent{
			ID: e.ID, Kind: e.EventType, ConversationID: e.ConversationID,
			SenderID: e.SenderID, Text: e.Text, CreatedAt: e.CreatedAt,
		})
		if idLess(newest, e.ID) || newest == "" {
			newest = e.ID
		}
	}
	sort.Slice(items, func(i, j int) bool { return idLess(items[i].ID, items[j].ID) })
	return &XDmEventsOut{
		Items: items, NextCursor: newest,
		HasMore: resp.Meta.NextToken != "" && len(items) == len(resp.Data), Count: len(items),
	}, nil
}
