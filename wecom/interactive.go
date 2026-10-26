package main

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

var actionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func interactionURL(record Alert) string {
	return record.PortalURL + "?interaction=" + url.QueryEscape(record.ID)
}

func previewDescription(button, content string) string {
	var b strings.Builder
	count := 0
	for _, r := range content {
		escaped := html.EscapeString(string(r))
		n := utf8.RuneCountInString(escaped)
		if count+n > 420 {
			b.WriteString("…")
			break
		}
		b.WriteString(escaped)
		count += n
	}
	b.WriteString("\n点击“" + html.EscapeString(button) + "”查看完整内容。")
	return b.String()
}

func validateMessageButtons(input []MessageButton, context map[string]any) ([]InteractiveButton, error) {
	if len(input) < 1 || len(input) > 8 {
		return nil, fmt.Errorf("请配置 1 至 8 个交互按钮")
	}
	buttons := make([]InteractiveButton, 0, len(input))
	seen := map[string]bool{}
	values := make([]map[string]any, 0, len(input))
	for _, button := range input {
		if !actionIDPattern.MatchString(button.ID) || seen[button.ID] {
			return nil, fmt.Errorf("按钮动作标识须唯一，且为 1 至 64 位字母、数字、下划线或连字符")
		}
		label := strings.TrimSpace(button.Text)
		if label == "" || !utf8.ValidString(label) || utf8.RuneCountInString(label) > 40 {
			return nil, fmt.Errorf("按钮文字应为 1 至 40 个字符")
		}
		seen[button.ID] = true
		buttons = append(buttons, InteractiveButton{ID: button.ID, Text: label, Value: button.Value})
		values = append(values, button.Value)
	}
	encoded, err := json.Marshal(struct {
		Context map[string]any
		Values  []map[string]any
	}{context, values})
	if err != nil || len(encoded) > 16384 {
		return nil, fmt.Errorf("业务上下文与按钮回传值须为有效 JSON 对象，合计不超过 16 KiB")
	}
	return buttons, nil
}

func (a *application) sendInteractiveMessage(ctx plugin.Ctx, in *SendInteractiveMessageIn) (*SendInteractiveMessageOut, error) {
	c, ns, err := credential(ctx)
	if err != nil {
		return nil, err
	}
	users, err := recipients(in.Users)
	if err != nil {
		return nil, err
	}
	portal, err := portalURL(in.PortalURL)
	if err != nil {
		return nil, err
	}
	title, requestID := strings.TrimSpace(in.Title), strings.TrimSpace(in.RequestID)
	if title == "" || !utf8.ValidString(title) || utf8.RuneCountInString(title) > 128 {
		return nil, fmt.Errorf("消息标题应为 1 至 128 个字符")
	}
	if !utf8.ValidString(requestID) || len(requestID) > 256 {
		return nil, fmt.Errorf("幂等键不能超过 256 字节")
	}
	if strings.TrimSpace(in.Content) == "" || !utf8.ValidString(in.Content) || len(in.Content) > 16384 {
		return nil, fmt.Errorf("消息正文应为 1 至 16384 字节")
	}
	buttons, err := validateMessageButtons(in.Buttons, in.Context)
	if err != nil {
		return nil, err
	}
	button := strings.TrimSpace(in.Button)
	if button == "" {
		button = "查看消息"
	}
	if !utf8.ValidString(button) || utf8.RuneCountInString(button) > 4 {
		return nil, fmt.Errorf("链接入口文字应为 1 至 4 个字符")
	}
	hours := in.ExpiresHours
	if hours == 0 {
		hours = 72
	}
	if hours < 1 || hours > 720 {
		return nil, fmt.Errorf("交互有效期须在 1 至 720 小时之间")
	}
	if err := a.api.VerifyApplication(ctx, c); err != nil {
		return nil, err
	}
	id, err := randomID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	record := Alert{ID: id, ExternalID: requestID, Title: title, Description: in.Content, Buttons: buttons, Context: in.Context, ButtonText: button, Users: users, PortalURL: portal, CreatedAt: now.Format(time.RFC3339), UpdatedAt: now.Format(time.RFC3339), ExpiresAt: now.Add(time.Duration(hours) * time.Hour).Format(time.RFC3339)}
	a.sendMu.Lock()
	defer a.sendMu.Unlock()
	record, reused, err := a.store.CreateAlert(ns, record)
	if err != nil {
		return nil, err
	}
	if !record.Sent {
		if expired(record.ExpiresAt, time.Now()) {
			return nil, fmt.Errorf("该交互消息已过期，请使用新的幂等键")
		}
		d, err := a.api.SendCard(ctx, c, record.Users, record.Title, previewDescription(record.ButtonText, record.Description), interactionURL(record), record.ButtonText)
		if err != nil {
			return nil, err
		}
		if err := a.store.SetDelivery(ns, record.ID, d); err != nil {
			return nil, fmt.Errorf("企业微信已接受消息，但保存发送结果失败；请检查持久卷，直接重试可能重复发送: %w", err)
		}
		record.Sent, record.Delivery = true, d
	}
	d := record.Delivery
	return &SendInteractiveMessageOut{InteractionID: record.ID, InteractionURL: interactionURL(record), Reused: reused, MsgID: d.MsgID, InvalidUser: d.InvalidUser, UnlicensedUser: d.UnlicensedUser, AllAccepted: accepted(d)}, nil
}

func messageButtons(buttons []InteractiveButton) []MessageButton {
	out := make([]MessageButton, 0, len(buttons))
	for _, b := range buttons {
		out = append(out, MessageButton{ID: b.ID, Text: b.Text, Value: b.Value})
	}
	return out
}

func (a *application) getInteractiveMessage(ctx plugin.Ctx, in *GetInteractiveMessageIn) (*GetInteractiveMessageOut, error) {
	c, ns, err := credential(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.api.VerifyApplication(ctx, c); err != nil {
		return nil, err
	}
	r, err := a.store.GetAlert(ns, in.InteractionID)
	if err != nil {
		return nil, err
	}
	return &GetInteractiveMessageOut{InteractionID: r.ID, RequestID: r.ExternalID, Title: r.Title, Content: r.Description, Buttons: messageButtons(r.Buttons), Context: r.Context, LastActionID: r.LastActionID, ActorUserID: r.ActorUserID, Revision: r.Revision, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, ExpiresAt: r.ExpiresAt, Sent: r.Sent, MsgID: r.Delivery.MsgID}, nil
}
