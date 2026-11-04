package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"strconv"
	"strings"
	"time"
)

func webhook(ctx sokel.WebhookCtx, req *sokel.WebhookRequest) sokel.WebhookResponse {
	reply := func(status int) sokel.WebhookResponse {
		return sokel.WebhookResponse{Status: status, Headers: map[string]string{"Content-Type": "application/json"}, Body: []byte(`{}`)}
	}
	if req.Method != "POST" {
		return reply(405)
	}
	cred := sokel.CredentialAs[Cred](ctx)
	if cred.AppSecret == "" || cred.RobotCode == "" {
		return reply(503)
	}
	ts := req.Header("timestamp")
	millis, err := strconv.ParseInt(ts, 10, 64)
	now := time.Now().UnixMilli()
	if err != nil || millis < now-3600000 || millis > now+3600000 {
		return reply(401)
	}
	mac := hmac.New(sha256.New, []byte(cred.AppSecret))
	mac.Write([]byte(ts + "\n" + cred.AppSecret))
	signed, err := base64.StdEncoding.DecodeString(req.Header("sign"))
	if err != nil || !hmac.Equal(signed, mac.Sum(nil)) {
		return reply(401)
	}
	raw := req.Body
	if len(raw) > 1<<20 {
		return reply(413)
	}
	if len(raw) == 0 && req.BodyB64 != "" {
		if len(req.BodyB64) > 2<<20 {
			return reply(413)
		}
		raw, err = base64.StdEncoding.DecodeString(req.BodyB64)
		if err != nil {
			return reply(400)
		}
	}
	if len(raw) > 1<<20 {
		return reply(413)
	}
	var body struct {
		MsgID            string `json:"msgId"`
		ConversationID   string `json:"conversationId"`
		ConversationType string `json:"conversationType"`
		SenderID         string `json:"senderId"`
		SenderStaffID    string `json:"senderStaffId"`
		SenderNick       string `json:"senderNick"`
		Type             string `json:"msgtype"`
		RobotCode        string `json:"robotCode"`
		CreatedAt        int    `json:"createAt"`
		Text             struct {
			Content string `json:"content"`
		} `json:"text"`
		Content struct {
			DownloadCode string `json:"downloadCode"`
			Recognition  string `json:"recognition"`
			RichText     []struct {
				Text         string `json:"text"`
				DownloadCode string `json:"downloadCode"`
			} `json:"richText"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &body) != nil || body.MsgID == "" || body.ConversationID == "" || body.Type == "" {
		return reply(400)
	}
	if body.RobotCode != cred.RobotCode {
		return reply(403)
	}
	event := &MessageEvent{MessageID: body.MsgID, ConversationID: body.ConversationID, ConversationType: body.ConversationType, SenderID: body.SenderID, SenderUserID: body.SenderStaffID, SenderName: body.SenderNick, MessageType: body.Type, Text: body.Text.Content, CreatedAt: body.CreatedAt, DownloadCodes: []string{}}
	if body.Content.DownloadCode != "" {
		event.DownloadCodes = append(event.DownloadCodes, body.Content.DownloadCode)
	}
	if body.Type == "audio" {
		event.Text = body.Content.Recognition
	}
	if body.Type == "richText" {
		var text strings.Builder
		for _, piece := range body.Content.RichText {
			text.WriteString(piece.Text)
			if piece.DownloadCode != "" {
				event.DownloadCodes = append(event.DownloadCodes, piece.DownloadCode)
			}
		}
		event.Text = text.String()
	}
	if err := TriggerMessage(ctx, body.MsgID, event); err != nil {
		return reply(503)
	}
	return reply(200)
}
