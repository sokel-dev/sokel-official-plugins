package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-official-plugins/zoom/schema"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var userPattern = regexp.MustCompile(`^[A-Za-z0-9_@.+-]+$`)
var meetingPattern = regexp.MustCompile(`^[0-9]{1,20}$`)

func userID(s string) (string, error) {
	if s == "" {
		return "me", nil
	}
	if len(s) > 254 || s == "." || strings.Contains(s, "..") || !userPattern.MatchString(s) {
		return "", errors.New("invalid user ID or email")
	}
	return url.PathEscape(s), nil
}
func meetingID(s string) error {
	if !meetingPattern.MatchString(s) {
		return errors.New("meeting ID must be a numeric meeting number; UUIDs are not supported")
	}
	return nil
}
func (a *api) health(ctx context.Context, in *HealthCheckIn) (*HealthCheckOut, error) {
	_, e := a.getUser(ctx, &GetUserIn{})
	if e != nil {
		return &HealthCheckOut{OK: false, Message: e.Error()}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Zoom user identity verified"}, nil
}
func (a *api) getUser(ctx context.Context, in *GetUserIn) (*GetUserOut, error) {
	id, e := userID(in.UserID)
	if e != nil {
		return nil, e
	}
	b, e := a.call(ctx, "GET", "/users/"+id, nil, nil, 200)
	if e != nil {
		return nil, e
	}
	var u schema.User
	if e = decode(b, &u); e != nil {
		return nil, e
	}
	if u.ID == "" {
		return nil, badResponse()
	}
	return &GetUserOut{User: u}, nil
}
func validateNextPageToken(prior, next string) error {
	if len(next) > 4096 || strings.ContainsAny(next, "\r\n") || (next != "" && next == prior) {
		return errors.New("API returned an invalid or repeated pagination token")
	}
	return nil
}
func (a *api) listUsers(ctx context.Context, in *ListUsersIn) (*ListUsersOut, error) {
	q, e := pageQuery(in.PageSize, 300, in.NextPageToken, "next_page_token")
	if e != nil {
		return nil, e
	}
	if in.Status != "" {
		switch in.Status {
		case "active", "inactive", "pending":
			q.Set("status", in.Status)
		default:
			return nil, errors.New("user status must be active, inactive or pending")
		}
	}
	b, e := a.call(ctx, "GET", "/users", q, nil, 200)
	if e != nil {
		return nil, e
	}
	var v struct {
		Users []schema.User `json:"users"`
		Next  string        `json:"next_page_token"`
	}
	if e = decode(b, &v); e != nil {
		return nil, e
	}
	if v.Users == nil {
		return nil, badResponse()
	}
	for _, u := range v.Users {
		if u.ID == "" {
			return nil, badResponse()
		}
	}
	if e = validateNextPageToken(in.NextPageToken, v.Next); e != nil {
		return nil, e
	}
	return &ListUsersOut{Users: v.Users, NextPageToken: v.Next, HasMore: v.Next != ""}, nil
}

// Zoom emits numeric meeting IDs. Decode the token as text before constructing typed output.
type wireMeeting struct {
	ID        json.Number `json:"id"`
	UUID      string      `json:"uuid"`
	HostID    string      `json:"host_id"`
	Topic     string      `json:"topic"`
	Agenda    string      `json:"agenda"`
	Type      int         `json:"type"`
	StartTime string      `json:"start_time"`
	Duration  int         `json:"duration"`
	Timezone  string      `json:"timezone"`
	JoinURL   string      `json:"join_url"`
	CreatedAt string      `json:"created_at"`
}

func (w wireMeeting) value() (schema.Meeting, error) {
	if meetingID(string(w.ID)) != nil {
		return schema.Meeting{}, badResponse()
	}
	return schema.Meeting{ID: string(w.ID), UUID: w.UUID, HostID: w.HostID, Topic: w.Topic, Agenda: w.Agenda, Type: w.Type, StartTime: w.StartTime, Duration: w.Duration, Timezone: w.Timezone, JoinURL: w.JoinURL, CreatedAt: w.CreatedAt}, nil
}
func (a *api) listMeetings(ctx context.Context, in *ListMeetingsIn) (*ListMeetingsOut, error) {
	id, e := userID(in.UserID)
	if e != nil {
		return nil, e
	}
	q, e := pageQuery(in.PageSize, 300, in.NextPageToken, "next_page_token")
	if e != nil {
		return nil, e
	}
	if in.Type != "" {
		switch in.Type {
		case "scheduled", "live", "upcoming", "upcoming_meetings", "previous_meetings":
			q.Set("type", in.Type)
		default:
			return nil, errors.New("invalid meeting list type")
		}
	}
	b, e := a.call(ctx, "GET", "/users/"+id+"/meetings", q, nil, 200)
	if e != nil {
		return nil, e
	}
	var v struct {
		Meetings []wireMeeting `json:"meetings"`
		Next     string        `json:"next_page_token"`
	}
	if e = decode(b, &v); e != nil {
		return nil, e
	}
	if v.Meetings == nil {
		return nil, badResponse()
	}
	if e = validateNextPageToken(in.NextPageToken, v.Next); e != nil {
		return nil, e
	}
	out := &ListMeetingsOut{Meetings: []schema.Meeting{}, NextPageToken: v.Next, HasMore: v.Next != ""}
	for _, w := range v.Meetings {
		m, e := w.value()
		if e != nil {
			return nil, e
		}
		out.Meetings = append(out.Meetings, m)
	}
	return out, nil
}
func meetingOutput(b []byte) (schema.Meeting, error) {
	var w wireMeeting
	if e := decode(b, &w); e != nil {
		return schema.Meeting{}, e
	}
	return w.value()
}
func (a *api) getMeeting(ctx context.Context, in *GetMeetingIn) (*GetMeetingOut, error) {
	if e := meetingID(in.MeetingID); e != nil {
		return nil, e
	}
	b, e := a.call(ctx, "GET", "/meetings/"+in.MeetingID, nil, nil, 200)
	if e != nil {
		return nil, e
	}
	v, e := meetingOutput(b)
	if e != nil {
		return nil, e
	}
	return &GetMeetingOut{Meeting: v}, nil
}
func normalizedStart(s string) (string, error) {
	v, e := time.Parse(time.RFC3339, s)
	if e != nil {
		return "", errors.New("start time must be RFC 3339 with an explicit UTC offset")
	}
	return v.UTC().Format(time.RFC3339), nil
}
func validateChanges(v *schema.MeetingChanges) error {
	if v.Topic != nil {
		if e := requiredText(*v.Topic, 200); e != nil {
			return e
		}
	}
	if v.Agenda != nil && len([]rune(*v.Agenda)) > 2000 {
		return errors.New("agenda must not exceed 2000 characters")
	}
	if v.Duration != nil && (*v.Duration < 1 || *v.Duration > 1440) {
		return errors.New("duration must be between 1 and 1440 minutes")
	}
	if v.StartTime != nil {
		s, e := normalizedStart(*v.StartTime)
		if e != nil {
			return e
		}
		v.StartTime = &s
	}
	return nil
}
func (a *api) createMeeting(ctx context.Context, in *CreateMeetingIn) (*CreateMeetingOut, error) {
	id, e := userID(in.UserID)
	if e != nil {
		return nil, e
	}
	details := schema.MeetingChanges{Topic: &in.Topic, StartTime: &in.StartTime, Duration: &in.Duration, Agenda: in.Details.Agenda, Settings: in.Details.Settings}
	if e = validateChanges(&details); e != nil {
		return nil, e
	}
	payload := struct {
		schema.MeetingChanges
		Type     int    `json:"type"`
		Timezone string `json:"timezone"`
	}{details, 2, "UTC"}
	b, e := a.call(ctx, "POST", "/users/"+id+"/meetings", nil, payload, 201)
	if e != nil {
		return nil, e
	}
	v, e := meetingOutput(b)
	if e != nil {
		return nil, e
	}
	return &CreateMeetingOut{Meeting: v}, nil
}
func (a *api) updateMeeting(ctx context.Context, in *UpdateMeetingIn) (*UpdateMeetingOut, error) {
	if e := meetingID(in.MeetingID); e != nil {
		return nil, e
	}
	v := in.Changes
	if v.StartTime != nil {
		start, e := time.Parse(time.RFC3339, *v.StartTime)
		if e != nil || !start.After(time.Now()) {
			return nil, errors.New("updated start time must be a future RFC 3339 timestamp")
		}
	}
	if e := validateChanges(&v); e != nil {
		return nil, e
	}
	b, _ := json.Marshal(v)
	if string(b) == "{}" || string(b) == `{"settings":{}}` {
		return nil, errors.New("provide at least one changed field")
	}
	_, e := a.call(ctx, "PATCH", "/meetings/"+in.MeetingID, nil, v, 204)
	if e != nil {
		return nil, e
	}
	return &UpdateMeetingOut{Updated: true}, nil
}
