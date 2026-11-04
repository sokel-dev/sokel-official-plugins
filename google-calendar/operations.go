package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/sokel-dev/sokel-official-plugins/google-calendar/schema"
	"net/mail"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const calendarOrigin = "https://www.googleapis.com"

func calendarPath(id string) (string, error) {
	if id == "" {
		id = "primary"
	}
	id, e := segment(id, "calendar_id")
	return "/calendar/v3/calendars/" + id + "/events", e
}
func validateTimeRange(start, end string, allDay bool) error {
	layout := time.RFC3339
	if allDay {
		layout = "2006-01-02"
	}
	s, e := time.Parse(layout, start)
	if e != nil {
		return fmt.Errorf("start must use %s", layout)
	}
	f, e := time.Parse(layout, end)
	if e != nil {
		return fmt.Errorf("end must use %s", layout)
	}
	if !f.After(s) {
		return fmt.Errorf("end must be after start; all-day end is exclusive")
	}
	return nil
}
func notifyMode(v string) (string, error) {
	if v == "" {
		v = "none"
	}
	if v != "none" && v != "all" && v != "externalOnly" {
		return "", fmt.Errorf("send_updates must be none, all or externalOnly")
	}
	return v, nil
}
func (a *api) list(ctx context.Context, in *ListEventsIn) (*ListEventsOut, error) {
	p, e := calendarPath(in.CalendarID)
	if e != nil {
		return nil, e
	}
	limit := in.PageSize
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 2500 {
		return nil, fmt.Errorf("page_size must be between 1 and 2500")
	}
	q := url.Values{"maxResults": {strconv.Itoa(limit)}, "singleEvents": {"true"}, "orderBy": {"startTime"}}
	for k, v := range map[string]string{"timeMin": in.TimeMin, "timeMax": in.TimeMax, "q": in.Query, "pageToken": in.PageToken} {
		if v != "" {
			if k == "timeMin" || k == "timeMax" {
				if _, e := time.Parse(time.RFC3339, v); e != nil {
					return nil, fmt.Errorf("time bounds must be RFC3339 with an offset")
				}
			}
			q.Set(k, v)
		}
	}
	if in.TimeMin != "" && in.TimeMax != "" {
		if e := validateTimeRange(in.TimeMin, in.TimeMax, false); e != nil {
			return nil, e
		}
	}
	var r struct {
		Items []schema.CalendarEvent `json:"items"`
		Next  string                 `json:"nextPageToken"`
	}
	e = a.json(ctx, "GET", calendarOrigin, p, q, nil, &r)
	if r.Items == nil {
		r.Items = []schema.CalendarEvent{}
	}
	return &ListEventsOut{Events: r.Items, NextPageToken: r.Next}, e
}
func (a *api) get(ctx context.Context, calendarID, eventID string) (schema.CalendarEvent, error) {
	var out schema.CalendarEvent
	p, e := calendarPath(calendarID)
	if e != nil {
		return out, e
	}
	id, e := segment(eventID, "event_id")
	if e != nil {
		return out, e
	}
	e = a.json(ctx, "GET", calendarOrigin, p+"/"+id, nil, nil, &out)
	return out, e
}
func (a *api) save(ctx context.Context, eventID string, in *CreateEventIn) (schema.CalendarEvent, error) {
	var out schema.CalendarEvent
	if strings.TrimSpace(in.Summary) == "" {
		return out, fmt.Errorf("summary is required")
	}
	if e := validateTimeRange(in.Start, in.End, in.AllDay); e != nil {
		return out, e
	}
	if in.TimeZone != "" {
		if _, e := time.LoadLocation(in.TimeZone); e != nil {
			return out, fmt.Errorf("time_zone must be an IANA time zone")
		}
	}
	mode, e := notifyMode(in.SendUpdates)
	if e != nil {
		return out, e
	}
	p, e := calendarPath(in.CalendarID)
	if e != nil {
		return out, e
	}
	start := map[string]any{"date": nil, "dateTime": in.Start, "timeZone": nil}
	end := map[string]any{"date": nil, "dateTime": in.End, "timeZone": nil}
	if in.TimeZone != "" {
		start["timeZone"] = in.TimeZone
		end["timeZone"] = in.TimeZone
	}
	if in.AllDay {
		start = map[string]any{"date": in.Start, "dateTime": nil, "timeZone": nil}
		end = map[string]any{"date": in.End, "dateTime": nil, "timeZone": nil}
	}
	attendees := []schema.Attendee{}
	if len(in.Attendees) > 200 {
		return out, fmt.Errorf("at most 200 attendee addresses are supported")
	}
	for _, email := range in.Attendees {
		addr, e := mail.ParseAddress(email)
		if e != nil || addr.Address != email {
			return out, fmt.Errorf("attendees must contain plain email addresses")
		}
		attendees = append(attendees, schema.Attendee{Email: email})
	}
	body := map[string]any{"summary": in.Summary, "description": in.Description, "location": in.Location, "start": start, "end": end, "attendees": attendees}
	method := "POST"
	if eventID != "" {
		id, e := segment(eventID, "event_id")
		if e != nil {
			return out, e
		}
		p += "/" + id
		method = "PATCH"
	}
	e = a.json(ctx, method, calendarOrigin, p, url.Values{"sendUpdates": {mode}}, body, &out)
	return out, e
}
func (a *api) remove(ctx context.Context, in *DeleteEventIn) error {
	p, e := calendarPath(in.CalendarID)
	if e != nil {
		return e
	}
	id, e := segment(in.EventID, "event_id")
	if e != nil {
		return e
	}
	mode, e := notifyMode(in.SendUpdates)
	if e != nil {
		return e
	}
	return a.json(ctx, "DELETE", calendarOrigin, p+"/"+id, url.Values{"sendUpdates": {mode}}, nil, nil)
}
func (a *api) freebusy(ctx context.Context, in *FreeBusyIn) (*FreeBusyOut, error) {
	if e := validateTimeRange(in.TimeMin, in.TimeMax, false); e != nil {
		return nil, e
	}
	if len(in.CalendarIDs) == 0 || len(in.CalendarIDs) > 50 {
		return nil, fmt.Errorf("calendar_ids must contain 1 to 50 calendars")
	}
	items := []map[string]string{}
	for _, id := range in.CalendarIDs {
		if strings.TrimSpace(id) == "" {
			return nil, fmt.Errorf("calendar ID cannot be empty")
		}
		items = append(items, map[string]string{"id": id})
	}
	var r struct {
		Calendars map[string]struct {
			Busy   []schema.BusyPeriod `json:"busy"`
			Errors []struct {
				Reason string `json:"reason"`
			} `json:"errors"`
		} `json:"calendars"`
	}
	if e := a.json(ctx, "POST", calendarOrigin, "/calendar/v3/freeBusy", nil, map[string]any{"timeMin": in.TimeMin, "timeMax": in.TimeMax, "items": items}, &r); e != nil {
		return nil, e
	}
	out := &FreeBusyOut{Calendars: []schema.FreeBusyCalendar{}}
	ids := make([]string, 0, len(r.Calendars))
	for id := range r.Calendars {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		c := r.Calendars[id]
		entry := schema.FreeBusyCalendar{CalendarID: id, Busy: c.Busy, Errors: []string{}}
		if entry.Busy == nil {
			entry.Busy = []schema.BusyPeriod{}
		}
		for _, e := range c.Errors {
			entry.Errors = append(entry.Errors, e.Reason)
		}
		out.Calendars = append(out.Calendars, entry)
	}
	return out, nil
}

type syncCursor struct {
	Calendar string `json:"calendar"`
	Token    string `json:"token"`
}

func (a *api) poll(ctx context.Context, cred Cred, cursor string, emit func(string, *EventChangedEvent) error) (string, error) {
	p, e := calendarPath(cred.WatchCalendarID)
	if e != nil {
		return cursor, e
	}
	cur := syncCursor{}
	if cursor != "" {
		if json.Unmarshal([]byte(cursor), &cur) != nil {
			return cursor, fmt.Errorf("invalid polling cursor; clear it to rebaseline")
		}
		if cur.Calendar != cred.WatchCalendarID {
			cur = syncCursor{}
		}
	}
	baseline := cur.Token == ""
	q := url.Values{"maxResults": {"2500"}, "showDeleted": {"true"}, "singleEvents": {"false"}}
	if !baseline {
		q.Set("syncToken", cur.Token)
	}
	seen := map[string]bool{}
	for n := 0; n < 1000; n++ {
		page := q.Get("pageToken")
		if seen[page] {
			return cursor, fmt.Errorf("Google repeated a calendar page token")
		}
		seen[page] = true
		var r struct {
			Items []schema.CalendarEvent `json:"items"`
			Next  string                 `json:"nextPageToken"`
			Sync  string                 `json:"nextSyncToken"`
		}
		if e = a.json(ctx, "GET", calendarOrigin, p, q, nil, &r); e != nil {
			return cursor, e
		}
		if !baseline {
			for _, v := range r.Items {
				if v.ID == "" {
					return cursor, fmt.Errorf("Google returned a calendar change without an event ID")
				}
				if e = emit(eventKey(cred.WatchCalendarID, v.ID, v.ETag, v.Updated, v.Status), &EventChangedEvent{CalendarID: cred.WatchCalendarID, Event: v}); e != nil {
					return cursor, e
				}
			}
		}
		if r.Next == "" {
			if r.Sync == "" {
				return cursor, fmt.Errorf("Google returned no final sync token")
			}
			b, e := json.Marshal(syncCursor{Calendar: cred.WatchCalendarID, Token: r.Sync})
			return string(b), e
		}
		q.Set("pageToken", r.Next)
	}
	return cursor, fmt.Errorf("calendar backlog exceeds 1000 pages; cursor has not advanced")
}
