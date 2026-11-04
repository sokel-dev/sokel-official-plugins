package main

import (
	"context"
	"github.com/sokel-dev/sokel-official-plugins/calendly/schema"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestCalendlyURIsAndFilters(t *testing.T) {
	for _, s := range []string{"http://api.calendly.com/users/x", "https://evil.example/users/x", "https://api.calendly.com.evil/users/x", "https://u:p@api.calendly.com/users/x", "https://api.calendly.com/users/x?token=t", "https://api.calendly.com/users/x#f", "https://api.calendly.com/users/%78", "https://api.calendly.com/users/x/y", "https://api.calendly.com/users/.."} {
		if validURI(s, "users") {
			t.Fatal("unsafe URI", s)
		}
	}
	for _, s := range []string{"https://evil.example/a", "http://calendly.com/a", "https://u:p@calendly.com/a", "https://calendly.com"} {
		if bookingURL(s) {
			t.Fatal("unsafe booking link")
		}
	}
	for _, p := range []schema.PageOptions{{Count: ptr(0)}, {Count: ptr(101)}, {Token: ptr("")}, {Token: ptr("bad\n")}, {Token: ptr(strings.Repeat("x", 4097))}} {
		if _, _, _, e := pageValues(p); e == nil {
			t.Fatal("bad page options")
		}
	}
	for _, pair := range [][2]string{{"", ""}, {"https://api.calendly.com/users/u", "https://api.calendly.com/organizations/o"}, {"https://evil.example/users/u", ""}, {"", "https://evil.example/organizations/o"}} {
		if scope(url.Values{}, pair[0], pair[1]) == nil {
			t.Fatal("invalid scope URI")
		}
	}
	q := url.Values{}
	if e := scope(q, "", "https://api.calendly.com/organizations/o"); e != nil || q.Get("organization") == "" {
		t.Fatal(e)
	}
	for _, f := range []schema.EventFilters{{Status: ptr("bad")}, {MinStartTime: ptr("bad")}, {MaxStartTime: ptr("bad")}, {MinStartTime: ptr("2026-11-05T00:00:00Z"), MaxStartTime: ptr("2026-11-04T00:00:00Z")}, {InviteeEmail: ptr("Not an email")}} {
		if eventFilters(url.Values{}, f) == nil {
			t.Fatal("bad filters")
		}
	}
	q = url.Values{}
	e := eventFilters(q, schema.EventFilters{Status: ptr("canceled"), MinStartTime: ptr("2026-11-05T09:00:00+08:00"), MaxStartTime: ptr("2026-11-06T01:00:00Z"), InviteeEmail: ptr("demo@example.com")})
	if e != nil || q.Get("min_start_time") != "2026-11-05T01:00:00Z" || q.Get("invitee_email") != "demo@example.com" {
		t.Fatal(q, e)
	}
	c := fresh()
	c.cred.AccessToken = "bad\n"
	if _, e := c.authorization(); e == nil {
		t.Fatal("bad token")
	}
}
func TestCalendlyPaginationAndReceipts(t *testing.T) {
	ctx := context.Background()
	for _, reply := range []string{`{}`, `{"collection":null,"pagination":{"count":0}}`, `{"collection":[],"pagination":{"count":-1}}`, `{"collection":[],"pagination":{"count":0,"next_page_token":"same"}}`, `{"collection":[],"pagination":{"count":0,"next_page_token":"token-test"}}`, `{"collection":[],"pagination":{"count":0,"next_page_token":"bad\n"}}`, `{"collection":[],"pagination":{"count":0,"next_page":"https://api.calendly.com/event_types?foo=bar"}}`} {
		c := fresh()
		withReply(c, 200, reply)
		if _, e := c.list_event_types(ctx, &ListEventTypesIn{UserURI: "https://api.calendly.com/users/user1", Page: schema.PageOptions{Token: ptr("same")}}); e == nil {
			t.Fatal("invalid pagination accepted", reply)
		}
	}
	c := fresh()
	calls := 0
	c.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "api.calendly.com" {
			t.Fatal("external URL fetched")
		}
		return httpResponse(200, `{"collection":[],"pagination":{"count":0,"next_page_token":"good","next_page":"https://evil.example/?token=token-test"}}`), nil
	})}
	o, e := c.list_event_types(ctx, &ListEventTypesIn{OrganizationURI: "https://api.calendly.com/organizations/org1"})
	if e != nil || o.NextPageToken != "good" || calls != 1 {
		t.Fatal(o, e, calls)
	}
	for _, body := range []string{`{}`, `{"resource":{"booking_url":"https://evil.example/a","owner":"https://api.calendly.com/event_types/type1","owner_type":"EventType"}}`, `{"resource":{"booking_url":"https://calendly.com/a","owner":"https://api.calendly.com/event_types/other","owner_type":"EventType"}}`} {
		c = fresh()
		withReply(c, 201, body)
		if _, e := c.create_scheduling_link(ctx, &CreateSchedulingLinkIn{EventTypeID: "type1"}); e == nil {
			t.Fatal("invalid write receipt")
		}
	}
	for _, body := range []string{`{}`, strings.Replace(eventTypeJSON, `"active":false,`, "", 1), strings.Replace(eventTypeJSON, `"duration":30`, `"duration":0`, 1)} {
		if _, e := eventType([]byte(body)); e == nil {
			t.Fatal("invalid event type")
		}
	}
	for _, body := range []string{`{}`, strings.Replace(eventJSON, `"status":"active"`, `"status":"unknown"`, 1), strings.Replace(eventJSON, `2026-11-05T01:30:00Z`, `2026-11-05T00:00:00Z`, 1)} {
		if _, e := event([]byte(body)); e == nil {
			t.Fatal("invalid event")
		}
	}
	c = fresh()
	withReply(c, 200, `{"resource":`+strings.Replace(eventJSON, "event1", "event2", 1)+`}`)
	if _, e := c.get_event(ctx, &GetEventIn{EventID: "event1"}); e == nil {
		t.Fatal("wrong event ID")
	}
	c = fresh()
	withReply(c, 200, `{"resource":`+strings.Replace(eventTypeJSON, "type1", "type2", 1)+`}`)
	if _, e := c.get_event_type(ctx, &GetEventTypeIn{EventTypeID: "type1"}); e == nil {
		t.Fatal("wrong event type ID")
	}
}
func TestCalendlyAvailabilityBounds(t *testing.T) {
	ctx := context.Background()
	c := fresh()
	for _, in := range []ListAvailableTimesIn{{EventTypeID: "../x"}, {EventTypeID: "type1", StartTime: "bad"}, {EventTypeID: "type1", StartTime: "2099-01-01T00:00:00Z", EndTime: "bad"}, {EventTypeID: "type1", StartTime: "2099-01-01T00:00:00Z", EndTime: "2099-02-01T00:00:01Z"}, {EventTypeID: "type1", StartTime: "2099-01-01T00:00:00Z", EndTime: "2099-01-01T00:00:00Z"}, {EventTypeID: "type1", StartTime: "2000-01-01T00:00:00Z", EndTime: "2000-01-02T00:00:00Z"}} {
		if _, e := c.list_available_times(ctx, &in); e == nil {
			t.Fatal("bad availability input")
		}
	}
	in := &ListAvailableTimesIn{EventTypeID: "type1", StartTime: "2099-01-01T00:00:00Z", EndTime: "2099-01-02T00:00:00Z"}
	for _, b := range []string{`{}`, `{"collection":[{}]}`, `{"collection":[{"status":"available","start_time":"2099-01-03T00:00:00Z","invitees_remaining":1,"scheduling_url":"https://calendly.com/a"}]}`, `{"collection":[{"status":"available","start_time":"2099-01-01T01:00:00Z","invitees_remaining":0,"scheduling_url":"https://calendly.com/a"}]}`} {
		withReply(c, 200, b)
		if _, e := c.list_available_times(ctx, in); e == nil {
			t.Fatal("invalid availability accepted")
		}
	}
	withReply(c, 200, `{"collection":[]}`)
	o, e := c.list_available_times(ctx, in)
	if e != nil || o.Times == nil {
		t.Fatal(o, e)
	}
	c = fresh()
	withReply(c, 200, `{"collection":[{"uri":"https://api.calendly.com/scheduled_events/event1/invitees/i","event":"https://api.calendly.com/scheduled_events/event1","email":"a@example.com","status":"active","created_at":"2026-11-04T01:00:00Z"}],"pagination":{"count":1}}`)
	iv, e := c.list_invitees(ctx, &ListInviteesIn{EventID: "event1"})
	if e != nil || iv.Invitees[0].QuestionsAndAnswers == nil {
		t.Fatal(iv, e)
	}
	for _, input := range []ListInviteesIn{{EventID: "../x"}, {EventID: "event1", Status: "unknown"}, {EventID: "event1", Page: schema.PageOptions{Count: ptr(0)}}} {
		if _, e := c.list_invitees(ctx, &input); e == nil {
			t.Fatal("bad invitee input")
		}
	}
	if time.Now().Year() >= 2099 {
		t.Fatal("update synthetic future test dates")
	}
}
