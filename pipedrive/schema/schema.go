package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type Page struct {
	Limit  *int    `json:"limit,omitempty" sokel:"limit,optional" label:"Page size"`
	Cursor *string `json:"cursor,omitempty" sokel:"cursor,optional" label:"Pagination cursor"`
}
type Address struct {
	Value   string  `json:"value" sokel:"value" label:"Email or phone"`
	Primary *bool   `json:"primary,omitempty" sokel:"primary,optional" label:"Primary"`
	Label   *string `json:"label,omitempty" sokel:"label,optional" label:"Label"`
}
type Person struct {
	ID     string    `json:"id" sokel:"id" label:"Person ID"`
	Name   string    `json:"name" sokel:"name" label:"Name"`
	OrgID  *string   `json:"org_id,omitempty" sokel:"org_id,optional" label:"Organization ID"`
	Emails []Address `json:"emails" sokel:"emails" label:"Email addresses"`
	Phones []Address `json:"phones" sokel:"phones" label:"Phone numbers"`
}
type Organization struct {
	ID   string `json:"id" sokel:"id" label:"Organization ID"`
	Name string `json:"name" sokel:"name" label:"Name"`
}
type Deal struct {
	ID       string  `json:"id" sokel:"id" label:"Deal ID"`
	Title    string  `json:"title" sokel:"title" label:"Title"`
	Status   string  `json:"status" sokel:"status" label:"Status"`
	Value    string  `json:"value" sokel:"value" label:"Exact monetary value"`
	Currency string  `json:"currency" sokel:"currency" label:"Currency"`
	PersonID *string `json:"person_id,omitempty" sokel:"person_id,optional" label:"Person ID"`
	OrgID    *string `json:"org_id,omitempty" sokel:"org_id,optional" label:"Organization ID"`
}
type PersonFields struct {
	OrgID  *string   `json:"org_id,omitempty" sokel:"org_id,optional" label:"Organization ID"`
	Emails []Address `json:"emails" sokel:"emails,optional" label:"Email addresses"`
	Phones []Address `json:"phones" sokel:"phones,optional" label:"Phone numbers"`
}
type DealFields struct {
	Value    *string `json:"value,omitempty" sokel:"value,optional" label:"Exact monetary value"`
	Currency *string `json:"currency,omitempty" sokel:"currency,optional" label:"Currency"`
	PersonID *string `json:"person_id,omitempty" sokel:"person_id,optional" label:"Person ID"`
	OrgID    *string `json:"org_id,omitempty" sokel:"org_id,optional" label:"Organization ID"`
	Status   *string `json:"status,omitempty" sokel:"status,optional" label:"Status"`
}
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check credentials", TimeoutSec: 30}
}
func (HealthCheck) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Usable"), field.String("message").Label("Check details")}
}

type ListPersons struct{}

func (ListPersons) Meta() contract.Meta {
	return contract.Meta{ID: "list_persons", Label: "List persons", TimeoutSec: 60}
}
func (ListPersons) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("page", Page{}).Label("Pagination").Optional()}
}
func (ListPersons) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("persons", []Person{}).Label("Persons"), field.String("next_cursor").Label("Next cursor"), field.Bool("has_more").Label("More results")}
}

type GetPerson struct{}

func (GetPerson) Meta() contract.Meta {
	return contract.Meta{ID: "get_person", Label: "Get person", TimeoutSec: 60}
}
func (GetPerson) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("person_id").Label("Person ID")}
}
func (GetPerson) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("person", Person{}).Label("Person")}
}

type CreatePerson struct{}

func (CreatePerson) Meta() contract.Meta {
	return contract.Meta{ID: "create_person", Label: "Create person", TimeoutSec: 60}
}
func (CreatePerson) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("name").Label("Name"), field.Json("fields", PersonFields{}).Label("Person fields").Optional()}
}
func (CreatePerson) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("person", Person{}).Label("Created person")}
}

type ListOrganizations struct{}

func (ListOrganizations) Meta() contract.Meta {
	return contract.Meta{ID: "list_organizations", Label: "List organizations", TimeoutSec: 60}
}
func (ListOrganizations) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("page", Page{}).Label("Pagination").Optional()}
}
func (ListOrganizations) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("organizations", []Organization{}).Label("Organizations"), field.String("next_cursor").Label("Next cursor"), field.Bool("has_more").Label("More results")}
}

type CreateOrganization struct{}

func (CreateOrganization) Meta() contract.Meta {
	return contract.Meta{ID: "create_organization", Label: "Create organization", TimeoutSec: 60}
}
func (CreateOrganization) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("name").Label("Name")}
}
func (CreateOrganization) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("organization", Organization{}).Label("Created organization")}
}

type ListDeals struct{}

func (ListDeals) Meta() contract.Meta {
	return contract.Meta{ID: "list_deals", Label: "List deals", TimeoutSec: 60}
}
func (ListDeals) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("page", Page{}).Label("Pagination").Optional()}
}
func (ListDeals) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("deals", []Deal{}).Label("Deals"), field.String("next_cursor").Label("Next cursor"), field.Bool("has_more").Label("More results")}
}

type CreateDeal struct{}

func (CreateDeal) Meta() contract.Meta {
	return contract.Meta{ID: "create_deal", Label: "Create deal", TimeoutSec: 60}
}
func (CreateDeal) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("title").Label("Title"), field.Json("fields", DealFields{}).Label("Deal fields").Optional()}
}
func (CreateDeal) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("deal", Deal{}).Label("Created deal")}
}

type UpdateDeal struct{}

func (UpdateDeal) Meta() contract.Meta {
	return contract.Meta{ID: "update_deal", Label: "Update deal", TimeoutSec: 60}
}
func (UpdateDeal) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("deal_id").Label("Deal ID"), field.String("title").Label("Title").Optional(), field.Json("fields", DealFields{}).Label("Deal fields").Optional()}
}
func (UpdateDeal) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("deal", Deal{}).Label("Updated deal")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("api_token").Label("API token").Desc("Use a personal API token. Rotating it invalidates integrations using the previous token."), field.Text("company_domain").Label("Company subdomain").Desc("Enter only the company subdomain, for example acme, from acme.pipedrive.com.")}
}
