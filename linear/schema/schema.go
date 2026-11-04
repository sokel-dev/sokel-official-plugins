package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type Named struct {
	ID   string `json:"id" sokel:"id" label:"ID"`
	Name string `json:"name" sokel:"name" label:"Name"`
}
type Team struct {
	ID   string `json:"id" sokel:"id" label:"Team ID"`
	Key  string `json:"key" sokel:"key" label:"Team key"`
	Name string `json:"name" sokel:"name" label:"Name"`
}
type Issue struct {
	ID          string `json:"id" sokel:"id" label:"Issue ID"`
	Identifier  string `json:"identifier" sokel:"identifier" label:"Identifier"`
	Title       string `json:"title" sokel:"title" label:"Title"`
	Description string `json:"description" sokel:"description" label:"Markdown description"`
	URL         string `json:"url" sokel:"url" label:"URL"`
	Priority    int    `json:"priority" sokel:"priority" label:"Priority"`
	DueDate     string `json:"dueDate" sokel:"due_date" label:"Due date"`
	UpdatedAt   string `json:"updatedAt" sokel:"updated_at" label:"Updated at"`
	Team        Team   `json:"team" sokel:"team" label:"Team"`
	State       Named  `json:"state" sokel:"state" label:"State"`
	Assignee    *Named `json:"assignee" sokel:"assignee,optional" label:"Assignee"`
	Project     *Named `json:"project" sokel:"project,optional" label:"Project"`
}
type Project struct {
	ID          string `json:"id" sokel:"id" label:"Project ID"`
	Name        string `json:"name" sokel:"name" label:"Name"`
	Description string `json:"description" sokel:"description" label:"Description"`
	URL         string `json:"url" sokel:"url" label:"URL"`
	UpdatedAt   string `json:"updatedAt" sokel:"updated_at" label:"Updated at"`
	TargetDate  string `json:"targetDate" sokel:"target_date" label:"Target date"`
}
type IssueChanges struct {
	Title       *string `json:"title,omitempty" sokel:"title,optional" label:"Title"`
	Description *string `json:"description,omitempty" sokel:"description,optional" label:"Markdown description"`
	StateID     *string `json:"stateId,omitempty" sokel:"state_id,optional" label:"State ID"`
	AssigneeID  *string `json:"assigneeId,omitempty" sokel:"assignee_id,optional" label:"Assignee ID"`
	ProjectID   *string `json:"projectId,omitempty" sokel:"project_id,optional" label:"Project ID"`
	Priority    *int    `json:"priority,omitempty" sokel:"priority,optional" label:"Priority"`
	DueDate     *string `json:"dueDate,omitempty" sokel:"due_date,optional" label:"Due date"`
}

type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check credential", TimeoutSec: 60}
}
func (HealthCheck) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Available"), field.String("message").Label("Message")}
}

type ListTeams struct{}

func (ListTeams) Meta() contract.Meta {
	return contract.Meta{ID: "list_teams", Label: "List teams", TimeoutSec: 60}
}
func (ListTeams) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Int("limit").Label("Page size").Optional().Desc("1 to 100; defaults to 50"), field.String("cursor").Label("Pagination cursor").Optional()}
}
func (ListTeams) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("teams", []Team{}).Label("Teams"), field.String("next_cursor").Label("Next cursor"), field.Bool("has_more").Label("More results")}
}

type ListIssues struct{}

func (ListIssues) Meta() contract.Meta {
	return contract.Meta{ID: "list_issues", Label: "List issues", TimeoutSec: 60}
}
func (ListIssues) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Int("limit").Label("Page size").Optional().Desc("1 to 100; defaults to 50"), field.String("cursor").Label("Pagination cursor").Optional(), field.String("team_id").Label("Team ID").Optional(), field.String("project_id").Label("Project ID").Optional(), field.String("assignee_id").Label("Assignee ID").Optional(), field.String("state_id").Label("State ID").Optional(), field.String("title_contains").Label("Title contains").Optional()}
}
func (ListIssues) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("issues", []Issue{}).Label("Issues"), field.String("next_cursor").Label("Next cursor"), field.Bool("has_more").Label("More results")}
}

type GetIssue struct{}

func (GetIssue) Meta() contract.Meta {
	return contract.Meta{ID: "get_issue", Label: "Get issue", TimeoutSec: 60}
}
func (GetIssue) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("issue_id").Label("Issue ID or identifier")}
}
func (GetIssue) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("issue", Issue{}).Label("Issue")}
}

type ListProjects struct{}

func (ListProjects) Meta() contract.Meta {
	return contract.Meta{ID: "list_projects", Label: "List projects", TimeoutSec: 60}
}
func (ListProjects) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Int("limit").Label("Page size").Optional().Desc("1 to 100; defaults to 50"), field.String("cursor").Label("Pagination cursor").Optional(), field.String("name_contains").Label("Name contains").Optional()}
}
func (ListProjects) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("projects", []Project{}).Label("Projects"), field.String("next_cursor").Label("Next cursor"), field.Bool("has_more").Label("More results")}
}

type CreateIssue struct{}

func (CreateIssue) Meta() contract.Meta {
	return contract.Meta{ID: "create_issue", Label: "Create issue", TimeoutSec: 60}
}
func (CreateIssue) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("team_id").Label("Team ID"), field.String("title").Label("Title"), field.Json("details", IssueChanges{}).Optional().Label("Optional issue details")}
}
func (CreateIssue) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("issue", Issue{}).Label("Issue")}
}

type UpdateIssue struct{}

func (UpdateIssue) Meta() contract.Meta {
	return contract.Meta{ID: "update_issue", Label: "Update issue", TimeoutSec: 60}
}
func (UpdateIssue) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("issue_id").Label("Issue ID or identifier"), field.Json("changes", IssueChanges{}).Label("Changed fields")}
}
func (UpdateIssue) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("issue", Issue{}).Label("Issue")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("api_key").Label("Personal API key")}
}
