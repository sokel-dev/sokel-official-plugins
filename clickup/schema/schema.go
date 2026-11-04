package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type Named struct {
	ID   string `json:"id" sokel:"id" label:"ID"`
	Name string `json:"name" sokel:"name" label:"Name"`
}
type Status struct {
	Status string `json:"status" sokel:"name" label:"Status"`
	Type   string `json:"type" sokel:"type" label:"Status type"`
}
type Priority struct {
	Priority string `json:"priority" sokel:"name" label:"Priority name"`
}
type User struct {
	ID       int64  `json:"id" sokel:"id" label:"User ID"`
	Username string `json:"username" sokel:"username" label:"Username"`
}
type Task struct {
	ID          string    `json:"id" sokel:"id" label:"Task ID"`
	Name        string    `json:"name" sokel:"name" label:"Name"`
	Description string    `json:"description" sokel:"description" label:"Description"`
	Status      Status    `json:"status" sokel:"status" label:"Status"`
	Priority    *Priority `json:"priority" sokel:"priority,optional" label:"Priority"`
	Assignees   []User    `json:"assignees" sokel:"assignees" label:"Assignees"`
	URL         string    `json:"url" sokel:"url" label:"URL"`
	DueDate     string    `json:"due_date" sokel:"due_date_ms" label:"Due date in milliseconds"`
	Updated     string    `json:"date_updated" sokel:"updated_at_ms" label:"Updated at in milliseconds"`
	Archived    bool      `json:"archived" sokel:"archived" label:"Archived"`
}
type QueryOptions struct {
	Archived      *bool `json:"archived,omitempty" sokel:"archived,optional" label:"Archived"`
	IncludeClosed *bool `json:"include_closed,omitempty" sokel:"include_closed,optional" label:"Include closed tasks"`
	IncludeTiml   *bool `json:"include_timl,omitempty" sokel:"include_timl,optional" label:"Include tasks from other home lists"`
	Subtasks      *bool `json:"subtasks,omitempty" sokel:"subtasks,optional" label:"Include subtasks"`
	Page          *int  `json:"page,omitempty" sokel:"page,optional" label:"Zero-based page"`
}
type TaskChanges struct {
	Name        *string `json:"name,omitempty" sokel:"name,optional" label:"Name"`
	Description *string `json:"description,omitempty" sokel:"description,optional" label:"Description"`
	Status      *string `json:"status,omitempty" sokel:"status,optional" label:"Status name"`
	Priority    *int    `json:"priority,omitempty" sokel:"priority,optional" label:"Priority (1 to 4)"`
	DueDate     *int64  `json:"due_date,omitempty" sokel:"due_date_ms,optional" label:"Due date in milliseconds"`
	Archived    *bool   `json:"archived,omitempty" sokel:"archived,optional" label:"Archived"`
	DueDateTime *bool   `json:"due_date_time,omitempty" sokel:"due_date_time,optional" label:"Include due time"`
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
func (ListWorkspaces) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (ListWorkspaces) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("workspaces", []Named{}).Label("Workspaces")}
}

type ListSpaces struct{}

func (ListSpaces) Meta() contract.Meta {
	return contract.Meta{ID: "list_spaces", Label: "List spaces", TimeoutSec: 60}
}
func (ListSpaces) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("workspace_id").Label("Workspace ID")}
}
func (ListSpaces) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("spaces", []Named{}).Label("Spaces")}
}

type ListFolders struct{}

func (ListFolders) Meta() contract.Meta {
	return contract.Meta{ID: "list_folders", Label: "List folders", TimeoutSec: 60}
}
func (ListFolders) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("space_id").Label("Space ID")}
}
func (ListFolders) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("folders", []Named{}).Label("Folders")}
}

type ListLists struct{}

func (ListLists) Meta() contract.Meta {
	return contract.Meta{ID: "list_lists", Label: "List task lists", TimeoutSec: 60}
}
func (ListLists) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("parent_type").Label("Parent type").Desc("space lists only folderless lists; folder lists lists inside a folder"), field.String("parent_id").Label("Parent ID")}
}
func (ListLists) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("lists", []Named{}).Label("Lists")}
}

type ListTasks struct{}

func (ListTasks) Meta() contract.Meta {
	return contract.Meta{ID: "list_tasks", Label: "List tasks", TimeoutSec: 60}
}
func (ListTasks) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("list_id").Label("List ID"), field.Json("options", QueryOptions{}).Label("Query options").Optional()}
}
func (ListTasks) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("tasks", []Task{}).Label("Tasks"), field.Int("next_page").Label("Next page"), field.Bool("has_more").Label("More results")}
}

type GetTask struct{}

func (GetTask) Meta() contract.Meta {
	return contract.Meta{ID: "get_task", Label: "Get task", TimeoutSec: 60}
}
func (GetTask) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("task_id").Label("Task ID")}
}
func (GetTask) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("task", Task{}).Label("Task")}
}

type CreateTask struct{}

func (CreateTask) Meta() contract.Meta {
	return contract.Meta{ID: "create_task", Label: "Create task", TimeoutSec: 60}
}
func (CreateTask) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("list_id").Label("List ID"), field.String("name").Label("Name"), field.Json("details", TaskChanges{}).Label("Optional task details").Optional()}
}
func (CreateTask) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("task", Task{}).Label("Task")}
}

type UpdateTask struct{}

func (UpdateTask) Meta() contract.Meta {
	return contract.Meta{ID: "update_task", Label: "Update task", TimeoutSec: 60}
}
func (UpdateTask) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("task_id").Label("Task ID"), field.Json("changes", TaskChanges{}).Label("Changed fields")}
}
func (UpdateTask) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("task", Task{}).Label("Task")}
}

type AddComment struct{}

func (AddComment) Meta() contract.Meta {
	return contract.Meta{ID: "add_comment", Label: "Add task comment", TimeoutSec: 60}
}
func (AddComment) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("task_id").Label("Task ID"), field.String("text").Label("Comment text"), field.Bool("notify_all").Label("Notify comment author too").Optional()}
}
func (AddComment) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("comment_id").Label("Comment ID")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("access_token").Label("Personal API token")}
}
