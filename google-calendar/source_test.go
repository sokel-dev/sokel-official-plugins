package main

import (
	"context"
	"errors"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"io"
	"net/http"
	"testing"
	"time"
)

type sourceFixture struct {
	plugin.Ctx
	fields  map[string]string
	patches []map[string]string
	fail    bool
	events  int
}

func (s *sourceFixture) Credential() map[string]string { return s.fields }
func (s *sourceFixture) UpdateCredential(p map[string]string) error {
	s.patches = append(s.patches, p)
	if s.fail {
		return errors.New("publish failed")
	}
	return nil
}
func (s *sourceFixture) Trigger(string, string, any) error { s.events++; return nil }
func (s *sourceFixture) ReportStatus(string, string)       {}
func TestSourcePublishesCheckpointOnlyAfterSuccessfulRound(t *testing.T) {
	a := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) { reply(w, `{"nextSyncToken":"next"}`) })
	s := &sourceFixture{Ctx: contextOnly{Context: context.Background()}, fields: map[string]string{"watch_calendar_id": "primary", "access_token": "test-secret"}}
	next, e := sourceStep(s, a, "")
	if e != nil || next == "" || len(s.patches) != 1 || s.patches[0]["poll_cursor"] != next || s.events != 0 {
		t.Fatalf("initial checkpoint %q %v %+v", next, e, s.patches)
	}
	s.fail = true
	next, e = sourceStep(s, a, "")
	if e == nil || next != "" {
		t.Fatalf("failed publication advanced cursor: %q %v", next, e)
	}
}

type contextOnly struct{ context.Context }

func (contextOnly) Credential() map[string]string { return nil }
func (contextOnly) Fetch(*plugin.File) ([]byte, error) {
	return nil, errors.New("unexpected file fetch")
}
func (contextOnly) Upload(string, string, []byte) (*plugin.File, error) {
	return nil, errors.New("unexpected file upload")
}
func (contextOnly) UploadReader(string, string, io.Reader) (*plugin.File, error) {
	return nil, errors.New("unexpected stream upload")
}

func TestSourceCheckpointCarriesNextDeadline(t *testing.T) {
	a := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) { reply(w, `{"nextSyncToken":"start"}`) })
	s := &sourceFixture{Ctx: contextOnly{Context: context.Background()}, fields: map[string]string{"watch_calendar_id": "primary", "access_token": "test-secret"}}
	before := time.Now().Add(60 * time.Second)
	_, err := sourceStep(s, a, "")
	if err != nil {
		t.Fatal(err)
	}
	next, err := time.Parse(time.RFC3339Nano, s.patches[0]["next_poll_at"])
	if err != nil || next.Before(before) || next.After(time.Now().Add(60*time.Second)) {
		t.Fatalf("checkpoint lacks a restart-safe deadline: %q %v", s.patches[0]["next_poll_at"], err)
	}
}
func TestSourceRestartWaitsForCheckpointDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	s := &sourceFixture{Ctx: contextOnly{Context: ctx}, fields: map[string]string{"watch_calendar_id": "primary", "access_token": "test-secret"}}
	s.fields["next_poll_at"] = time.Now().Add(time.Minute).Format(time.RFC3339Nano)
	calls := 0
	a := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) { calls++; reply(w, `{"nextSyncToken":"start"}`) })
	err := runSourceUsing(s, func(string) *api { return a })
	if !errors.Is(err, context.DeadlineExceeded) || calls != 0 || len(s.patches) != 0 {
		t.Fatalf("restarted source polled early: calls=%d patches=%d err=%v", calls, len(s.patches), err)
	}
}

func TestSourceDeadlineValidation(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		raw  string
		want time.Duration
		bad  bool
	}{
		{"", 0, false}, {now.Add(-time.Second).Format(time.RFC3339Nano), 0, false},
		{now.Add(30 * time.Second).Format(time.RFC3339Nano), 30 * time.Second, false},
		{now.Add(time.Hour).Format(time.RFC3339Nano), time.Minute, false}, {"broken", 0, true},
	} {
		got, err := pollDelay(tc.raw, now)
		if got != tc.want || (err != nil) != tc.bad {
			t.Fatalf("pollDelay(%q) = %v %v", tc.raw, got, err)
		}
	}
}
