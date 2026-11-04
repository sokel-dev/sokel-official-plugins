package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func ptr[T any](v T) *T                                                   { return &v }

// Fixtures below are synthetic examples based on the official reference shapes.
// They are not captured from a live account. The transport checks the actual request.
func fixture(t *testing.T, c *client, method, path string, query url.Values, body string, status int, reply string) {
	t.Helper()
	c.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Helper()
		if r.Method != method || r.URL.Scheme != "https" || r.URL.Host != "api.calendly.com" || r.URL.Path != path {
			t.Fatalf("request %s %s", r.Method, r.URL)
		}
		if r.URL.Query().Encode() != query.Encode() {
			t.Fatalf("query %s want %s", r.URL.RawQuery, query.Encode())
		}
		if r.Header.Get("Authorization") != "Bearer token-test" {
			t.Fatal("missing header authorization")
		}
		if r.URL.Query().Get("token") != "" || r.URL.Query().Get("key") != "" || r.URL.Query().Get("access_token") != "" {
			t.Fatal("URL credential leak")
		}
		b, e := io.ReadAll(r.Body)
		if e != nil {
			t.Fatal(e)
		}
		if string(b) != body {
			t.Fatalf("body %s want %s", b, body)
		}
		if body != "" && r.Header.Get("Content-Type") != "application/json" {
			t.Fatal("missing JSON Content-Type")
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(reply)), Request: r}, nil
	})}
}
func bind(t *testing.T, body string, out any) {
	t.Helper()
	if e := contract.BindInput(json.RawMessage(body), out); e != nil {
		t.Fatal(e)
	}
}

func fresh() *client { return newClient(Cred{AccessToken: "token-test"}) }

const userJSON = `{"uri":"https://api.calendly.com/users/user1","name":"Demo","email":"demo@example.com","timezone":"UTC","current_organization":"https://api.calendly.com/organizations/org1","scheduling_url":"https://calendly.com/demo"}`
const eventTypeJSON = `{"uri":"https://api.calendly.com/event_types/type1","name":"Intro","active":false,"duration":30,"kind":"solo","scheduling_url":"https://calendly.com/demo/intro"}`
const eventJSON = `{"uri":"https://api.calendly.com/scheduled_events/event1","name":"Intro","status":"active","start_time":"2026-11-05T01:00:00Z","end_time":"2026-11-05T01:30:00Z","event_type":"https://api.calendly.com/event_types/type1","location":{"type":"physical","location":"Office"}}`

func TestDocumentedWireRequests(t *testing.T) {
	ctx := context.Background()
	t.Run("health", func(t *testing.T) {
		c := fresh()
		fixture(t, c, "GET", "/users/me", nil, "", 200, `{"resource":`+userJSON+`}`)
		o, e := c.health_check(ctx, &HealthCheckIn{})
		if e != nil || o == nil || !o.OK {
			t.Fatalf("%+v %v", o, e)
		}
	})
	t.Run("user", func(t *testing.T) {
		c := fresh()
		fixture(t, c, "GET", "/users/me", nil, "", 200, `{"resource":`+userJSON+`}`)
		o, e := c.get_current_user(ctx, &GetCurrentUserIn{})
		if e != nil || o == nil || o.User.URI != "https://api.calendly.com/users/user1" {
			t.Fatalf("%+v %v", o, e)
		}
	})
	t.Run("types false with token", func(t *testing.T) {
		c := fresh()
		var in ListEventTypesIn
		bind(t, `{"user_uri":"https://api.calendly.com/users/user1","filters":{"active":false},"page":{"count":10,"token":"abc+/="}}`, &in)
		fixture(t, c, "GET", "/event_types", url.Values{"user": {"https://api.calendly.com/users/user1"}, "count": {"10"}, "page_token": {"abc+/="}, "active": {"false"}}, "", 200, `{"collection":[`+eventTypeJSON+`],"pagination":{"count":1,"next_page_token":"next+/=","next_page":"https://api.calendly.com/event_types?count=10&page_token=next%2B%2F%3D"}}`)
		o, e := c.list_event_types(ctx, &in)
		if e != nil || o == nil || len(o.EventTypes) != 1 || o.EventTypes[0].Active || o.NextPageToken != "next+/=" {
			t.Fatalf("%+v %v", o, e)
		}
	})
	t.Run("type", func(t *testing.T) {
		c := fresh()
		fixture(t, c, "GET", "/event_types/type1", nil, "", 200, `{"resource":`+eventTypeJSON+`}`)
		o, e := c.get_event_type(ctx, &GetEventTypeIn{EventTypeID: "type1"})
		if e != nil || o == nil || o.EventType.Duration != 30 {
			t.Fatalf("%+v %v", o, e)
		}
	})
	t.Run("events", func(t *testing.T) {
		c := fresh()
		fixture(t, c, "GET", "/scheduled_events", url.Values{"user": {"https://api.calendly.com/users/user1"}, "count": {"100"}}, "", 200, `{"collection":[`+eventJSON+`],"pagination":{"count":1,"next_page_token":null,"next_page":null}}`)
		o, e := c.list_events(ctx, &ListEventsIn{UserURI: "https://api.calendly.com/users/user1"})
		if e != nil || o == nil || len(o.Events) != 1 || o.NextPageToken != "" {
			t.Fatalf("%+v %v", o, e)
		}
	})
	t.Run("event", func(t *testing.T) {
		c := fresh()
		fixture(t, c, "GET", "/scheduled_events/event1", nil, "", 200, `{"resource":`+eventJSON+`}`)
		o, e := c.get_event(ctx, &GetEventIn{EventID: "event1"})
		if e != nil || o == nil || o.Event.Status != "active" {
			t.Fatalf("%+v %v", o, e)
		}
	})
	t.Run("invitees", func(t *testing.T) {
		c := fresh()
		fixture(t, c, "GET", "/scheduled_events/event1/invitees", url.Values{"count": {"100"}, "status": {"active"}}, "", 200, `{"collection":[{"uri":"https://api.calendly.com/scheduled_events/event1/invitees/person1","event":"https://api.calendly.com/scheduled_events/event1","name":"Jane","email":"jane@example.com","status":"active","created_at":"2026-11-04T01:00:00Z","questions_and_answers":[{"question":"Topic?","answer":"Sales","position":0}]}],"pagination":{"count":1,"next_page_token":null,"next_page":null}}`)
		o, e := c.list_invitees(ctx, &ListInviteesIn{EventID: "event1", Status: "active"})
		if e != nil || o == nil || len(o.Invitees) != 1 || o.Invitees[0].QuestionsAndAnswers[0].Answer != "Sales" {
			t.Fatalf("%+v %v", o, e)
		}
	})
	t.Run("availability", func(t *testing.T) {
		c := fresh()
		fixture(t, c, "GET", "/event_type_available_times", url.Values{"event_type": {"https://api.calendly.com/event_types/type1"}, "start_time": {"2099-01-01T00:00:00Z"}, "end_time": {"2099-02-01T00:00:00Z"}}, "", 200, `{"collection":[{"status":"available","start_time":"2099-01-02T01:00:00Z","invitees_remaining":2,"scheduling_url":"https://calendly.com/demo/intro"}]}`)
		o, e := c.list_available_times(ctx, &ListAvailableTimesIn{EventTypeID: "type1", StartTime: "2099-01-01T00:00:00Z", EndTime: "2099-02-01T00:00:00Z"})
		if e != nil || o == nil || len(o.Times) != 1 || o.Times[0].InviteesRemaining != 2 {
			t.Fatalf("%+v %v", o, e)
		}
	})
	t.Run("single-use link", func(t *testing.T) {
		c := fresh()
		fixture(t, c, "POST", "/scheduling_links", nil, `{"max_event_count":1,"owner":"https://api.calendly.com/event_types/type1","owner_type":"EventType"}`, 201, `{"resource":{"booking_url":"https://calendly.com/d/demo","owner":"https://api.calendly.com/event_types/type1","owner_type":"EventType"}}`)
		o, e := c.create_scheduling_link(ctx, &CreateSchedulingLinkIn{EventTypeID: "type1"})
		if e != nil || o == nil || o.BookingURL != "https://calendly.com/d/demo" {
			t.Fatalf("%+v %v", o, e)
		}
	})
}
