package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type Project struct {
	ID          string `json:"id" sokel:"id" label:"Project ID"`
	Title       string `json:"title" sokel:"title" label:"Title"`
	Description string `json:"description" sokel:"description" label:"Description HTML"`
	IsArchived  bool   `json:"is_archived" sokel:"is_archived" label:"Archived"`
}
type Task struct {
	ID          string `json:"id" sokel:"id" label:"Task ID"`
	ProjectID   string `json:"project_id" sokel:"project_id" label:"Project ID"`
	Title       string `json:"title" sokel:"title" label:"Title"`
	Description string `json:"description" sokel:"description" label:"Description HTML"`
	Done        bool   `json:"done" sokel:"done" label:"Completed"`
	Priority    int    `json:"priority" sokel:"priority" label:"Priority"`
	DueDate     string `json:"due_date" sokel:"due_date" label:"Due date"`
}
type TaskFields struct {
	Title       *string `json:"title,omitempty" sokel:"title,optional" label:"Title"`
	Description *string `json:"description,omitempty" sokel:"description,optional" label:"Description HTML"`
	Done        *bool   `json:"done,omitempty" sokel:"done,optional" label:"Completed"`
	Priority    *int    `json:"priority,omitempty" sokel:"priority,optional" label:"Priority"`
	DueDate     *string `json:"due_date,omitempty" sokel:"due_date,optional" label:"Due date"`
}
type TaskOptions struct {
	Description *string `json:"description,omitempty" sokel:"description,optional" label:"Description HTML"`
	Done        *bool   `json:"done,omitempty" sokel:"done,optional" label:"Completed"`
	Priority    *int    `json:"priority,omitempty" sokel:"priority,optional" label:"Priority"`
	DueDate     *string `json:"due_date,omitempty" sokel:"due_date,optional" label:"Due date"`
}
type Page struct {
	Number *int    `json:"number,omitempty" sokel:"number,optional" label:"Page number"`
	Size   *int    `json:"size,omitempty" sokel:"size,optional" label:"Requested page size"`
	Query  *string `json:"query,omitempty" sokel:"query,optional" label:"Search text"`
}
type Comment struct {
	ID      string `json:"id" sokel:"id" label:"Comment ID"`
	Comment string `json:"comment" sokel:"comment" label:"Comment HTML"`
	Created string `json:"created" sokel:"created" label:"Created at"`
}

type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check connection and token", TimeoutSec: 60}
}
func (HealthCheck) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Usable"), field.String("message").Label("Check details")}
}

type ListProjects struct{}

func (ListProjects) Meta() contract.Meta {
	return contract.Meta{ID: "list_projects", Label: "List projects", TimeoutSec: 60}
}
func (ListProjects) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("page", Page{}).Label("Pagination").Optional()}
}
func (ListProjects) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("projects", []Project{}).Label("Projects"), field.String("total").Label("Total result count"), field.Int("next_page").Label("Next page; zero means complete"), field.Bool("has_more").Label("More results")}
}

type GetProject struct{}

func (GetProject) Meta() contract.Meta {
	return contract.Meta{ID: "get_project", Label: "Get project", TimeoutSec: 60}
}
func (GetProject) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("project_id").Label("Project ID")}
}
func (GetProject) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("project", Project{}).Label("Project")}
}

type CreateProject struct{}

func (CreateProject) Meta() contract.Meta {
	return contract.Meta{ID: "create_project", Label: "Create project", TimeoutSec: 60}
}
func (CreateProject) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("title").Label("Title"), field.String("description").Label("Description HTML").Optional()}
}
func (CreateProject) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("project", Project{}).Label("Created project")}
}

type ListTasks struct{}

func (ListTasks) Meta() contract.Meta {
	return contract.Meta{ID: "list_tasks", Label: "List project tasks", TimeoutSec: 60}
}
func (ListTasks) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("project_id").Label("Project ID"), field.Json("page", Page{}).Label("Pagination").Optional()}
}
func (ListTasks) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("tasks", []Task{}).Label("Tasks"), field.String("total").Label("Total result count"), field.Int("next_page").Label("Next page; zero means complete"), field.Bool("has_more").Label("More results")}
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
	return []contract.FieldSpec{field.String("project_id").Label("Project ID"), field.String("title").Label("Title"), field.Json("fields", TaskOptions{}).Label("Optional task fields").Optional()}
}
func (CreateTask) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("task", Task{}).Label("Created task")}
}

type UpdateTask struct{}

func (UpdateTask) Meta() contract.Meta {
	return contract.Meta{ID: "update_task", Label: "Update task fields", TimeoutSec: 60}
}
func (UpdateTask) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("task_id").Label("Task ID"), field.Json("fields", TaskFields{}).Label("Fields to change")}
}
func (UpdateTask) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("task", Task{}).Label("Updated task")}
}

type AddComment struct{}

func (AddComment) Meta() contract.Meta {
	return contract.Meta{ID: "add_comment", Label: "Add task comment", TimeoutSec: 60}
}
func (AddComment) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("task_id").Label("Task ID"), field.String("comment").Label("Comment HTML")}
}
func (AddComment) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("comment", Comment{}).Label("Created comment")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("access_token").Label("API token").Desc("Use a scoped API token with permission to read projects and perform the requested task operations. Manage expiry and rotation outside this plugin.")}
}
