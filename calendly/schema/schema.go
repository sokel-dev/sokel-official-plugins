package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type User struct {
	URI             string `json:"uri" sokel:"uri" label:"User URI"`
	Name            string `json:"name" sokel:"name" label:"Name"`
	Email           string `json:"email" sokel:"email" label:"Email"`
	Timezone        string `json:"timezone" sokel:"timezone" label:"Timezone"`
	OrganizationURI string `json:"current_organization" sokel:"organization_uri" label:"Organization URI"`
	SchedulingURL   string `json:"scheduling_url" sokel:"scheduling_url" label:"Scheduling URL"`
}
type EventType struct {
	URI           string  `json:"uri" sokel:"uri" label:"Event type URI"`
	Name          string  `json:"name" sokel:"name" label:"Name"`
	Active        bool    `json:"active" sokel:"active" label:"Active"`
	Duration      int     `json:"duration" sokel:"duration" label:"Duration in minutes"`
	Kind          string  `json:"kind" sokel:"kind" label:"Event kind"`
	SchedulingURL string  `json:"scheduling_url" sokel:"scheduling_url" label:"Scheduling URL"`
	Description   *string `json:"description_plain,omitempty" sokel:"description,optional" label:"Description"`
}
type Event struct {
	URI          string         `json:"uri" sokel:"uri" label:"Event URI"`
	Name         string         `json:"name" sokel:"name" label:"Event name"`
	Status       string         `json:"status" sokel:"status" label:"Status"`
	StartTime    string         `json:"start_time" sokel:"start_time" label:"Start time"`
	EndTime      string         `json:"end_time" sokel:"end_time" label:"End time"`
	EventTypeURI string         `json:"event_type" sokel:"event_type_uri" label:"Event type URI"`
	Location     map[string]any `json:"location,omitempty" sokel:"location,optional" label:"Meeting location" opaque:"Location is a kind-dependent union of physical, phone and conferencing details."`
}
type QuestionAnswer struct {
	Question string `json:"question" sokel:"question" label:"Question"`
	Answer   string `json:"answer" sokel:"answer" label:"Answer"`
	Position int    `json:"position" sokel:"position" label:"Question position"`
}
type Invitee struct {
	URI                 string           `json:"uri" sokel:"uri" label:"Invitee URI"`
	EventURI            string           `json:"event" sokel:"event_uri" label:"Event URI"`
	Name                string           `json:"name" sokel:"name" label:"Name"`
	Email               string           `json:"email" sokel:"email" label:"Email"`
	Status              string           `json:"status" sokel:"status" label:"Status"`
	Timezone            *string          `json:"timezone,omitempty" sokel:"timezone,optional" label:"Timezone"`
	QuestionsAndAnswers []QuestionAnswer `json:"questions_and_answers" sokel:"questions_and_answers" label:"Questions and answers"`
	Rescheduled         bool             `json:"rescheduled" sokel:"rescheduled" label:"Rescheduled"`
	CreatedAt           string           `json:"created_at" sokel:"created_at" label:"Created at"`
}
type AvailableTime struct {
	Status            string `json:"status" sokel:"status" label:"Status"`
	StartTime         string `json:"start_time" sokel:"start_time" label:"Start time"`
	InviteesRemaining int    `json:"invitees_remaining" sokel:"invitees_remaining" label:"Remaining invitee slots"`
	SchedulingURL     string `json:"scheduling_url" sokel:"scheduling_url" label:"Scheduling URL"`
}
type PageOptions struct {
	Count *int    `json:"count,omitempty" sokel:"count,optional" label:"Page size"`
	Token *string `json:"page_token,omitempty" sokel:"token,optional" label:"Page token"`
}
type EventTypeFilters struct {
	Active *bool `json:"active,omitempty" sokel:"active,optional" label:"Active only"`
}
type EventFilters struct {
	Status       *string `json:"status,omitempty" sokel:"status,optional" label:"Event status"`
	MinStartTime *string `json:"min_start_time,omitempty" sokel:"min_start_time,optional" label:"Earliest start time"`
	MaxStartTime *string `json:"max_start_time,omitempty" sokel:"max_start_time,optional" label:"Latest start time"`
	InviteeEmail *string `json:"invitee_email,omitempty" sokel:"invitee_email,optional" label:"Invitee email"`
}
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check credentials", TimeoutSec: 30}
}
func (HealthCheck) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Usable"), field.String("message").Label("Check details")}
}

type GetCurrentUser struct{}

func (GetCurrentUser) Meta() contract.Meta {
	return contract.Meta{ID: "get_current_user", Label: "Get current user", TimeoutSec: 60}
}
func (GetCurrentUser) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (GetCurrentUser) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("user", User{}).Label("User")}
}

type ListEventTypes struct{}

func (ListEventTypes) Meta() contract.Meta {
	return contract.Meta{ID: "list_event_types", Label: "List event types", TimeoutSec: 60}
}
func (ListEventTypes) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("user_uri").Label("User URI").Optional(), field.String("organization_uri").Label("Organization URI").Optional(), field.Json("filters", EventTypeFilters{}).Label("Event type filters").Optional(), field.Json("page", PageOptions{}).Label("Pagination").Optional()}
}
func (ListEventTypes) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("event_types", []EventType{}).Label("Event types"), field.String("next_page_token").Label("Next page token")}
}

type GetEventType struct{}

func (GetEventType) Meta() contract.Meta {
	return contract.Meta{ID: "get_event_type", Label: "Get event type", TimeoutSec: 60}
}
func (GetEventType) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("event_type_id").Label("Event type ID")}
}
func (GetEventType) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("event_type", EventType{}).Label("Event type")}
}

type ListEvents struct{}

func (ListEvents) Meta() contract.Meta {
	return contract.Meta{ID: "list_events", Label: "List scheduled events", TimeoutSec: 60}
}
func (ListEvents) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("user_uri").Label("User URI").Optional(), field.String("organization_uri").Label("Organization URI").Optional(), field.Json("filters", EventFilters{}).Label("Event filters").Optional(), field.Json("page", PageOptions{}).Label("Pagination").Optional()}
}
func (ListEvents) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("events", []Event{}).Label("Scheduled events"), field.String("next_page_token").Label("Next page token")}
}

type GetEvent struct{}

func (GetEvent) Meta() contract.Meta {
	return contract.Meta{ID: "get_event", Label: "Get scheduled event", TimeoutSec: 60}
}
func (GetEvent) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("event_id").Label("Event ID")}
}
func (GetEvent) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("event", Event{}).Label("Scheduled event")}
}

type ListInvitees struct{}

func (ListInvitees) Meta() contract.Meta {
	return contract.Meta{ID: "list_invitees", Label: "List event invitees", TimeoutSec: 60}
}
func (ListInvitees) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("event_id").Label("Event ID"), field.String("status").Label("Invitee status").Optional(), field.Json("page", PageOptions{}).Label("Pagination").Optional()}
}
func (ListInvitees) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("invitees", []Invitee{}).Label("Invitees"), field.String("next_page_token").Label("Next page token")}
}

type ListAvailableTimes struct{}

func (ListAvailableTimes) Meta() contract.Meta {
	return contract.Meta{ID: "list_available_times", Label: "List available times", TimeoutSec: 60}
}
func (ListAvailableTimes) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("event_type_id").Label("Event type ID"), field.String("start_time").Label("Start time"), field.String("end_time").Label("End time")}
}
func (ListAvailableTimes) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("times", []AvailableTime{}).Label("Available times")}
}

type CreateSchedulingLink struct{}

func (CreateSchedulingLink) Meta() contract.Meta {
	return contract.Meta{ID: "create_scheduling_link", Label: "Create single-use scheduling link", TimeoutSec: 60}
}
func (CreateSchedulingLink) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("event_type_id").Label("Event type ID")}
}
func (CreateSchedulingLink) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("booking_url").Label("Single-use booking URL"), field.String("event_type_uri").Label("Event type URI")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("access_token").Label("Access token").Desc("Use a Calendly personal access token. Refresh or replace it outside this plugin.")}
}
