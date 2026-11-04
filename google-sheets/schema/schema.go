// Package schema declares the public Google integration contract.
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/auth"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type Sheet struct {
	ID          int    `json:"sheetId" sokel:"id" label:"Sheet ID"`
	Title       string `json:"title" sokel:"title" label:"Name"`
	RowCount    int    `json:"rowCount" sokel:"row_count" label:"Grid row count"`
	ColumnCount int    `json:"columnCount" sokel:"column_count" label:"Grid column count"`
}
type ValueRange struct {
	Range  string `json:"range" sokel:"range" label:"A1 range"`
	Values []any  `json:"values" sokel:"values" label:"Two-dimensional cell values" desc:"Each row is an array; cell types are defined by the spreadsheet"`
}
type GetSpreadsheet struct{}

func (GetSpreadsheet) Meta() contract.Meta {
	return contract.Meta{ID: "get_spreadsheet", Label: "Get spreadsheet metadata", TimeoutSec: 60}
}
func (GetSpreadsheet) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("spreadsheet_id").Label("Spreadsheet ID")}
}
func (GetSpreadsheet) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("spreadsheet_id").Label("Spreadsheet ID"), field.String("title").Label("Title"), field.String("time_zone").Label("Time zone"), field.Array("sheets", []Sheet{}).Label("Sheets")}
}

type ReadValues struct{}

func (ReadValues) Meta() contract.Meta {
	return contract.Meta{ID: "read_values", Label: "Read cells", TimeoutSec: 60}
}
func (ReadValues) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("spreadsheet_id").Label("Spreadsheet ID"), field.String("range").Label("A1 range"), field.String("value_render_option").Label("Value render option").Optional().Desc("FORMATTED_VALUE, UNFORMATTED_VALUE or FORMULA")}
}
func (ReadValues) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("range").Label("A1 range"), field.Array("values", []any{}).Label("Two-dimensional cell values").Opaque("Each row is an array of string, number, boolean or null cells; columns are defined by the spreadsheet")}
}

type UpdateValues struct{}

func (UpdateValues) Meta() contract.Meta {
	return contract.Meta{ID: "update_values", Label: "Update cells", TimeoutSec: 60}
}
func (UpdateValues) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("spreadsheet_id").Label("Spreadsheet ID"), field.String("range").Label("A1 range"), field.Array("values", []any{}).Label("Two-dimensional cell values").Opaque("Each row is an array of string, number, boolean or null cells; columns are defined by the spreadsheet"), field.String("value_input_option").Label("Value input option").Optional().Desc("RAW or USER_ENTERED; defaults to RAW")}
}
func (UpdateValues) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("updated_range").Label("Updated range"), field.Int("updated_rows").Label("Updated row count"), field.Int("updated_cells").Label("Updated cell count")}
}

type AppendValues struct{}

func (AppendValues) Meta() contract.Meta {
	return contract.Meta{ID: "append_values", Label: "Append rows", TimeoutSec: 60}
}
func (AppendValues) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("spreadsheet_id").Label("Spreadsheet ID"), field.String("range").Label("A1 range"), field.Array("values", []any{}).Label("Two-dimensional cell values").Opaque("Each row is an array of string, number, boolean or null cells; columns are defined by the spreadsheet"), field.String("value_input_option").Label("Value input option").Optional().Desc("RAW or USER_ENTERED; defaults to RAW")}
}
func (AppendValues) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("updated_range").Label("Updated range"), field.Int("updated_rows").Label("Updated row count"), field.Int("updated_cells").Label("Updated cell count")}
}

type BatchUpdateValues struct{}

func (BatchUpdateValues) Meta() contract.Meta {
	return contract.Meta{ID: "batch_update_values", Label: "Batch update ranges", TimeoutSec: 60}
}
func (BatchUpdateValues) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("spreadsheet_id").Label("Spreadsheet ID"), field.Array("data", []ValueRange{}).Label("Range data"), field.String("value_input_option").Label("Value input option").Optional().Desc("RAW or USER_ENTERED; defaults to RAW")}
}
func (BatchUpdateValues) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Int("updated_rows").Label("Updated row count"), field.Int("updated_cells").Label("Updated cell count")}
}

type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check credential", TimeoutSec: 30}
}
func (HealthCheck) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Available"), field.String("message").Label("Message")}
}

type Credential struct{}

func (Credential) AuthMeta() contract.AuthMeta {
	return auth.OAuth("google", "https://www.googleapis.com/auth/spreadsheets", "https://www.googleapis.com/auth/userinfo.email")
}
func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("access_token").Label("Access token (injected by platform)").Optional(), field.Secret("refresh_token").Label("Refresh token (managed by platform)").Optional(), field.Text("poll_cursor").Label("Polling cursor (managed automatically)").Optional(), field.Text("next_poll_at").Label("Next poll time (managed automatically)").Optional(), field.Text("watch_spreadsheet_id").Label("Watched spreadsheet ID").Optional(), field.Text("watch_range").Label("Watched A1 range").Optional()}
}

type RowAdded struct{}

func (RowAdded) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "row_added", Label: "Row appended"}
}
func (RowAdded) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("spreadsheet_id").Label("Spreadsheet ID"), field.String("range").Label("A1 range"), field.Int("row_offset").Label("Row offset within range"), field.Array("values", []any{}).Label("Cell values").Opaque("Cell types are defined by the spreadsheet")}
}
