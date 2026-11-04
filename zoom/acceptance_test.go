package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"net/http"
	"strings"
	"testing"
)

func TestCreateMeetingBytes(t *testing.T) {
	var in CreateMeetingIn
	if e := bindTestInput(map[string]any{"topic": "Review", "start_time": "2030-01-01T10:00:00+08:00", "duration": 30, "details": map[string]any{"settings": map[string]any{"host_video": false, "waiting_room": true}}}, &in); e != nil {
		t.Fatal(e)
	}
	a, n := fakeAPI(t, "POST", "/v2/users/me/meetings", 201, fixture(t, "meeting"), func(r *http.Request, b []byte) {
		var x map[string]any
		if json.Unmarshal(b, &x) != nil {
			t.Fatal("JSON")
		}
		if x["start_time"] != "2030-01-01T02:00:00Z" || x["type"] != float64(2) || x["duration"] != float64(30) {
			t.Fatalf("body %s", b)
		}
		s := x["settings"].(map[string]any)
		if v, ok := s["host_video"]; !ok || v != false {
			t.Fatalf("false lost: %s", b)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatal("content type")
		}
	})
	o, e := a.createMeeting(context.Background(), &in)
	if e != nil || o == nil || o.Meeting.ID != "9007199254740993" || *n != 1 {
		t.Fatalf("meeting %+v %v calls=%d", o, e, *n)
	}
	b, _ := json.Marshal(o)
	if strings.Contains(string(b), "host-secret") || strings.Contains(string(b), "start_url") {
		t.Fatal("host secret exposed")
	}
}
func TestMeetingPagination(t *testing.T) {
	a, n := fakeAPI(t, "GET", "/v2/users/me/meetings", 200, fixture(t, "meetings"), func(r *http.Request, b []byte) {
		if r.URL.Query().Get("next_page_token") != "https://evil.invalid/?x=prior" {
			t.Fatal("cursor changed")
		}
	})
	o, e := a.listMeetings(context.Background(), &ListMeetingsIn{NextPageToken: "https://evil.invalid/?x=prior"})
	if e != nil || o == nil || !o.HasMore || *n != 1 {
		t.Fatalf("page %+v %v", o, e)
	}
}

func bindTestInput(v map[string]any, out any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	return contract.BindInput(b, out)
}
