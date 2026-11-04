package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type Issue struct {
	ID                string   `json:"id" sokel:"id" label:"Issue ID"`
	Key               string   `json:"key" sokel:"key" label:"Issue key"`
	Summary           string   `json:"summary" sokel:"summary" label:"Summary"`
	DescriptionADF    string   `json:"description_adf" sokel:"description_adf" label:"Description ADF JSON"`
	Status            string   `json:"status" sokel:"status" label:"Status"`
	StatusID          string   `json:"status_id" sokel:"status_id" label:"Status ID"`
	AssigneeAccountID string   `json:"assignee_account_id" sokel:"assignee_account_id" label:"Assignee account ID"`
	Labels            []string `json:"labels" sokel:"labels" label:"Labels"`
	Updated           string   `json:"updated" sokel:"updated" label:"Updated at"`
	DueDate           string   `json:"due_date" sokel:"due_date" label:"Due date"`
	PriorityID        string   `json:"priority_id" sokel:"priority_id" label:"Priority ID"`
}
type IssueChanges struct {
	Summary           *string  `json:"summary,omitempty" sokel:"summary,optional" label:"Summary"`
	Description       *string  `json:"description,omitempty" sokel:"description,optional" label:"Plain text description"`
	AssigneeAccountID *string  `json:"assignee_account_id,omitempty" sokel:"assignee_account_id,optional" label:"Assignee account ID"`
	Labels            []string `json:"labels,omitempty" sokel:"labels,optional" label:"Labels"`
	DueDate           *string  `json:"due_date,omitempty" sokel:"due_date,optional" label:"Due date"`
	PriorityID        *string  `json:"priority_id,omitempty" sokel:"priority_id,optional" label:"Priority ID"`
}
type Transition struct {
	ID         string `json:"id" sokel:"id" label:"Transition ID"`
	Name       string `json:"name" sokel:"name" label:"Name"`
	ToID       string `json:"to_id" sokel:"to_id" label:"Destination status ID"`
	ToName     string `json:"to_name" sokel:"to_name" label:"Destination status"`
	FieldsJSON string `json:"fields_json" sokel:"fields_json" label:"Required field metadata JSON"`
}
type Comment struct {
	ID              string `json:"id" sokel:"id" label:"Comment ID"`
	BodyADF         string `json:"body_adf" sokel:"body_adf" label:"Comment ADF JSON"`
	AuthorAccountID string `json:"author_account_id" sokel:"author_account_id" label:"Author account ID"`
	Created         string `json:"created" sokel:"created" label:"Created at"`
	Updated         string `json:"updated" sokel:"updated" label:"Updated at"`
}

type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check credential", TimeoutSec: 60}
}
func (HealthCheck) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Available"), field.String("message").Label("Message")}
}

type SearchIssues struct{}

func (SearchIssues) Meta() contract.Meta {
	return contract.Meta{ID: "search_issues", Label: "Search issues with JQL", TimeoutSec: 60}
}
func (SearchIssues) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("jql").Label("JQL query"), field.Int("limit").Label("Page size").Optional().Desc("1 to 100; defaults to 50"), field.String("next_page_token").Label("Pagination token").Optional()}
}
func (SearchIssues) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("issues", []Issue{}).Label("Issues"), field.String("next_page_token").Label("Next page token"), field.Bool("is_last").Label("Last page")}
}

type GetIssue struct{}

func (GetIssue) Meta() contract.Meta {
	return contract.Meta{ID: "get_issue", Label: "Get issue", TimeoutSec: 60}
}
func (GetIssue) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("issue_key").Label("Issue key or ID")}
}
func (GetIssue) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("issue", Issue{}).Label("Issue")}
}

type CreateIssue struct{}

func (CreateIssue) Meta() contract.Meta {
	return contract.Meta{ID: "create_issue", Label: "Create issue", TimeoutSec: 60}
}
func (CreateIssue) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("project_key").Label("Project key"), field.String("issue_type_id").Label("Issue type ID"), field.String("summary").Label("Summary"), field.Json("details", IssueChanges{}).Optional().Label("Optional issue details"), field.Object("custom_fields", "Custom field IDs and value schemas are configured independently by each Jira project").Label("Custom field values").Optional()}
}
func (CreateIssue) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("id").Label("Issue ID"), field.String("key").Label("Issue key")}
}

type UpdateIssue struct{}

func (UpdateIssue) Meta() contract.Meta {
	return contract.Meta{ID: "update_issue", Label: "Update issue", TimeoutSec: 60}
}
func (UpdateIssue) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("issue_key").Label("Issue key or ID"), field.Json("changes", IssueChanges{}).Label("Changed fields"), field.Object("custom_fields", "Custom field IDs and value schemas are configured independently by each Jira project").Label("Custom field values").Optional()}
}
func (UpdateIssue) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Updated")}
}

type ListTransitions struct{}

func (ListTransitions) Meta() contract.Meta {
	return contract.Meta{ID: "list_transitions", Label: "List available transitions", TimeoutSec: 60}
}
func (ListTransitions) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("issue_key").Label("Issue key or ID")}
}
func (ListTransitions) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("transitions", []Transition{}).Label("Transitions")}
}

type TransitionIssue struct{}

func (TransitionIssue) Meta() contract.Meta {
	return contract.Meta{ID: "transition_issue", Label: "Transition issue", TimeoutSec: 60}
}
func (TransitionIssue) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("issue_key").Label("Issue key or ID"), field.String("transition_id").Label("Transition ID"), field.Object("fields", "Fields required by the selected transition depend on Jira project configuration").Label("Transition field values").Optional()}
}
func (TransitionIssue) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Transition applied")}
}

type ListComments struct{}

func (ListComments) Meta() contract.Meta {
	return contract.Meta{ID: "list_comments", Label: "List comments", TimeoutSec: 60}
}
func (ListComments) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("issue_key").Label("Issue key or ID"), field.Int("start_at").Label("Start offset").Optional(), field.Int("limit").Label("Page size").Optional().Desc("1 to 100; defaults to 50")}
}
func (ListComments) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("comments", []Comment{}).Label("Comments"), field.Int("total").Label("Total comments"), field.Int("next_start_at").Label("Next offset"), field.Bool("has_more").Label("More results")}
}

type AddComment struct{}

func (AddComment) Meta() contract.Meta {
	return contract.Meta{ID: "add_comment", Label: "Add comment", TimeoutSec: 60}
}
func (AddComment) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("issue_key").Label("Issue key or ID"), field.String("text").Label("Plain text comment")}
}
func (AddComment) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("comment", Comment{}).Label("Comment")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Text("site").Label("Atlassian Cloud site").Optional().Desc("For tokens without scopes: https://your-site.atlassian.net"), field.Text("cloud_id").Label("Cloud ID for scoped tokens").Optional().Desc("Scoped tokens use the fixed api.atlassian.com gateway; takes precedence over site"), field.Text("email").Label("Atlassian account email"), field.Secret("api_token").Label("Atlassian API token")}
}
