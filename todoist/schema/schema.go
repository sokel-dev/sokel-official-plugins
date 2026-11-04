package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type Project struct {
	ID          string  `json:"id" sokel:"id" label:"Project ID"`
	Name        string  `json:"name" sokel:"name" label:"Name"`
	Description string  `json:"description" sokel:"description" label:"Description"`
	Color       string  `json:"color" sokel:"color" label:"Color"`
	IsFavorite  bool    `json:"is_favorite" sokel:"is_favorite" label:"Favorite"`
	IsArchived  bool    `json:"is_archived" sokel:"is_archived" label:"Archived"`
	ViewStyle   string  `json:"view_style" sokel:"view_style" label:"View style"`
	ParentID    *string `json:"parent_id,omitempty" sokel:"parent_id,optional" label:"Parent project ID"`
}
type Due struct {
	Date        string  `json:"date" sokel:"date" label:"Due date"`
	IsRecurring bool    `json:"is_recurring" sokel:"is_recurring" label:"Recurring"`
	String      string  `json:"string" sokel:"string" label:"Due text"`
	Timezone    *string `json:"timezone,omitempty" sokel:"timezone,optional" label:"Timezone"`
}
type Task struct {
	ID          string   `json:"id" sokel:"id" label:"Task ID"`
	ProjectID   string   `json:"project_id" sokel:"project_id" label:"Project ID"`
	SectionID   *string  `json:"section_id,omitempty" sokel:"section_id,optional" label:"Section ID"`
	ParentID    *string  `json:"parent_id,omitempty" sokel:"parent_id,optional" label:"Parent task ID"`
	Content     string   `json:"content" sokel:"content" label:"Task content"`
	Description string   `json:"description" sokel:"description" label:"Description"`
	Labels      []string `json:"labels" sokel:"labels" label:"Labels"`
	Priority    int      `json:"priority" sokel:"priority" label:"Priority"`
	Checked     bool     `json:"checked" sokel:"checked" label:"Completed"`
	Due         *Due     `json:"due,omitempty" sokel:"due,optional" label:"Due"`
	AddedAt     string   `json:"added_at" sokel:"added_at" label:"Created at"`
	UpdatedAt   string   `json:"updated_at" sokel:"updated_at" label:"Updated at"`
}
type TaskDetails struct {
	Description *string   `json:"description,omitempty" sokel:"description,optional" label:"Description"`
	ProjectID   *string   `json:"project_id,omitempty" sokel:"project_id,optional" label:"Project ID"`
	SectionID   *string   `json:"section_id,omitempty" sokel:"section_id,optional" label:"Section ID"`
	ParentID    *string   `json:"parent_id,omitempty" sokel:"parent_id,optional" label:"Parent task ID"`
	Order       *int      `json:"order,omitempty" sokel:"order,optional" label:"Order"`
	Labels      *[]string `json:"labels,omitempty" sokel:"labels,optional" label:"Labels"`
	Priority    *int      `json:"priority,omitempty" sokel:"priority,optional" label:"Priority"`
	DueDate     *string   `json:"due_date,omitempty" sokel:"due_date,optional" label:"Due date"`
	DueDatetime *string   `json:"due_datetime,omitempty" sokel:"due_datetime,optional" label:"Due timestamp"`
}
type TaskChanges struct {
	Content     *string   `json:"content,omitempty" sokel:"content,optional" label:"Task content"`
	Description *string   `json:"description,omitempty" sokel:"description,optional" label:"Description"`
	Labels      *[]string `json:"labels,omitempty" sokel:"labels,optional" label:"Labels"`
	Priority    *int      `json:"priority,omitempty" sokel:"priority,optional" label:"Priority"`
	DueDate     *string   `json:"due_date,omitempty" sokel:"due_date,optional" label:"Due date"`
	DueDatetime *string   `json:"due_datetime,omitempty" sokel:"due_datetime,optional" label:"Due timestamp"`
	ChildOrder  *int      `json:"child_order,omitempty" sokel:"child_order,optional" label:"Order"`
	IsCollapsed *bool     `json:"is_collapsed,omitempty" sokel:"is_collapsed,optional" label:"Collapsed"`
}
type ProjectDetails struct {
	Description *string `json:"description,omitempty" sokel:"description,optional" label:"Description"`
	ParentID    *string `json:"parent_id,omitempty" sokel:"parent_id,optional" label:"Parent project ID"`
	IsFavorite  *bool   `json:"is_favorite,omitempty" sokel:"is_favorite,optional" label:"Favorite"`
	ViewStyle   *string `json:"view_style,omitempty" sokel:"view_style,optional" label:"View style"`
}
type ListProjects struct{}

func (ListProjects) Meta() contract.Meta {
	return contract.Meta{ID: "list_projects", Label: "List projects", TimeoutSec: 60}
}
func (ListProjects) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Int("limit").Label("Page size").Optional(), field.String("cursor").Label("Pagination cursor").Optional()}
}
func (ListProjects) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("projects", []Project{}).Label("Projects"), field.String("next_cursor").Label("Next cursor"), field.Bool("has_more").Label("More results")}
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
	return []contract.FieldSpec{field.String("name").Label("Name"), field.Json("details", ProjectDetails{}).Label("Project details").Optional()}
}
func (CreateProject) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("project", Project{}).Label("Project")}
}

type ListTasks struct{}

func (ListTasks) Meta() contract.Meta {
	return contract.Meta{ID: "list_tasks", Label: "List active tasks", TimeoutSec: 60}
}
func (ListTasks) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Int("limit").Label("Page size").Optional(), field.String("cursor").Label("Pagination cursor").Optional(), field.String("project_id").Label("Project ID").Optional(), field.String("section_id").Label("Section ID").Optional(), field.String("parent_id").Label("Parent task ID").Optional(), field.String("label").Label("Label").Optional()}
}
func (ListTasks) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("tasks", []Task{}).Label("Tasks"), field.String("next_cursor").Label("Next cursor"), field.Bool("has_more").Label("More results")}
}

type GetTask struct{}

func (GetTask) Meta() contract.Meta {
	return contract.Meta{ID: "get_task", Label: "Get active task", TimeoutSec: 60}
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
	return []contract.FieldSpec{field.String("content").Label("Task content"), field.Json("details", TaskDetails{}).Label("Task details").Optional()}
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

type CloseTask struct{}

func (CloseTask) Meta() contract.Meta {
	return contract.Meta{ID: "close_task", Label: "Complete task occurrence", TimeoutSec: 60}
}
func (CloseTask) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("task_id").Label("Task ID")}
}
func (CloseTask) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("accepted").Label("Accepted")}
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
	return []contract.FieldSpec{field.Secret("access_token").Label("Personal API token")}
}
