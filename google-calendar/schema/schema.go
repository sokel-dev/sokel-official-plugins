// Package schema declares the public Google integration contract.
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/auth"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type EventTime struct {
	Date     string `json:"date,omitempty" sokel:"date,optional" label:"All-day date"`
	DateTime string `json:"dateTime,omitempty" sokel:"date_time,optional" label:"Date and time with offset"`
	TimeZone string `json:"timeZone,omitempty" sokel:"time_zone,optional" label:"Time zone"`
}
type Attendee struct {
	Email          string `json:"email" sokel:"email" label:"Email"`
	ResponseStatus string `json:"responseStatus,omitempty" sokel:"response_status,optional" label:"Response status"`
}
type CalendarEvent struct {
	ID          string     `json:"id" sokel:"id" label:"Event ID"`
	Status      string     `json:"status" sokel:"status" label:"Status"`
	Summary     string     `json:"summary" sokel:"summary" label:"Title"`
	Description string     `json:"description" sokel:"description" label:"Description"`
	Location    string     `json:"location" sokel:"location" label:"Location"`
	Start       EventTime  `json:"start" sokel:"start" label:"Start"`
	End         EventTime  `json:"end" sokel:"end" label:"End"`
	Updated     string     `json:"updated" sokel:"updated" label:"Updated time"`
	ETag        string     `json:"etag" sokel:"etag" label:"ETag"`
	HTMLLink    string     `json:"htmlLink" sokel:"html_link" label:"Web view link"`
	Attendees   []Attendee `json:"attendees" sokel:"attendees" label:"Attendees"`
}
type BusyPeriod struct {
	Start string `json:"start" sokel:"start" label:"Start"`
	End   string `json:"end" sokel:"end" label:"End"`
}
type FreeBusyCalendar struct {
	CalendarID string       `sokel:"calendar_id" label:"Calendar ID"`
	Busy       []BusyPeriod `sokel:"busy" label:"Busy periods"`
	Errors     []string     `sokel:"errors" label:"Upstream error codes"`
}
type ListEvents struct{}

func (ListEvents) Meta() contract.Meta {
	return contract.Meta{ID: "list_events", Label: "List events", TimeoutSec: 60}
}
func (ListEvents) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("calendar_id").Label("Calendar ID").Optional().Desc("Leave empty to use primary"), field.String("time_min").Label("Start time").Optional(), field.String("time_max").Label("End time").Optional(), field.String("query").Label("Search query").Optional(), field.String("page_token").Label("Page token").Optional(), field.Int("page_size").Label("Page size").Optional()}
}
func (ListEvents) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("events", []CalendarEvent{}).Label("Events"), field.String("next_page_token").Label("Next page token").Optional()}
}

type GetEvent struct{}

func (GetEvent) Meta() contract.Meta {
	return contract.Meta{ID: "get_event", Label: "Get event", TimeoutSec: 60}
}
func (GetEvent) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("calendar_id").Label("Calendar ID").Optional().Desc("Leave empty to use primary"), field.String("event_id").Label("Event ID")}
}
func (GetEvent) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("event", CalendarEvent{}).Label("Event")}
}

type CreateEvent struct{}

func (CreateEvent) Meta() contract.Meta {
	return contract.Meta{ID: "create_event", Label: "Create event", TimeoutSec: 60}
}
func (CreateEvent) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("calendar_id").Label("Calendar ID").Optional().Desc("Leave empty to use primary"), field.String("summary").Label("Title"), field.String("description").Label("Description").Optional(), field.String("location").Label("Location").Optional(), field.String("start").Label("Start time or date"), field.String("end").Label("End time or date"), field.Bool("all_day").Label("All-day event").Optional(), field.String("time_zone").Label("IANA time zone").Optional(), field.Array("attendees", []string{}).Label("Attendee emails").Optional(), field.String("send_updates").Label("Invitation notifications").Optional().Desc("none, all or externalOnly; defaults to none")}
}
func (CreateEvent) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("event", CalendarEvent{}).Label("Event")}
}

type UpdateEvent struct{}

func (UpdateEvent) Meta() contract.Meta {
	return contract.Meta{ID: "update_event", Label: "Update event", TimeoutSec: 60}
}
func (UpdateEvent) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("calendar_id").Label("Calendar ID").Optional().Desc("Leave empty to use primary"), field.String("event_id").Label("Event ID"), field.String("summary").Label("Title"), field.String("description").Label("Description").Optional(), field.String("location").Label("Location").Optional(), field.String("start").Label("Start time or date"), field.String("end").Label("End time or date"), field.Bool("all_day").Label("All-day event").Optional(), field.String("time_zone").Label("IANA time zone").Optional(), field.Array("attendees", []string{}).Label("Attendee emails").Optional(), field.String("send_updates").Label("Invitation notifications").Optional().Desc("none, all or externalOnly; defaults to none")}
}
func (UpdateEvent) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("event", CalendarEvent{}).Label("Event")}
}

type DeleteEvent struct{}

func (DeleteEvent) Meta() contract.Meta {
	return contract.Meta{ID: "delete_event", Label: "Delete event", TimeoutSec: 60}
}
func (DeleteEvent) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("calendar_id").Label("Calendar ID").Optional().Desc("Leave empty to use primary"), field.String("event_id").Label("Event ID"), field.String("send_updates").Label("Invitation notifications").Optional()}
}
func (DeleteEvent) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Success")}
}

type FreeBusy struct{}

func (FreeBusy) Meta() contract.Meta {
	return contract.Meta{ID: "free_busy", Label: "Query free/busy", TimeoutSec: 60}
}
func (FreeBusy) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("time_min").Label("Start time"), field.String("time_max").Label("End time"), field.Array("calendar_ids", []string{}).Label("Calendar IDs")}
}
func (FreeBusy) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("calendars", []FreeBusyCalendar{}).Label("Calendar availability")}
}

type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check credential", TimeoutSec: 30}
}
func (HealthCheck) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Available"), field.String("message").Label("Message")}
}

type Credential struct{}

func (Credential) AuthMeta() contract.AuthMeta {
	return auth.OAuth("google", "https://www.googleapis.com/auth/calendar.events", "https://www.googleapis.com/auth/calendar.freebusy", "https://www.googleapis.com/auth/userinfo.email")
}
func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("access_token").Label("Access token (injected by platform)").Optional(), field.Secret("refresh_token").Label("Refresh token (managed by platform)").Optional(), field.Text("poll_cursor").Label("Polling cursor (managed automatically)").Optional(), field.Text("next_poll_at").Label("Next poll time (managed automatically)").Optional(), field.Text("watch_calendar_id").Label("Watched calendar ID").Optional().Desc("Leave empty to disable polling; primary selects the main calendar")}
}

type EventChanged struct{}

func (EventChanged) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "event_changed", Label: "Event changed"}
}
func (EventChanged) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("calendar_id").Label("Calendar ID"), field.Json("event", CalendarEvent{}).Label("Event")}
}
