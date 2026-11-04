package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Text("username").Label("Username").Desc("Optional HTTP Basic username. Use either Basic authentication or a bearer token.").Optional(), field.Secret("password").Label("Password").Optional(), field.Secret("bearer_token").Label("Bearer token").Desc("Optional reverse-proxy bearer token. Leave all credential fields empty for an unprotected private service.").Optional()}
}

type QueryOptions struct {
	Time  *string `json:"time,omitempty" sokel:"time,optional" label:"Evaluation time"`
	Limit *int    `json:"limit,omitempty" sokel:"limit,optional" label:"Series limit"`
}
type RangeOptions struct {
	Limit *int `json:"limit,omitempty" sokel:"limit,optional" label:"Series limit"`
}
type MetadataOptions struct {
	Start   *string  `json:"start,omitempty" sokel:"start,optional" label:"Start time"`
	End     *string  `json:"end,omitempty" sokel:"end,optional" label:"End time"`
	Matches []string `json:"matches,omitempty" sokel:"matches,optional" label:"Series selectors"`
	Limit   *int     `json:"limit,omitempty" sokel:"limit,optional" label:"Result limit"`
}
type Alert struct {
	LabelsJSON      string  `json:"labels_json" sokel:"labels_json" label:"Labels JSON"`
	AnnotationsJSON string  `json:"annotations_json" sokel:"annotations_json" label:"Annotations JSON"`
	State           string  `json:"state" sokel:"state" label:"Alert state"`
	ActiveAt        *string `json:"active_at,omitempty" sokel:"active_at,optional" label:"Active since"`
	Value           string  `json:"value" sokel:"value" label:"Sample value"`
}
type GetBuildInfo struct{}

func (GetBuildInfo) Meta() contract.Meta {
	return contract.Meta{ID: "get_build_info", Label: "Get build information", Desc: "", TimeoutSec: 60}
}
func (GetBuildInfo) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (GetBuildInfo) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("version").Label("Version"), field.String("revision").Label("Revision"), field.String("build_date").Label("Build date")}
}

type Query struct{}

func (Query) Meta() contract.Meta {
	return contract.Meta{ID: "query", Label: "Run instant query", Desc: "", TimeoutSec: 60}
}
func (Query) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("query").Label("PromQL expression"), field.Json("options", QueryOptions{}).Label("Query options").Optional()}
}
func (Query) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("result_type").Label("Result type"), field.String("result_json").Label("Result JSON").Desc("PromQL returns dynamic label sets, scalar tuples or native histograms; JSON text preserves their structure and sample precision."), field.Strings("warnings").Label("Warnings"), field.Strings("infos").Label("Information annotations")}
}

type QueryRange struct{}

func (QueryRange) Meta() contract.Meta {
	return contract.Meta{ID: "query_range", Label: "Run range query", Desc: "", TimeoutSec: 60}
}
func (QueryRange) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("query").Label("PromQL expression"), field.String("start").Label("Start time"), field.String("end").Label("End time"), field.String("step_seconds").Label("Step in seconds"), field.Json("options", RangeOptions{}).Label("Query options").Optional()}
}
func (QueryRange) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("result_type").Label("Result type"), field.String("result_json").Label("Result JSON").Desc("PromQL returns dynamic label sets, scalar tuples or native histograms; JSON text preserves their structure and sample precision."), field.Strings("warnings").Label("Warnings"), field.Strings("infos").Label("Information annotations")}
}

type ListLabelNames struct{}

func (ListLabelNames) Meta() contract.Meta {
	return contract.Meta{ID: "list_label_names", Label: "List label names", Desc: "", TimeoutSec: 60}
}
func (ListLabelNames) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("options", MetadataOptions{}).Label("Metadata filters").Optional()}
}
func (ListLabelNames) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Strings("names").Label("Label names"), field.Strings("warnings").Label("Warnings"), field.Strings("infos").Label("Information annotations")}
}

type ListLabelValues struct{}

func (ListLabelValues) Meta() contract.Meta {
	return contract.Meta{ID: "list_label_values", Label: "List label values", Desc: "", TimeoutSec: 60}
}
func (ListLabelValues) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("label").Label("Label name"), field.Json("options", MetadataOptions{}).Label("Metadata filters").Optional()}
}
func (ListLabelValues) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Strings("values").Label("Label values"), field.Strings("warnings").Label("Warnings"), field.Strings("infos").Label("Information annotations")}
}

type FindSeries struct{}

func (FindSeries) Meta() contract.Meta {
	return contract.Meta{ID: "find_series", Label: "Find series", Desc: "", TimeoutSec: 60}
}
func (FindSeries) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("options", MetadataOptions{}).Label("Metadata filters")}
}
func (FindSeries) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Strings("series_json").Label("Series label sets JSON").Desc("Each JSON string contains one dynamic metric label set, preserving user-defined label names."), field.Strings("warnings").Label("Warnings"), field.Strings("infos").Label("Information annotations")}
}

type ListAlerts struct{}

func (ListAlerts) Meta() contract.Meta {
	return contract.Meta{ID: "list_alerts", Label: "List active alerts", Desc: "Read pending and firing alert instances from Prometheus; this does not manage Alertmanager silences or alert rules.", TimeoutSec: 60}
}
func (ListAlerts) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (ListAlerts) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("alerts", []Alert{}).Label("Alerts"), field.Strings("warnings").Label("Warnings"), field.Strings("infos").Label("Information annotations")}
}
