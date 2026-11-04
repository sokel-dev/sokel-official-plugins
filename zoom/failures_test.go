package main

import (
	"context"
	"github.com/sokel-dev/sokel-official-plugins/zoom/schema"
	"strings"
	"testing"
)

func TestValidationBeforeNetwork(t *testing.T) {
	a, n := fakeAPI(t, "GET", "unused", 200, "{}", nil)
	ctx := context.Background()
	for _, s := range []string{"../bad", "https://evil.invalid", "%2F", "."} {
		if _, e := a.getUser(ctx, &GetUserIn{UserID: s}); e == nil {
			t.Fatal("user accepted")
		}
	}
	for _, s := range []string{"", "abc", "../123"} {
		if _, e := a.getMeeting(ctx, &GetMeetingIn{MeetingID: s}); e == nil {
			t.Fatal("meeting accepted")
		}
	}
	for _, in := range []CreateMeetingIn{{Topic: "", StartTime: "2030-01-01T00:00:00Z", Duration: 30}, {Topic: "T", StartTime: "2030-01-01", Duration: 30}, {Topic: "T", StartTime: "2030-01-01T00:00:00Z", Duration: 0}, {Topic: "T", StartTime: "2030-01-01T00:00:00Z", Duration: 1441}} {
		if _, e := a.createMeeting(ctx, &in); e == nil {
			t.Fatal("create accepted")
		}
	}
	if _, e := a.listUsers(ctx, &ListUsersIn{Status: "all"}); e == nil {
		t.Fatal("status")
	}
	if _, e := a.listMeetings(ctx, &ListMeetingsIn{Type: "instant"}); e == nil {
		t.Fatal("type")
	}
	for _, v := range []schema.MeetingChanges{{}, {Settings: &schema.Settings{}}} {
		if _, e := a.updateMeeting(ctx, &UpdateMeetingIn{MeetingID: "123", Changes: v}); e == nil {
			t.Fatal("empty patch")
		}
	}
	agenda := strings.Repeat("x", 2001)
	if _, e := a.updateMeeting(ctx, &UpdateMeetingIn{MeetingID: "123", Changes: schema.MeetingChanges{Agenda: &agenda}}); e == nil {
		t.Fatal("agenda")
	}
	if *n != 0 {
		t.Fatal("invalid request reached network")
	}
}
func TestMalformedSuccessIsNotEmptyData(t *testing.T) {
	ctx := context.Background()
	for _, body := range []string{"{}", "null", `{"users":[{}]}`, `{"meetings":[{"id":"not-a-number"}]}`} {
		a, _ := fakeAPI(t, "GET", "/v2/users", 200, body, nil)
		if _, e := a.listUsers(ctx, &ListUsersIn{}); e == nil {
			t.Fatal("users accepted", body)
		}
		a, _ = fakeAPI(t, "GET", "/v2/users/me/meetings", 200, body, nil)
		if _, e := a.listMeetings(ctx, &ListMeetingsIn{}); e == nil {
			t.Fatal("meetings accepted", body)
		}
	}
	a, _ := fakeAPI(t, "GET", "/v2/meetings/123", 200, `{}`, nil)
	if _, e := a.getMeeting(ctx, &GetMeetingIn{MeetingID: "123"}); e == nil {
		t.Fatal("missing meeting")
	}
	a, _ = fakeAPI(t, "GET", "/v2/users/me", 200, `{}`, nil)
	o, e := a.health(ctx, &HealthCheckIn{})
	if e != nil || o.OK {
		t.Fatal("bad health")
	}
}
func TestUpdatesNormalizeTimeAndKeepFalse(t *testing.T) {
	ctx := context.Background()
	timeText := "2030-01-01T08:00:00+08:00"
	duration := 45
	title := "Updated"
	a, n := fakeAPI(t, "PATCH", "/v2/meetings/123", 204, "", nil)
	o, e := a.updateMeeting(ctx, &UpdateMeetingIn{MeetingID: "123", Changes: schema.MeetingChanges{Topic: &title, StartTime: &timeText, Duration: &duration}})
	if e != nil || !o.Updated || *n != 1 {
		t.Fatal(o, e)
	}
	if timeText != "2030-01-01T08:00:00+08:00" {
		t.Fatal("mutated caller input")
	}
}
func TestPastUpdateStartRejected(t *testing.T) {
	a, n := fakeAPI(t, "PATCH", "/v2/meetings/123", 204, "", nil)
	past := "2000-01-01T00:00:00Z"
	_, e := a.updateMeeting(context.Background(), &UpdateMeetingIn{MeetingID: "123", Changes: schema.MeetingChanges{StartTime: &past}})
	if e == nil || *n != 0 {
		t.Fatal("past start was silently accepted")
	}
}
