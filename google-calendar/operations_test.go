package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestCalendarCreateTimedAllDayAndUpdate(t *testing.T) {
	calls := 0
	a := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("sendUpdates") != "none" {
			t.Error("unexpected notifications")
		}
		var b map[string]any
		if json.NewDecoder(r.Body).Decode(&b) != nil {
			t.Fatal("body")
		}
		start := b["start"].(map[string]any)
		if calls == 1 {
			if start["dateTime"] != "2026-11-03T09:00:00+08:00" {
				t.Error("offset lost")
			}
		} else {
			if start["date"] != "2026-11-03" || start["dateTime"] != nil {
				t.Error("all day encoding")
			}
		}
		if calls == 3 && (r.Method != "PATCH" || !strings.HasSuffix(r.URL.Path, "/evt")) {
			t.Error("update method")
		}
		reply(w, `{"id":"evt","summary":"Meeting","start":{"date":"2026-11-03"},"end":{"date":"2026-11-04"}}`)
	})
	in := &CreateEventIn{Summary: "Meeting", Start: "2026-11-03T09:00:00+08:00", End: "2026-11-03T10:00:00+08:00", TimeZone: "Asia/Shanghai", Attendees: []string{"a@example.com"}}
	if _, e := a.save(context.Background(), "", in); e != nil {
		t.Fatal(e)
	}
	in.AllDay = true
	in.Start = "2026-11-03"
	in.End = "2026-11-04"
	if _, e := a.save(context.Background(), "", in); e != nil {
		t.Fatal(e)
	}
	if _, e := a.save(context.Background(), "evt", in); e != nil {
		t.Fatal(e)
	}
}
func TestCalendarTimeValidation(t *testing.T) {
	a := newAPI("token")
	for _, in := range []*CreateEventIn{{Summary: "x", Start: "2026-11-03T09:00:00", End: "2026-11-03T10:00:00"}, {Summary: "x", AllDay: true, Start: "2026-11-03", End: "2026-11-03"}, {Summary: "x", Start: "2026-11-03T09:00:00Z", End: "2026-11-03T10:00:00Z", TimeZone: "No/SuchZone"}} {
		if _, e := a.save(context.Background(), "", in); e == nil {
			t.Fatal("invalid time accepted")
		}
	}
}
func TestCalendarListGetDeleteAndFreebusy(t *testing.T) {
	a := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "freeBusy") {
			reply(w, `{"calendars":{"a":{"busy":[{"start":"2026-11-03T01:00:00Z","end":"2026-11-03T02:00:00Z"}]},"b":{"errors":[{"reason":"notFound"}]}}}`)
			return
		}
		if r.Method == "DELETE" {
			w.WriteHeader(204)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/evt") {
			reply(w, `{"id":"evt"}`)
			return
		}
		if r.URL.Query().Get("pageToken") != "p2" {
			t.Error("page token")
		}
		reply(w, `{"items":[{"id":"evt","status":"confirmed"}],"nextPageToken":"p3"}`)
	})
	o, e := a.list(context.Background(), &ListEventsIn{PageToken: "p2", TimeMin: "2026-11-03T00:00:00Z"})
	if e != nil || o.NextPageToken != "p3" {
		t.Fatalf("list %v %v", o, e)
	}
	v, e := a.get(context.Background(), "primary", "evt")
	if e != nil || v.ID != "evt" {
		t.Fatal(e)
	}
	if e = a.remove(context.Background(), &DeleteEventIn{EventID: "evt"}); e != nil {
		t.Fatal(e)
	}
	f, e := a.freebusy(context.Background(), &FreeBusyIn{TimeMin: "2026-11-03T00:00:00Z", TimeMax: "2026-11-04T00:00:00Z", CalendarIDs: []string{"a", "b"}})
	if e != nil || len(f.Calendars) != 2 || len(f.Calendars[1].Errors) != 1 {
		t.Fatalf("freebusy %v %v", f, e)
	}
}
func TestCalendarSyncPaginationAndDeliveryFailure(t *testing.T) {
	a := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("timeMin") != "" {
			t.Error("incremental sync cannot use timeMin")
		}
		if q.Get("pageToken") == "" {
			reply(w, `{"items":[{"id":"one","etag":"v1"}],"nextPageToken":"second"}`)
		} else {
			reply(w, `{"items":[{"id":"gone","etag":"v2","status":"cancelled"}],"nextSyncToken":"next"}`)
		}
	})
	cred := Cred{WatchCalendarID: "primary"}
	cur, e := a.poll(context.Background(), cred, "", func(string, *EventChangedEvent) error { t.Fatal("historical baseline event"); return nil })
	if e != nil || cur == "" {
		t.Fatalf("baseline %v", e)
	}
	n := 0
	next, e := a.poll(context.Background(), cred, cur, func(_ string, v *EventChangedEvent) error { n++; return nil })
	if e != nil || next == "" || n != 2 {
		t.Fatalf("changes %d %v", n, e)
	}
	if _, e = a.poll(context.Background(), cred, cur, func(string, *EventChangedEvent) error { return errors.New("delivery failed") }); e == nil {
		t.Fatal("delivery failure lost")
	}
}
func TestCalendarPatchClearsOppositeTimeRepresentation(t *testing.T) {
	a := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		if json.NewDecoder(r.Body).Decode(&b) != nil {
			t.Fatal("body")
		}
		start := b["start"].(map[string]any)
		if date, exists := start["date"]; !exists || date != nil {
			t.Error("timed patch must explicitly clear previous all-day date")
		}
		reply(w, `{"id":"evt"}`)
	})
	_, e := a.save(context.Background(), "evt", &CreateEventIn{Summary: "timed", Start: "2026-11-03T01:00:00Z", End: "2026-11-03T02:00:00Z"})
	if e != nil {
		t.Fatal(e)
	}
}
