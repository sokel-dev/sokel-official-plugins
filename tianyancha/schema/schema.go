package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type Staff struct {
	ID        string   `json:"id" sokel:"id" label:"Person or company ID"`
	Name      string   `json:"name" sokel:"name" label:"Name"`
	Positions []string `json:"positions" sokel:"positions" label:"Positions"`
}
type Company struct {
	ID                  string  `json:"id" sokel:"id" label:"Company ID"`
	Name                string  `json:"name" sokel:"name" label:"Company name"`
	Status              string  `json:"status" sokel:"status" label:"Registration status"`
	CreditCode          string  `json:"credit_code" sokel:"credit_code" label:"Unified social credit code"`
	LegalRepresentative string  `json:"legal_representative" sokel:"legal_representative" label:"Legal representative"`
	Capital             string  `json:"capital" sokel:"capital" label:"Registered capital"`
	Industry            string  `json:"industry" sokel:"industry" label:"Industry"`
	Address             string  `json:"address" sokel:"address" label:"Registered address"`
	BusinessScope       string  `json:"business_scope" sokel:"business_scope" label:"Business scope"`
	EstablishedAt       string  `json:"established_at_ms" sokel:"established_at_ms" label:"Established at in milliseconds"`
	Staff               []Staff `json:"staff" sokel:"staff" label:"Key personnel"`
	StaffTotal          int     `json:"staff_total" sokel:"staff_total" label:"Total key personnel"`
}
type Change struct {
	Item      string `json:"changeItem" sokel:"item" label:"Changed item"`
	Before    string `json:"contentBefore" sokel:"before" label:"Before"`
	After     string `json:"contentAfter" sokel:"after" label:"After"`
	ChangedAt string `json:"changeTime" sokel:"changed_at" label:"Changed at"`
	CreatedAt string `json:"createTime" sokel:"created_at" label:"Created at"`
}
type PageOptions struct {
	Page     *int `json:"page,omitempty" sokel:"page,optional" label:"Page number"`
	PageSize *int `json:"page_size,omitempty" sokel:"page_size,optional" label:"Page size"`
}
type GetCompany struct{}

func (GetCompany) Meta() contract.Meta {
	return contract.Meta{ID: "get_company", Label: "Get company and key personnel", Desc: "Billable lookup; requires access to API 365", TimeoutSec: 60}
}
func (GetCompany) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("keyword").Label("Company name, ID or registration code")}
}
func (GetCompany) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("company", Company{}).Label("Company")}
}

type ListCompanyChanges struct{}

func (ListCompanyChanges) Meta() contract.Meta {
	return contract.Meta{ID: "list_company_changes", Label: "List company registration changes", Desc: "Billable lookup; requires access to API 822", TimeoutSec: 60}
}
func (ListCompanyChanges) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("keyword").Label("Company name or ID"), field.Json("page", PageOptions{}).Label("Pagination").Optional()}
}
func (ListCompanyChanges) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("changes", []Change{}).Label("Changes"), field.Int("total").Label("Total changes"), field.Int("next_page").Label("Next page"), field.Bool("has_more").Label("More results")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("access_token").Label("Commercial API token").Desc("Obtain a token and enable each required API in the Tianyancha data console")}
}
