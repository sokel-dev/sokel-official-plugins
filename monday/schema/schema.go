package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type Page struct {
	Limit  *int    `json:"limit,omitempty" sokel:"limit,optional" label:"Page size"`
	Cursor *string `json:"cursor,omitempty" sokel:"cursor,optional" label:"Pagination cursor"`
}
type Board struct {
	ID    string `json:"id" sokel:"id" label:"Board ID"`
	Name  string `json:"name" sokel:"name" label:"Name"`
	State string `json:"state" sokel:"state" label:"State"`
	URL   string `json:"url" sokel:"url" label:"Board URL"`
}
type Group struct {
	ID    string `json:"id" sokel:"id" label:"Group ID"`
	Title string `json:"title" sokel:"title" label:"Title"`
}
type Column struct {
	ID    string `json:"id" sokel:"id" label:"Column ID"`
	Title string `json:"title" sokel:"title" label:"Title"`
	Type  string `json:"type" sokel:"type" label:"Column type"`
}
type ColumnValue struct {
	ID    string  `json:"id" sokel:"id" label:"Column ID"`
	Type  string  `json:"type" sokel:"type" label:"Column type"`
	Text  *string `json:"text,omitempty" sokel:"text,optional" label:"Display text"`
	Value *string `json:"value,omitempty" sokel:"value,optional" label:"Raw value JSON"`
}
type Item struct {
	ID      string        `json:"id" sokel:"id" label:"Item ID"`
	Name    string        `json:"name" sokel:"name" label:"Name"`
	State   string        `json:"state" sokel:"state" label:"State"`
	URL     string        `json:"url" sokel:"url" label:"Item URL"`
	Board   BoardRef      `json:"board" sokel:"board" label:"Board"`
	Group   *Group        `json:"group,omitempty" sokel:"group,optional" label:"Group"`
	Columns []ColumnValue `json:"column_values" sokel:"column_values" label:"Column values"`
}
type BoardRef struct {
	ID string `json:"id" sokel:"id" label:"Board ID"`
}
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check credentials", TimeoutSec: 30}
}
func (HealthCheck) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Usable"), field.String("message").Label("Check details")}
}

type ListBoards struct{}

func (ListBoards) Meta() contract.Meta {
	return contract.Meta{ID: "list_boards", Label: "List boards", TimeoutSec: 60}
}
func (ListBoards) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("page", BoardPage{}).Label("Pagination").Optional()}
}
func (ListBoards) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("boards", []Board{}).Label("Boards"), field.Int("next_page").Label("Next page; zero means complete")}
}

type GetBoard struct{}

func (GetBoard) Meta() contract.Meta {
	return contract.Meta{ID: "get_board", Label: "Get board structure", TimeoutSec: 60}
}
func (GetBoard) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("board_id").Label("Board ID")}
}
func (GetBoard) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("board", Board{}).Label("Board"), field.Array("groups", []Group{}).Label("Groups"), field.Array("columns", []Column{}).Label("Columns")}
}

type ListItems struct{}

func (ListItems) Meta() contract.Meta {
	return contract.Meta{ID: "list_items", Label: "List board items", TimeoutSec: 60}
}
func (ListItems) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("board_id").Label("Board ID"), field.Json("page", Page{}).Label("Pagination").Optional()}
}
func (ListItems) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("items", []Item{}).Label("Items"), field.String("next_cursor").Label("Next cursor"), field.Bool("has_more").Label("More results")}
}

type GetItem struct{}

func (GetItem) Meta() contract.Meta {
	return contract.Meta{ID: "get_item", Label: "Get item", TimeoutSec: 60}
}
func (GetItem) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("item_id").Label("Item ID")}
}
func (GetItem) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("item", Item{}).Label("Item")}
}

type CreateItem struct{}

func (CreateItem) Meta() contract.Meta {
	return contract.Meta{ID: "create_item", Label: "Create item", TimeoutSec: 60}
}
func (CreateItem) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("board_id").Label("Board ID"), field.String("name").Label("Name"), field.String("group_id").Label("Group ID").Optional(), field.String("column_values_json").Label("Column values as a JSON object string").Optional(), field.Json("options", MutationOptions{}).Label("Mutation options").Optional()}
}
func (CreateItem) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("item", Item{}).Label("Created item")}
}

type ChangeColumns struct{}

func (ChangeColumns) Meta() contract.Meta {
	return contract.Meta{ID: "change_columns", Label: "Change item column values", TimeoutSec: 60}
}
func (ChangeColumns) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("board_id").Label("Board ID"), field.String("item_id").Label("Item ID"), field.String("column_values_json").Label("Column values as a JSON object string"), field.Json("options", MutationOptions{}).Label("Mutation options").Optional()}
}
func (ChangeColumns) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("item", Item{}).Label("Updated item")}
}

type AddUpdate struct{}

func (AddUpdate) Meta() contract.Meta {
	return contract.Meta{ID: "add_update", Label: "Add item update", TimeoutSec: 60}
}
func (AddUpdate) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("item_id").Label("Item ID"), field.String("body").Label("Update body")}
}
func (AddUpdate) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("update_id").Label("Update ID"), field.String("body").Label("Update body")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("access_token").Label("API token").Desc("Provide a personal API token with access to the target boards; manage renewal outside this plugin.")}
}

type BoardPage struct {
	Number *int `json:"number,omitempty" sokel:"number,optional" label:"Page number"`
	Limit  *int `json:"limit,omitempty" sokel:"limit,optional" label:"Page size"`
}
type MutationOptions struct {
	CreateLabels *bool `json:"create_labels,omitempty" sokel:"create_labels,optional" label:"Create missing labels"`
}
