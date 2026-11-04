package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type User struct {
	ID          string `json:"id" sokel:"id" label:"User ID"`
	Email       string `json:"email" sokel:"email" label:"Email"`
	FirstName   string `json:"first_name" sokel:"first_name" label:"First name"`
	LastName    string `json:"last_name" sokel:"last_name" label:"Last name"`
	DisplayName string `json:"display_name" sokel:"display_name" label:"Display name"`
	Type        int    `json:"type" sokel:"type" label:"User type"`
	Status      string `json:"status" sokel:"status" label:"Status"`
}
type Meeting struct {
	ID        string `json:"id" sokel:"id" label:"Meeting ID"`
	UUID      string `json:"uuid" sokel:"uuid" label:"Meeting UUID"`
	HostID    string `json:"host_id" sokel:"host_id" label:"Host ID"`
	Topic     string `json:"topic" sokel:"topic" label:"Topic"`
	Agenda    string `json:"agenda" sokel:"agenda" label:"Agenda"`
	Type      int    `json:"type" sokel:"type" label:"Meeting type"`
	StartTime string `json:"start_time" sokel:"start_time" label:"Start time"`
	Duration  int    `json:"duration" sokel:"duration" label:"Duration in minutes"`
	Timezone  string `json:"timezone" sokel:"timezone" label:"Timezone"`
	JoinURL   string `json:"join_url" sokel:"join_url" label:"Join URL"`
	CreatedAt string `json:"created_at" sokel:"created_at" label:"Created at"`
}
type Settings struct {
	HostVideo        *bool `json:"host_video,omitempty" sokel:"host_video,optional" label:"Host video"`
	ParticipantVideo *bool `json:"participant_video,omitempty" sokel:"participant_video,optional" label:"Participant video"`
	JoinBeforeHost   *bool `json:"join_before_host,omitempty" sokel:"join_before_host,optional" label:"Join before host"`
	MuteUponEntry    *bool `json:"mute_upon_entry,omitempty" sokel:"mute_upon_entry,optional" label:"Mute upon entry"`
	WaitingRoom      *bool `json:"waiting_room,omitempty" sokel:"waiting_room,optional" label:"Waiting room"`
}
type CreateDetails struct {
	Agenda   *string   `json:"agenda,omitempty" sokel:"agenda,optional" label:"Agenda"`
	Settings *Settings `json:"settings,omitempty" sokel:"settings,optional" label:"Meeting settings"`
}
type MeetingChanges struct {
	Topic     *string   `json:"topic,omitempty" sokel:"topic,optional" label:"Topic"`
	Agenda    *string   `json:"agenda,omitempty" sokel:"agenda,optional" label:"Agenda"`
	StartTime *string   `json:"start_time,omitempty" sokel:"start_time,optional" label:"Start time"`
	Duration  *int      `json:"duration,omitempty" sokel:"duration,optional" label:"Duration in minutes"`
	Settings  *Settings `json:"settings,omitempty" sokel:"settings,optional" label:"Meeting settings"`
}
type GetUser struct{}

func (GetUser) Meta() contract.Meta {
	return contract.Meta{ID: "get_user", Label: "Get user", TimeoutSec: 60}
}
func (GetUser) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("user_id").Label("User ID or email").Optional()}
}
func (GetUser) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("user", User{}).Label("User")}
}

type ListUsers struct{}

func (ListUsers) Meta() contract.Meta {
	return contract.Meta{ID: "list_users", Label: "List users", TimeoutSec: 60}
}
func (ListUsers) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Int("page_size").Label("Page size").Optional(), field.String("next_page_token").Label("Pagination token").Optional(), field.String("status").Label("User status").Optional()}
}
func (ListUsers) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("users", []User{}).Label("Users"), field.String("next_page_token").Label("Next page token"), field.Bool("has_more").Label("More results")}
}

type ListMeetings struct{}

func (ListMeetings) Meta() contract.Meta {
	return contract.Meta{ID: "list_meetings", Label: "List meetings", TimeoutSec: 60}
}
func (ListMeetings) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("user_id").Label("User ID or email").Optional(), field.Int("page_size").Label("Page size").Optional(), field.String("next_page_token").Label("Pagination token").Optional(), field.String("type").Label("Meeting list type").Optional()}
}
func (ListMeetings) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("meetings", []Meeting{}).Label("Meetings"), field.String("next_page_token").Label("Next page token"), field.Bool("has_more").Label("More results")}
}

type GetMeeting struct{}

func (GetMeeting) Meta() contract.Meta {
	return contract.Meta{ID: "get_meeting", Label: "Get meeting", TimeoutSec: 60}
}
func (GetMeeting) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("meeting_id").Label("Meeting ID")}
}
func (GetMeeting) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("meeting", Meeting{}).Label("Meeting")}
}

type CreateMeeting struct{}

func (CreateMeeting) Meta() contract.Meta {
	return contract.Meta{ID: "create_meeting", Label: "Create scheduled meeting", TimeoutSec: 60}
}
func (CreateMeeting) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("user_id").Label("User ID or email").Optional(), field.String("topic").Label("Topic"), field.String("start_time").Label("Start time"), field.Int("duration").Label("Duration in minutes"), field.Json("details", CreateDetails{}).Label("Meeting details").Optional()}
}
func (CreateMeeting) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("meeting", Meeting{}).Label("Meeting")}
}

type UpdateMeeting struct{}

func (UpdateMeeting) Meta() contract.Meta {
	return contract.Meta{ID: "update_meeting", Label: "Update meeting", TimeoutSec: 60}
}
func (UpdateMeeting) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("meeting_id").Label("Meeting ID"), field.Json("changes", MeetingChanges{}).Label("Changed fields")}
}
func (UpdateMeeting) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("updated").Label("Updated")}
}

type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check credential", TimeoutSec: 60}
}
func (HealthCheck) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Available"), field.String("message").Label("Message")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("access_token").Label("OAuth access token")}
}
