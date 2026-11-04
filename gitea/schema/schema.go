package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type PageOptions struct {
	Page  *int `json:"page,omitempty" sokel:"page,optional" label:"Page number"`
	Limit *int `json:"limit,omitempty" sokel:"limit,optional" label:"Page size"`
}
type IssueOptions struct {
	Body      *string  `json:"body,omitempty" sokel:"body,optional" label:"Issue description"`
	Closed    *bool    `json:"closed,omitempty" sokel:"closed,optional" label:"Create closed"`
	Assignees []string `json:"assignees,omitempty" sokel:"assignees,optional" label:"Assignee usernames"`
	LabelIDs  []string `json:"label_ids,omitempty" sokel:"label_ids,optional" label:"Label IDs"`
}
type Repository struct {
	ID            string `json:"id" sokel:"id" label:"Repository ID"`
	Name          string `json:"name" sokel:"name" label:"Repository name"`
	FullName      string `json:"full_name" sokel:"full_name" label:"Full repository name"`
	Private       bool   `json:"private" sokel:"private" label:"Private repository"`
	DefaultBranch string `json:"default_branch" sokel:"default_branch" label:"Default branch"`
	HTMLURL       string `json:"html_url" sokel:"html_url" label:"Web URL"`
}
type Issue struct {
	ID            string `json:"id" sokel:"id" label:"Issue ID"`
	Number        string `json:"number" sokel:"number" label:"Issue number"`
	Title         string `json:"title" sokel:"title" label:"Title"`
	Body          string `json:"body" sokel:"body" label:"Description"`
	State         string `json:"state" sokel:"state" label:"State"`
	HTMLURL       string `json:"html_url" sokel:"html_url" label:"Web URL"`
	UpdatedAt     string `json:"updated_at" sokel:"updated_at" label:"Updated at"`
	IsPullRequest bool   `json:"is_pull_request" sokel:"is_pull_request" label:"Is a pull request"`
}
type PullRequest struct {
	ID      string `json:"id" sokel:"id" label:"Pull request ID"`
	Number  string `json:"number" sokel:"number" label:"Pull request number"`
	Title   string `json:"title" sokel:"title" label:"Title"`
	Body    string `json:"body" sokel:"body" label:"Description"`
	State   string `json:"state" sokel:"state" label:"State"`
	HTMLURL string `json:"html_url" sokel:"html_url" label:"Web URL"`
	Merged  bool   `json:"merged" sokel:"merged" label:"Merged"`
	Draft   bool   `json:"draft" sokel:"draft" label:"Draft"`
}
type Comment struct {
	ID      string `json:"id" sokel:"id" label:"Comment ID"`
	Body    string `json:"body" sokel:"body" label:"Comment text"`
	HTMLURL string `json:"html_url" sokel:"html_url" label:"Web URL"`
}

type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check token and service", TimeoutSec: 60}
}
func (HealthCheck) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Healthy"), field.String("message").Label("Check details")}
}

type ListRepositories struct{}

func (ListRepositories) Meta() contract.Meta {
	return contract.Meta{ID: "list_repositories", Label: "List accessible repositories", TimeoutSec: 60}
}
func (ListRepositories) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("pagination", PageOptions{}).Label("Pagination").Optional()}
}
func (ListRepositories) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("repositories", []Repository{}).Label("Repositories"), field.Int("next_page").Label("Next page; zero means finished")}
}

type GetRepository struct{}

func (GetRepository) Meta() contract.Meta {
	return contract.Meta{ID: "get_repository", Label: "Get repository", TimeoutSec: 60}
}
func (GetRepository) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("owner").Label("Repository owner"), field.String("repository").Label("Repository name")}
}
func (GetRepository) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("repository", Repository{}).Label("Repository")}
}

type ListIssues struct{}

func (ListIssues) Meta() contract.Meta {
	return contract.Meta{ID: "list_issues", Label: "List issues", TimeoutSec: 60}
}
func (ListIssues) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("owner").Label("Repository owner"), field.String("repository").Label("Repository name"), field.String("state").Label("State: open, closed or all").Optional(), field.Json("pagination", PageOptions{}).Label("Pagination").Optional()}
}
func (ListIssues) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("issues", []Issue{}).Label("Issues"), field.Int("next_page").Label("Next page; zero means finished")}
}

type GetIssue struct{}

func (GetIssue) Meta() contract.Meta {
	return contract.Meta{ID: "get_issue", Label: "Get issue", TimeoutSec: 60}
}
func (GetIssue) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("owner").Label("Repository owner"), field.String("repository").Label("Repository name"), field.String("number").Label("Issue number")}
}
func (GetIssue) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("issue", Issue{}).Label("Issue")}
}

type CreateIssue struct{}

func (CreateIssue) Meta() contract.Meta {
	return contract.Meta{ID: "create_issue", Label: "Create issue", TimeoutSec: 60}
}
func (CreateIssue) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("owner").Label("Repository owner"), field.String("repository").Label("Repository name"), field.String("title").Label("Title"), field.Json("options", IssueOptions{}).Label("Issue options").Optional()}
}
func (CreateIssue) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("issue", Issue{}).Label("Issue")}
}

type AddComment struct{}

func (AddComment) Meta() contract.Meta {
	return contract.Meta{ID: "add_comment", Label: "Add issue or pull request comment", TimeoutSec: 60}
}
func (AddComment) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("owner").Label("Repository owner"), field.String("repository").Label("Repository name"), field.String("number").Label("Issue or pull request number"), field.String("body").Label("Comment text")}
}
func (AddComment) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("comment", Comment{}).Label("Comment")}
}

type ListPullRequests struct{}

func (ListPullRequests) Meta() contract.Meta {
	return contract.Meta{ID: "list_pull_requests", Label: "List pull requests", TimeoutSec: 60}
}
func (ListPullRequests) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("owner").Label("Repository owner"), field.String("repository").Label("Repository name"), field.String("state").Label("State: open, closed or all").Optional(), field.Json("pagination", PageOptions{}).Label("Pagination").Optional()}
}
func (ListPullRequests) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("pull_requests", []PullRequest{}).Label("Pull requests"), field.Int("next_page").Label("Next page; zero means finished")}
}

type CreatePullRequest struct{}

func (CreatePullRequest) Meta() contract.Meta {
	return contract.Meta{ID: "create_pull_request", Label: "Create pull request", TimeoutSec: 60}
}
func (CreatePullRequest) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("owner").Label("Repository owner"), field.String("repository").Label("Repository name"), field.String("title").Label("Title"), field.String("head").Label("Head branch or owner:branch"), field.String("base").Label("Base branch"), field.String("body").Label("Description").Optional()}
}
func (CreatePullRequest) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("pull_request", PullRequest{}).Label("Pull request")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("token").Label("API token").Desc("Use a token issued by your configured self-hosted service. The token is sent only to that service.")}
}
