package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-official-plugins/calendly/schema"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func checkID(s string) error {
	if !idPattern.MatchString(s) {
		return errors.New("Provide a Calendly resource ID without a URL, path or query string.")
	}
	return nil
}
func resourceURI(kind, id string) string { return origin + "/" + kind + "/" + id }
func validURI(raw, kind string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Host != "api.calendly.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
		return false
	}
	prefix := "/" + kind + "/"
	return strings.HasPrefix(u.Path, prefix) && idPattern.MatchString(strings.TrimPrefix(u.Path, prefix))
}
func bookingURL(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Scheme == "https" && (u.Host == "calendly.com" || u.Host == "www.calendly.com") && u.User == nil && u.Fragment == "" && u.Path != ""
}
func (c *client) authorization() (string, error) {
	if !safeToken(c.cred.AccessToken) {
		return "", errors.New("Provide a valid Calendly personal access token.")
	}
	return "Bearer " + c.cred.AccessToken, nil
}
func (c *client) get_current_user(ctx context.Context, in *GetCurrentUserIn) (*GetCurrentUserOut, error) {
	var r struct {
		Resource schema.User `json:"resource"`
	}
	if e := c.json(ctx, "GET", "/users/me", nil, nil, &r); e != nil {
		return nil, e
	}
	v := r.Resource
	if !validURI(v.URI, "users") || !validURI(v.OrganizationURI, "organizations") || v.Email == "" {
		return nil, errors.New("Calendly did not return a valid user identity.")
	}
	return &GetCurrentUserOut{User: v}, nil
}
func (c *client) health_check(ctx context.Context, in *HealthCheckIn) (*HealthCheckOut, error) {
	_, e := c.get_current_user(ctx, &GetCurrentUserIn{})
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Calendly user identity verified. Other operations require their own scopes and resource access."}, nil
}
func pageValues(p schema.PageOptions) (url.Values, string, int, error) {
	n := 100
	if p.Count != nil {
		n = *p.Count
	}
	if n < 1 || n > 100 {
		return nil, "", 0, errors.New("Page size must be between 1 and 100.")
	}
	q := url.Values{"count": {strconv.Itoa(n)}}
	token := ""
	if p.Token != nil {
		token = *p.Token
		if !validCursor(token) {
			return nil, "", 0, errors.New("Page token must contain 1 to 4096 printable ASCII characters without spaces.")
		}
		q.Set("page_token", token)
	}
	return q, token, n, nil
}
func validCursor(s string) bool { return len(s) > 0 && len(s) <= 4096 && safeToken(s) }
func scope(q url.Values, user, org string) error {
	if (user == "") == (org == "") {
		return errors.New("Provide exactly one user URI or organization URI.")
	}
	if user != "" {
		if !validURI(user, "users") {
			return errors.New("User URI must identify a user on api.calendly.com.")
		}
		q.Set("user", user)
	} else {
		if !validURI(org, "organizations") {
			return errors.New("Organization URI must identify an organization on api.calendly.com.")
		}
		q.Set("organization", org)
	}
	return nil
}
func (c *client) collection(ctx context.Context, path string, q url.Values, previous string, limit int) ([]json.RawMessage, string, error) {
	var r struct {
		Collection []json.RawMessage `json:"collection"`
		Pagination *struct {
			Count     *int    `json:"count"`
			NextToken *string `json:"next_page_token"`
			Next      *string `json:"next_page"`
		} `json:"pagination"`
	}
	if e := c.json(ctx, "GET", path, q, nil, &r); e != nil {
		return nil, "", e
	}
	if r.Collection == nil || len(r.Collection) > limit || r.Pagination == nil || r.Pagination.Count == nil || *r.Pagination.Count < 0 {
		return nil, "", errors.New("Calendly returned missing or inconsistent pagination metadata.")
	}
	next := ""
	if r.Pagination.NextToken != nil {
		next = *r.Pagination.NextToken
	}
	if next != "" && (!validCursor(next) || next == previous || next == c.cred.AccessToken) {
		return nil, "", errors.New("Calendly returned an invalid or repeated pagination token.")
	}
	if r.Pagination.Next != nil && *r.Pagination.Next != "" && next == "" {
		return nil, "", errors.New("Calendly returned a next-page URL without its required page token.")
	}
	return r.Collection, next, nil
}
func eventType(b []byte) (schema.EventType, error) {
	var r schema.EventType
	if e := requireKeys(b, "uri", "name", "active", "duration", "kind", "scheduling_url"); e != nil {
		return r, e
	}
	if e := decodeJSON(b, &r); e != nil {
		return r, e
	}
	if !validURI(r.URI, "event_types") || r.Name == "" || r.Duration < 1 || !bookingURL(r.SchedulingURL) {
		return r, errors.New("Calendly returned incomplete event type metadata.")
	}
	return r, nil
}
func event(b []byte) (schema.Event, error) {
	var r schema.Event
	if e := decodeJSON(b, &r); e != nil {
		return r, e
	}
	if !validURI(r.URI, "scheduled_events") || !validURI(r.EventTypeURI, "event_types") || r.Name == "" || (r.Status != "active" && r.Status != "canceled") {
		return r, errors.New("Calendly returned incomplete event metadata.")
	}
	s, e := time.Parse(time.RFC3339Nano, r.StartTime)
	f, e2 := time.Parse(time.RFC3339Nano, r.EndTime)
	if e != nil || e2 != nil || !f.After(s) {
		return r, errors.New("Calendly returned invalid event timestamps.")
	}
	return r, nil
}
func (c *client) list_event_types(ctx context.Context, in *ListEventTypesIn) (*ListEventTypesOut, error) {
	q, token, n, e := pageValues(in.Page)
	if e != nil {
		return nil, e
	}
	if e = scope(q, in.UserURI, in.OrganizationURI); e != nil {
		return nil, e
	}
	if in.Filters.Active != nil {
		q.Set("active", strconv.FormatBool(*in.Filters.Active))
	}
	rows, next, e := c.collection(ctx, "/event_types", q, token, n)
	if e != nil {
		return nil, e
	}
	out := &ListEventTypesOut{EventTypes: []schema.EventType{}, NextPageToken: next}
	for _, b := range rows {
		r, e := eventType(b)
		if e != nil {
			return nil, e
		}
		out.EventTypes = append(out.EventTypes, r)
	}
	return out, nil
}
func (c *client) get_event_type(ctx context.Context, in *GetEventTypeIn) (*GetEventTypeOut, error) {
	if e := checkID(in.EventTypeID); e != nil {
		return nil, e
	}
	var r struct {
		Resource json.RawMessage `json:"resource"`
	}
	if e := c.json(ctx, "GET", "/event_types/"+in.EventTypeID, nil, nil, &r); e != nil {
		return nil, e
	}
	v, e := eventType(r.Resource)
	if e != nil {
		return nil, e
	}
	if v.URI != resourceURI("event_types", in.EventTypeID) {
		return nil, errors.New("Calendly returned a different event type.")
	}
	return &GetEventTypeOut{EventType: v}, nil
}
func status(s string) error {
	if s != "active" && s != "canceled" {
		return errors.New("Status must be active or canceled.")
	}
	return nil
}
func utc(s string) (time.Time, error) {
	v, e := time.Parse(time.RFC3339Nano, s)
	if e != nil {
		return time.Time{}, errors.New("Time must be RFC3339 with an explicit timezone offset.")
	}
	return v.UTC(), nil
}
func eventFilters(q url.Values, f schema.EventFilters) error {
	if f.Status != nil {
		if e := status(*f.Status); e != nil {
			return e
		}
		q.Set("status", *f.Status)
	}
	var start, end time.Time
	if f.MinStartTime != nil {
		v, e := utc(*f.MinStartTime)
		if e != nil {
			return e
		}
		start = v
		q.Set("min_start_time", v.Format(time.RFC3339Nano))
	}
	if f.MaxStartTime != nil {
		v, e := utc(*f.MaxStartTime)
		if e != nil {
			return e
		}
		end = v
		q.Set("max_start_time", v.Format(time.RFC3339Nano))
	}
	if !start.IsZero() && !end.IsZero() && !end.After(start) {
		return errors.New("Latest start time must be later than earliest start time.")
	}
	if f.InviteeEmail != nil {
		v, e := mail.ParseAddress(*f.InviteeEmail)
		if e != nil || v.Address != *f.InviteeEmail || len(v.Address) > 320 {
			return errors.New("Invitee email must be a plain email address.")
		}
		q.Set("invitee_email", *f.InviteeEmail)
	}
	return nil
}
func (c *client) list_events(ctx context.Context, in *ListEventsIn) (*ListEventsOut, error) {
	q, token, n, e := pageValues(in.Page)
	if e != nil {
		return nil, e
	}
	if e = scope(q, in.UserURI, in.OrganizationURI); e != nil {
		return nil, e
	}
	if e = eventFilters(q, in.Filters); e != nil {
		return nil, e
	}
	rows, next, e := c.collection(ctx, "/scheduled_events", q, token, n)
	if e != nil {
		return nil, e
	}
	out := &ListEventsOut{Events: []schema.Event{}, NextPageToken: next}
	for _, b := range rows {
		r, e := event(b)
		if e != nil {
			return nil, e
		}
		out.Events = append(out.Events, r)
	}
	return out, nil
}
func (c *client) get_event(ctx context.Context, in *GetEventIn) (*GetEventOut, error) {
	if e := checkID(in.EventID); e != nil {
		return nil, e
	}
	var r struct {
		Resource json.RawMessage `json:"resource"`
	}
	if e := c.json(ctx, "GET", "/scheduled_events/"+in.EventID, nil, nil, &r); e != nil {
		return nil, e
	}
	v, e := event(r.Resource)
	if e != nil {
		return nil, e
	}
	if v.URI != resourceURI("scheduled_events", in.EventID) {
		return nil, errors.New("Calendly returned a different event.")
	}
	return &GetEventOut{Event: v}, nil
}
func (c *client) list_invitees(ctx context.Context, in *ListInviteesIn) (*ListInviteesOut, error) {
	if e := checkID(in.EventID); e != nil {
		return nil, e
	}
	q, token, n, e := pageValues(in.Page)
	if e != nil {
		return nil, e
	}
	if in.Status != "" {
		if e = status(in.Status); e != nil {
			return nil, e
		}
		q.Set("status", in.Status)
	}
	rows, next, e := c.collection(ctx, "/scheduled_events/"+in.EventID+"/invitees", q, token, n)
	if e != nil {
		return nil, e
	}
	out := &ListInviteesOut{Invitees: []schema.Invitee{}, NextPageToken: next}
	for _, b := range rows {
		var r schema.Invitee
		if e := decodeJSON(b, &r); e != nil {
			return nil, e
		}
		if !validURI(r.URI, "scheduled_events/"+in.EventID+"/invitees") || r.EventURI != resourceURI("scheduled_events", in.EventID) || r.Email == "" || status(r.Status) != nil || r.CreatedAt == "" {
			return nil, errors.New("Calendly returned incomplete or mismatched invitee metadata.")
		}
		if r.QuestionsAndAnswers == nil {
			r.QuestionsAndAnswers = []schema.QuestionAnswer{}
		}
		out.Invitees = append(out.Invitees, r)
	}
	return out, nil
}
func (c *client) list_available_times(ctx context.Context, in *ListAvailableTimesIn) (*ListAvailableTimesOut, error) {
	if e := checkID(in.EventTypeID); e != nil {
		return nil, e
	}
	start, e := utc(in.StartTime)
	if e != nil {
		return nil, e
	}
	end, e := utc(in.EndTime)
	if e != nil {
		return nil, e
	}
	if !end.After(start) || end.Sub(start) > 31*24*time.Hour || start.Before(time.Now()) {
		return nil, errors.New("Availability must start in the future and span more than zero and at most 31 days.")
	}
	q := url.Values{"event_type": {resourceURI("event_types", in.EventTypeID)}, "start_time": {start.Format(time.RFC3339Nano)}, "end_time": {end.Format(time.RFC3339Nano)}}
	var r struct {
		Collection []json.RawMessage `json:"collection"`
	}
	if e := c.json(ctx, "GET", "/event_type_available_times", q, nil, &r); e != nil {
		return nil, e
	}
	if r.Collection == nil {
		return nil, errors.New("Calendly returned a missing availability collection.")
	}
	out := &ListAvailableTimesOut{Times: []schema.AvailableTime{}}
	for _, b := range r.Collection {
		if e := requireKeys(b, "status", "start_time", "invitees_remaining", "scheduling_url"); e != nil {
			return nil, e
		}
		var v schema.AvailableTime
		if e := decodeJSON(b, &v); e != nil {
			return nil, e
		}
		t, e := utc(v.StartTime)
		if e != nil || t.Before(start) || t.After(end) || v.Status != "available" || v.InviteesRemaining < 1 || !bookingURL(v.SchedulingURL) {
			return nil, errors.New("Calendly returned invalid availability metadata.")
		}
		out.Times = append(out.Times, v)
	}
	return out, nil
}
func (c *client) create_scheduling_link(ctx context.Context, in *CreateSchedulingLinkIn) (*CreateSchedulingLinkOut, error) {
	if e := checkID(in.EventTypeID); e != nil {
		return nil, e
	}
	owner := resourceURI("event_types", in.EventTypeID)
	var r struct {
		Resource struct {
			BookingURL string `json:"booking_url"`
			Owner      string `json:"owner"`
			OwnerType  string `json:"owner_type"`
		} `json:"resource"`
	}
	if e := c.json(ctx, "POST", "/scheduling_links", nil, map[string]any{"max_event_count": 1, "owner": owner, "owner_type": "EventType"}, &r); e != nil {
		return nil, e
	}
	if r.Resource.Owner != owner || r.Resource.OwnerType != "EventType" || !bookingURL(r.Resource.BookingURL) {
		return nil, errors.New("Calendly returned no valid scheduling link receipt. Inspect existing links before retrying.")
	}
	return &CreateSchedulingLinkOut{BookingURL: r.Resource.BookingURL, EventTypeURI: owner}, nil
}
