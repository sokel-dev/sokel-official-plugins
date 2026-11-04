package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-official-plugins/pipedrive/schema"
)

func (c *client) list_persons(ctx context.Context, in *ListPersonsIn) (*ListPersonsOut, error) {
	rows, next, e := c.page(ctx, "/api/v2/persons", in.Page)
	if e != nil {
		return nil, e
	}
	out := &ListPersonsOut{Persons: []schema.Person{}, NextCursor: next, HasMore: next != ""}
	seen := map[string]bool{}
	for _, b := range rows {
		v, e := person(b)
		if e != nil {
			return nil, e
		}
		if seen[v.ID] {
			return nil, errors.New("Pipedrive returned duplicate persons.")
		}
		seen[v.ID] = true
		out.Persons = append(out.Persons, v)
	}
	return out, nil
}
func (c *client) get_person(ctx context.Context, in *GetPersonIn) (*GetPersonOut, error) {
	if e := checkID(in.PersonID); e != nil {
		return nil, e
	}
	b, _, e := c.call(ctx, "GET", "/api/v2/persons/"+in.PersonID, nil, nil)
	if e != nil {
		return nil, e
	}
	v, e := person(b)
	if e != nil {
		return nil, e
	}
	if v.ID != in.PersonID {
		return nil, errors.New("Pipedrive returned a different person.")
	}
	return &GetPersonOut{Person: v}, nil
}
func (c *client) create_person(ctx context.Context, in *CreatePersonIn) (*CreatePersonOut, error) {
	if !textOK(in.Name, 1024) {
		return nil, errors.New("Person name is required and must be at most 1024 bytes.")
	}
	body := map[string]any{"name": in.Name}
	if in.Fields.OrgID != nil {
		if e := checkID(*in.Fields.OrgID); e != nil {
			return nil, e
		}
		body["org_id"] = json.Number(*in.Fields.OrgID)
	}
	if e := addresses(in.Fields.Emails, true); e != nil {
		return nil, e
	}
	if e := addresses(in.Fields.Phones, false); e != nil {
		return nil, e
	}
	if in.Fields.Emails != nil {
		body["emails"] = in.Fields.Emails
	}
	if in.Fields.Phones != nil {
		body["phones"] = in.Fields.Phones
	}
	b, _, e := c.call(ctx, "POST", "/api/v2/persons", nil, body)
	if e != nil {
		return nil, e
	}
	v, e := person(b)
	if e != nil {
		return nil, e
	}
	return &CreatePersonOut{Person: v}, nil
}
func (c *client) list_organizations(ctx context.Context, in *ListOrganizationsIn) (*ListOrganizationsOut, error) {
	rows, next, e := c.page(ctx, "/api/v2/organizations", in.Page)
	if e != nil {
		return nil, e
	}
	out := &ListOrganizationsOut{Organizations: []schema.Organization{}, NextCursor: next, HasMore: next != ""}
	seen := map[string]bool{}
	for _, b := range rows {
		v, e := organization(b)
		if e != nil {
			return nil, e
		}
		if seen[v.ID] {
			return nil, errors.New("Pipedrive returned duplicate organizations.")
		}
		seen[v.ID] = true
		out.Organizations = append(out.Organizations, v)
	}
	return out, nil
}
func (c *client) create_organization(ctx context.Context, in *CreateOrganizationIn) (*CreateOrganizationOut, error) {
	if !textOK(in.Name, 1024) {
		return nil, errors.New("Organization name is required and must be at most 1024 bytes.")
	}
	b, _, e := c.call(ctx, "POST", "/api/v2/organizations", nil, map[string]any{"name": in.Name})
	if e != nil {
		return nil, e
	}
	v, e := organization(b)
	if e != nil {
		return nil, e
	}
	return &CreateOrganizationOut{Organization: v}, nil
}
func (c *client) list_deals(ctx context.Context, in *ListDealsIn) (*ListDealsOut, error) {
	rows, next, e := c.page(ctx, "/api/v2/deals", in.Page)
	if e != nil {
		return nil, e
	}
	out := &ListDealsOut{Deals: []schema.Deal{}, NextCursor: next, HasMore: next != ""}
	seen := map[string]bool{}
	for _, b := range rows {
		v, e := deal(b)
		if e != nil {
			return nil, e
		}
		if seen[v.ID] {
			return nil, errors.New("Pipedrive returned duplicate deals.")
		}
		seen[v.ID] = true
		out.Deals = append(out.Deals, v)
	}
	return out, nil
}
func (c *client) create_deal(ctx context.Context, in *CreateDealIn) (*CreateDealOut, error) {
	if !textOK(in.Title, 1024) {
		return nil, errors.New("Deal title is required and must be at most 1024 bytes.")
	}
	body, e := dealBody(in.Title, in.Fields)
	if e != nil {
		return nil, e
	}
	b, _, e := c.call(ctx, "POST", "/api/v2/deals", nil, body)
	if e != nil {
		return nil, e
	}
	v, e := deal(b)
	if e != nil {
		return nil, e
	}
	return &CreateDealOut{Deal: v}, nil
}
func (c *client) update_deal(ctx context.Context, in *UpdateDealIn) (*UpdateDealOut, error) {
	if e := checkID(in.DealID); e != nil {
		return nil, e
	}
	body, e := dealBody(in.Title, in.Fields)
	if e != nil {
		return nil, e
	}
	b, _, e := c.call(ctx, "PATCH", "/api/v2/deals/"+in.DealID, nil, body)
	if e != nil {
		return nil, e
	}
	v, e := deal(b)
	if e != nil {
		return nil, e
	}
	if v.ID != in.DealID {
		return nil, errors.New("Pipedrive returned a different deal. Inspect the write before retrying.")
	}
	return &UpdateDealOut{Deal: v}, nil
}
