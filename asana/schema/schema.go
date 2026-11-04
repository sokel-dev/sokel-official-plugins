package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type Named struct {
	GID  string `json:"gid" sokel:"gid" label:"GID"`
	Name string `json:"name" sokel:"name" label:"Name"`
}
type Task struct {
	GID        string `json:"gid" sokel:"gid" label:"Task GID"`
	Name       string `json:"name" sokel:"name" label:"Name"`
	Notes      string `json:"notes" sokel:"notes" label:"Notes"`
	Completed  bool   `json:"completed" sokel:"completed" label:"Completed"`
	DueOn      string `json:"due_on" sokel:"due_on" label:"Due date"`
	Assignee   *Named `json:"assignee" sokel:"assignee,optional" label:"Assignee"`
	CreatedAt  string `json:"created_at" sokel:"created_at" label:"Created at"`
	ModifiedAt string `json:"modified_at" sokel:"modified_at" label:"Modified at"`
	URL        string `json:"permalink_url" sokel:"url" label:"Task URL"`
}
type PageOptions struct {
	Limit  *int   `json:"limit,omitempty" sokel:"limit,optional" label:"Page size"`
	Offset string `json:"offset,omitempty" sokel:"offset,optional" label:"Pagination token"`
}
type TaskChanges struct {
	Name      *string `json:"name,omitempty" sokel:"name,optional" label:"Name"`
	Notes     *string `json:"notes,omitempty" sokel:"notes,optional" label:"Notes"`
	Completed *bool   `json:"completed,omitempty" sokel:"completed,optional" label:"Completed"`
	Assignee  *string `json:"assignee,omitempty" sokel:"assignee_gid,optional" label:"Assignee GID (empty clears)"`
	DueOn     *string `json:"due_on,omitempty" sokel:"due_on,optional" label:"Due date (empty clears)"`
}
type Story struct {
	GID       string `json:"gid" sokel:"gid" label:"Story GID"`
	Text      string `json:"text" sokel:"text" label:"Comment text"`
	CreatedAt string `json:"created_at" sokel:"created_at" label:"Created at"`
	Type      string `json:"resource_subtype" sokel:"type" label:"Story type"`
}
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check credential", TimeoutSec: 60}
}
func (HealthCheck) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Available"), field.String("message").Label("Message")}
}

type ListWorkspaces struct{}

func (ListWorkspaces) Meta() contract.Meta {
	return contract.Meta{ID: "list_workspaces", Label: "List workspaces", TimeoutSec: 60}
}
func (ListWorkspaces) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("page", PageOptions{}).Label("Pagination").Optional()}
}
func (ListWorkspaces) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("workspaces", []Named{}).Label("Workspaces"), field.String("next_offset").Label("Next pagination token"), field.Bool("has_more").Label("More results")}
}

type ListProjects struct{}

func (ListProjects) Meta() contract.Meta {
	return contract.Meta{ID: "list_projects", Label: "List projects", TimeoutSec: 60}
}
func (ListProjects) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("workspace_gid").Label("Workspace GID"), field.Json("page", PageOptions{}).Label("Pagination").Optional()}
}
func (ListProjects) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("projects", []Named{}).Label("Projects"), field.String("next_offset").Label("Next pagination token"), field.Bool("has_more").Label("More results")}
}

type ListTasks struct{}

func (ListTasks) Meta() contract.Meta {
	return contract.Meta{ID: "list_tasks", Label: "List project tasks", TimeoutSec: 60}
}
func (ListTasks) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("project_gid").Label("Project GID"), field.Json("page", PageOptions{}).Label("Pagination").Optional()}
}
func (ListTasks) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("tasks", []Task{}).Label("Tasks"), field.String("next_offset").Label("Next pagination token"), field.Bool("has_more").Label("More results")}
}

type GetTask struct{}

func (GetTask) Meta() contract.Meta {
	return contract.Meta{ID: "get_task", Label: "Get task", TimeoutSec: 60}
}
func (GetTask) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("task_gid").Label("Task GID")}
}
func (GetTask) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("task", Task{}).Label("Task")}
}

type CreateTask struct{}

func (CreateTask) Meta() contract.Meta {
	return contract.Meta{ID: "create_task", Label: "Create task", TimeoutSec: 60}
}
func (CreateTask) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("workspace_gid").Label("Workspace GID"), field.String("name").Label("Name"), field.Strings("project_gids").Label("Project GIDs").Optional(), field.Json("details", TaskChanges{}).Label("Optional task details").Optional()}
}
func (CreateTask) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("task", Task{}).Label("Task")}
}

type UpdateTask struct{}

func (UpdateTask) Meta() contract.Meta {
	return contract.Meta{ID: "update_task", Label: "Update task", TimeoutSec: 60}
}
func (UpdateTask) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("task_gid").Label("Task GID"), field.Json("changes", TaskChanges{}).Label("Changed fields")}
}
func (UpdateTask) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("task", Task{}).Label("Task")}
}

type AddComment struct{}

func (AddComment) Meta() contract.Meta {
	return contract.Meta{ID: "add_comment", Label: "Add task comment", TimeoutSec: 60}
}
func (AddComment) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("task_gid").Label("Task GID"), field.String("text").Label("Comment text")}
}
func (AddComment) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("story", Story{}).Label("Comment story")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("access_token").Label("Personal access token")}
}
