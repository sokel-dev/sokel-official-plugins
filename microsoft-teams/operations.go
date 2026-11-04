package main

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

func teamPath(id string) (string, error) {
	v, e := segment(id, "team_id")
	if e != nil {
		return "", e
	}
	return "/v1.0/teams/" + v, nil
}
func channelPath(team, channel string) (string, error) {
	p, e := teamPath(team)
	if e != nil {
		return "", e
	}
	c, e := segment(channel, "channel_id")
	if e != nil {
		return "", e
	}
	return p + "/channels/" + c, nil
}
func chatPath(id string) (string, error) {
	v, e := segment(id, "chat_id")
	if e != nil {
		return "", e
	}
	return "/v1.0/chats/" + v, nil
}
func (a *api) list_teams(ctx context.Context, in *ListTeamsIn) (*ListTeamsOut, error) {
	base, e := a.delegated(ctx)
	if e != nil {
		return nil, e
	}
	var values []struct{ ID, DisplayName, Description string }
	next, e := a.page(ctx, base+"/joinedTeams", nil, in.NextLink, &values)
	if e != nil {
		return nil, e
	}
	out := &ListTeamsOut{Teams: []Team{}, NextLink: next}
	for _, v := range values {
		if v.ID == "" {
			return nil, fmt.Errorf("Microsoft Graph returned a team without its ID")
		}
		out.Teams = append(out.Teams, Team{ID: v.ID, Name: v.DisplayName, Description: v.Description})
	}
	return out, nil
}
func (a *api) list_channels(ctx context.Context, in *ListChannelsIn) (*ListChannelsOut, error) {
	p, e := teamPath(in.TeamID)
	if e != nil {
		return nil, e
	}
	if _, e = a.delegated(ctx); e != nil {
		return nil, e
	}
	var values []struct{ ID, DisplayName, Description, MembershipType, WebURL string }
	next, e := a.page(ctx, p+"/channels", url.Values{"$select": {"id,displayName,description,membershipType,webUrl"}}, in.NextLink, &values)
	if e != nil {
		return nil, e
	}
	out := &ListChannelsOut{Channels: []Channel{}, NextLink: next}
	for _, v := range values {
		if v.ID == "" {
			return nil, fmt.Errorf("Microsoft Graph returned a channel without its ID")
		}
		out.Channels = append(out.Channels, Channel{ID: v.ID, Name: v.DisplayName, Description: v.Description, MembershipType: v.MembershipType, WebURL: v.WebURL})
	}
	return out, nil
}
func (a *api) list_chats(ctx context.Context, in *ListChatsIn) (*ListChatsOut, error) {
	n, e := pageSize(in.Limit)
	if e != nil {
		return nil, e
	}
	base, e := a.delegated(ctx)
	if e != nil {
		return nil, e
	}
	var values []struct{ ID, Topic, ChatType, WebURL, CreatedDateTime, LastUpdatedDateTime string }
	next, e := a.page(ctx, base+"/chats", url.Values{"$top": {strconv.Itoa(n)}}, in.NextLink, &values)
	if e != nil {
		return nil, e
	}
	out := &ListChatsOut{Chats: []Chat{}, NextLink: next}
	for _, v := range values {
		if v.ID == "" {
			return nil, fmt.Errorf("Microsoft Graph returned a chat without its ID")
		}
		out.Chats = append(out.Chats, Chat{ID: v.ID, Topic: v.Topic, ChatType: v.ChatType, WebURL: v.WebURL, CreatedAt: v.CreatedDateTime, UpdatedAt: v.LastUpdatedDateTime})
	}
	return out, nil
}

type rawMessage struct {
	ID, CreatedDateTime, LastModifiedDateTime, DeletedDateTime, ReplyToID, WebURL, MessageType string
	Body                                                                                       struct{ Content, ContentType string }
	From                                                                                       struct {
		User, Application *struct{ ID, DisplayName string }
	}
}

func (r rawMessage) typed() TeamsMessage {
	out := TeamsMessage{ID: r.ID, Body: r.Body.Content, BodyType: r.Body.ContentType, CreatedAt: r.CreatedDateTime, UpdatedAt: r.LastModifiedDateTime, DeletedAt: r.DeletedDateTime, ReplyToID: r.ReplyToID, WebURL: r.WebURL, MessageType: r.MessageType}
	author := r.From.User
	if author == nil {
		author = r.From.Application
	}
	if author != nil {
		out.AuthorID = author.ID
		out.AuthorName = author.DisplayName
	}
	return out
}
func (a *api) messages(ctx context.Context, path, next string, limit int) ([]TeamsMessage, string, error) {
	n, e := pageSize(limit)
	if e != nil {
		return nil, "", e
	}
	if _, e = a.delegated(ctx); e != nil {
		return nil, "", e
	}
	var values []rawMessage
	next, e = a.page(ctx, path, url.Values{"$top": {strconv.Itoa(n)}}, next, &values)
	if e != nil {
		return nil, "", e
	}
	out := []TeamsMessage{}
	for _, v := range values {
		if v.ID == "" {
			return nil, "", fmt.Errorf("Microsoft Graph returned a message without its ID")
		}
		out = append(out, v.typed())
	}
	return out, next, nil
}
func (a *api) list_channel_messages(ctx context.Context, in *ListChannelMessagesIn) (*ListChannelMessagesOut, error) {
	p, e := channelPath(in.TeamID, in.ChannelID)
	if e != nil {
		return nil, e
	}
	m, next, e := a.messages(ctx, p+"/messages", in.NextLink, in.Limit)
	if e != nil {
		return nil, e
	}
	return &ListChannelMessagesOut{Messages: m, NextLink: next}, nil
}
func (a *api) list_replies(ctx context.Context, in *ListRepliesIn) (*ListRepliesOut, error) {
	p, e := channelPath(in.TeamID, in.ChannelID)
	if e != nil {
		return nil, e
	}
	id, e := segment(in.MessageID, "message_id")
	if e != nil {
		return nil, e
	}
	m, next, e := a.messages(ctx, p+"/messages/"+id+"/replies", in.NextLink, in.Limit)
	if e != nil {
		return nil, e
	}
	return &ListRepliesOut{Messages: m, NextLink: next}, nil
}
func (a *api) list_chat_messages(ctx context.Context, in *ListChatMessagesIn) (*ListChatMessagesOut, error) {
	p, e := chatPath(in.ChatID)
	if e != nil {
		return nil, e
	}
	m, next, e := a.messages(ctx, p+"/messages", in.NextLink, in.Limit)
	if e != nil {
		return nil, e
	}
	return &ListChatMessagesOut{Messages: m, NextLink: next}, nil
}
func (a *api) postMessage(ctx context.Context, path, content, kind string) (TeamsMessage, error) {
	typ, e := contentType(kind)
	if e != nil {
		return TeamsMessage{}, e
	}
	if strings.TrimSpace(content) == "" || len(content) > 64<<10 {
		return TeamsMessage{}, fmt.Errorf("message content must be nonempty and at most 64 KiB")
	}
	if _, e = a.delegated(ctx); e != nil {
		return TeamsMessage{}, e
	}
	var r rawMessage
	if e = a.json(ctx, "POST", path, nil, map[string]any{"body": map[string]string{"content": content, "contentType": typ}}, &r, 201); e != nil {
		return TeamsMessage{}, e
	}
	if r.ID == "" {
		return TeamsMessage{}, fmt.Errorf("Microsoft Graph did not confirm message creation; inspect Teams before retrying")
	}
	return r.typed(), nil
}
func (a *api) send_channel_message(ctx context.Context, in *SendChannelMessageIn) (*SendChannelMessageOut, error) {
	p, e := channelPath(in.TeamID, in.ChannelID)
	if e != nil {
		return nil, e
	}
	p += "/messages"
	if in.ReplyToID != "" {
		id, e := segment(in.ReplyToID, "reply_to_id")
		if e != nil {
			return nil, e
		}
		p += "/" + id + "/replies"
	}
	m, e := a.postMessage(ctx, p, in.Content, in.ContentType)
	if e != nil {
		return nil, e
	}
	return &SendChannelMessageOut{Message: m}, nil
}
func (a *api) send_chat_message(ctx context.Context, in *SendChatMessageIn) (*SendChatMessageOut, error) {
	p, e := chatPath(in.ChatID)
	if e != nil {
		return nil, e
	}
	m, e := a.postMessage(ctx, p+"/messages", in.Content, in.ContentType)
	if e != nil {
		return nil, e
	}
	return &SendChatMessageOut{Message: m}, nil
}
