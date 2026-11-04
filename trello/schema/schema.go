package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type Board struct {
	ID          string `json:"id" sokel:"id" label:"Board ID"`
	Name        string `json:"name" sokel:"name" label:"Name"`
	Description string `json:"desc" sokel:"description" label:"Description"`
	Closed      bool   `json:"closed" sokel:"closed" label:"Archived"`
	URL         string `json:"url" sokel:"url" label:"Board URL"`
}
type BoardList struct {
	ID       string  `json:"id" sokel:"id" label:"List ID"`
	BoardID  string  `json:"idBoard" sokel:"board_id" label:"Board ID"`
	Name     string  `json:"name" sokel:"name" label:"Name"`
	Closed   bool    `json:"closed" sokel:"closed" label:"Archived"`
	Position float64 `json:"pos" sokel:"position" label:"Position"`
}
type Card struct {
	ID          string  `json:"id" sokel:"id" label:"Card ID"`
	BoardID     string  `json:"idBoard" sokel:"board_id" label:"Board ID"`
	ListID      string  `json:"idList" sokel:"list_id" label:"List ID"`
	Name        string  `json:"name" sokel:"name" label:"Name"`
	Description string  `json:"desc" sokel:"description" label:"Description"`
	Closed      bool    `json:"closed" sokel:"closed" label:"Archived"`
	Due         *string `json:"due,omitempty" sokel:"due,optional" label:"Due date"`
	DueComplete bool    `json:"dueComplete" sokel:"due_complete" label:"Due date completed"`
	URL         string  `json:"url" sokel:"url" label:"Card URL"`
}
type CardChanges struct {
	Name        *string `json:"name,omitempty" sokel:"name,optional" label:"Name"`
	Description *string `json:"desc,omitempty" sokel:"description,optional" label:"Description"`
	ListID      *string `json:"idList,omitempty" sokel:"list_id,optional" label:"Destination list ID"`
	Due         *string `json:"due,omitempty" sokel:"due,optional" label:"Due date; empty clears it"`
	DueComplete *bool   `json:"dueComplete,omitempty" sokel:"due_complete,optional" label:"Due date completed"`
	Closed      *bool   `json:"closed,omitempty" sokel:"closed,optional" label:"Archived"`
}
type CardPage struct {
	Limit  *int    `json:"limit,omitempty" sokel:"limit,optional" label:"Page size"`
	Before *string `json:"before,omitempty" sokel:"before,optional" label:"Before card ID"`
}
type Comment struct {
	ID       string `json:"id" sokel:"id" label:"Comment ID"`
	CardID   string `json:"card_id" sokel:"card_id" label:"Card ID"`
	Text     string `json:"text" sokel:"text" label:"Comment text"`
	Date     string `json:"date" sokel:"date" label:"Created at"`
	MemberID string `json:"member_id" sokel:"member_id" label:"Author ID"`
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
	return []contract.FieldSpec{field.String("filter").Label("Board filter").Optional()}
}
func (ListBoards) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("boards", []Board{}).Label("Boards")}
}

type GetBoard struct{}

func (GetBoard) Meta() contract.Meta {
	return contract.Meta{ID: "get_board", Label: "Get board", TimeoutSec: 60}
}
func (GetBoard) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("board_id").Label("Board ID")}
}
func (GetBoard) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("board", Board{}).Label("Board")}
}

type ListLists struct{}

func (ListLists) Meta() contract.Meta {
	return contract.Meta{ID: "list_lists", Label: "List board lists", TimeoutSec: 60}
}
func (ListLists) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("board_id").Label("Board ID"), field.String("filter").Label("List filter").Optional()}
}
func (ListLists) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("lists", []BoardList{}).Label("Lists")}
}

type ListCards struct{}

func (ListCards) Meta() contract.Meta {
	return contract.Meta{ID: "list_cards", Label: "List board cards", TimeoutSec: 60}
}
func (ListCards) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("board_id").Label("Board ID"), field.String("filter").Label("Card filter").Optional(), field.Json("page", CardPage{}).Label("Pagination").Optional()}
}
func (ListCards) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("cards", []Card{}).Label("Cards"), field.String("oldest_card_id").Label("Oldest card ID"), field.Bool("may_have_more").Label("A further page may exist")}
}

type GetCard struct{}

func (GetCard) Meta() contract.Meta {
	return contract.Meta{ID: "get_card", Label: "Get card", TimeoutSec: 60}
}
func (GetCard) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("card_id").Label("Card ID")}
}
func (GetCard) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("card", Card{}).Label("Card")}
}

type CreateCard struct{}

func (CreateCard) Meta() contract.Meta {
	return contract.Meta{ID: "create_card", Label: "Create card", TimeoutSec: 60}
}
func (CreateCard) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("list_id").Label("List ID"), field.String("name").Label("Name"), field.String("description").Label("Description").Optional(), field.String("due").Label("Due date").Optional()}
}
func (CreateCard) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("card", Card{}).Label("Created card")}
}

type UpdateCard struct{}

func (UpdateCard) Meta() contract.Meta {
	return contract.Meta{ID: "update_card", Label: "Update card", TimeoutSec: 60}
}
func (UpdateCard) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("card_id").Label("Card ID"), field.Json("changes", CardChanges{}).Label("Card changes")}
}
func (UpdateCard) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("card", Card{}).Label("Updated card")}
}

type AddComment struct{}

func (AddComment) Meta() contract.Meta {
	return contract.Meta{ID: "add_comment", Label: "Add card comment", TimeoutSec: 60}
}
func (AddComment) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("card_id").Label("Card ID"), field.String("text").Label("Comment text")}
}
func (AddComment) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("comment", Comment{}).Label("Comment")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("api_key").Label("API key").Desc("Trello Power-Up API key used with the user token."), field.Secret("api_token").Label("User token").Desc("Supply a Trello user token with read scope; writes also require write scope.")}
}
