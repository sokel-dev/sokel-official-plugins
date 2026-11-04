package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-official-plugins/pipedrive/schema"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var numberID = regexp.MustCompile(`^[1-9][0-9]{0,39}$`)
var decimal = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?$`)
var moneyNumber = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

func checkID(s string) error {
	if !numberID.MatchString(s) {
		return errors.New("Resource ID must be a positive decimal string.")
	}
	return nil
}
func textOK(s string, max int) bool {
	return strings.TrimSpace(s) != "" && len(s) <= max && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}
func cursorOK(s string) bool {
	return len(s) <= 8192 && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

type exactID string

func (v *exactID) UnmarshalJSON(b []byte) error {
	var s string
	if len(b) > 0 && b[0] == '"' {
		if json.Unmarshal(b, &s) != nil {
			return errors.New("Invalid identifier.")
		}
	} else {
		s = string(b)
	}
	if checkID(s) != nil {
		return errors.New("Invalid identifier.")
	}
	*v = exactID(s)
	return nil
}

type exactMoney string

func (v *exactMoney) UnmarshalJSON(b []byte) error {
	s := string(b)
	if len(b) > 0 && b[0] == '"' {
		if json.Unmarshal(b, &s) != nil {
			return errors.New("Invalid monetary value.")
		}
	}
	if len(s) > 128 || !moneyNumber.MatchString(s) {
		return errors.New("Invalid monetary value.")
	}
	*v = exactMoney(s)
	return nil
}
func idPointer(v *exactID) *string {
	if v == nil {
		return nil
	}
	s := string(*v)
	return &s
}
func (c *client) call(ctx context.Context, method, path string, q url.Values, input any) (json.RawMessage, json.RawMessage, error) {
	var env struct {
		Success    *bool           `json:"success"`
		Data       json.RawMessage `json:"data"`
		Additional json.RawMessage `json:"additional_data"`
	}
	if e := c.json(ctx, method, path, q, input, &env); e != nil {
		return nil, nil, e
	}
	if env.Success == nil || !*env.Success {
		return nil, nil, errors.New("Pipedrive rejected the request. Check permissions, required fields and account limits; inspect writes before retrying.")
	}
	if len(env.Data) == 0 || string(env.Data) == "null" {
		return nil, nil, errors.New("Pipedrive returned no result data.")
	}
	return env.Data, env.Additional, nil
}
func person(b []byte) (schema.Person, error) {
	var w struct {
		ID     exactID          `json:"id"`
		Name   string           `json:"name"`
		OrgID  *exactID         `json:"org_id"`
		Emails []schema.Address `json:"emails"`
		Phones []schema.Address `json:"phones"`
	}
	if e := decodeJSON(b, &w); e != nil {
		return schema.Person{}, e
	}
	if w.ID == "" || w.Name == "" {
		return schema.Person{}, errors.New("Pipedrive returned incomplete person data.")
	}
	if w.Emails == nil {
		w.Emails = []schema.Address{}
	}
	if w.Phones == nil {
		w.Phones = []schema.Address{}
	}
	return schema.Person{ID: string(w.ID), Name: w.Name, OrgID: idPointer(w.OrgID), Emails: w.Emails, Phones: w.Phones}, nil
}
func organization(b []byte) (schema.Organization, error) {
	var w struct {
		ID   exactID `json:"id"`
		Name string  `json:"name"`
	}
	if e := decodeJSON(b, &w); e != nil {
		return schema.Organization{}, e
	}
	if w.ID == "" || w.Name == "" {
		return schema.Organization{}, errors.New("Pipedrive returned incomplete organization data.")
	}
	return schema.Organization{ID: string(w.ID), Name: w.Name}, nil
}
func deal(b []byte) (schema.Deal, error) {
	var w struct {
		ID       exactID     `json:"id"`
		Title    string      `json:"title"`
		Status   string      `json:"status"`
		Value    *exactMoney `json:"value"`
		Currency string      `json:"currency"`
		PersonID *exactID    `json:"person_id"`
		OrgID    *exactID    `json:"org_id"`
	}
	if e := decodeJSON(b, &w); e != nil {
		return schema.Deal{}, e
	}
	if w.ID == "" || w.Title == "" || w.Status == "" || w.Value == nil || w.Currency == "" {
		return schema.Deal{}, errors.New("Pipedrive returned incomplete deal data.")
	}
	return schema.Deal{ID: string(w.ID), Title: w.Title, Status: w.Status, Value: string(*w.Value), Currency: w.Currency, PersonID: idPointer(w.PersonID), OrgID: idPointer(w.OrgID)}, nil
}
func (c *client) health_check(ctx context.Context, _ *HealthCheckIn) (*HealthCheckOut, error) {
	b, _, e := c.call(ctx, "GET", "/api/v1/users/me", nil, nil)
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	var v struct {
		ID    exactID `json:"id"`
		Name  string  `json:"name"`
		Email string  `json:"email"`
	}
	if decodeJSON(b, &v) != nil || v.ID == "" || v.Name == "" || v.Email == "" {
		return &HealthCheckOut{Message: "Pipedrive did not return a valid current user."}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Pipedrive credentials are usable for the current user."}, nil
}
func (c *client) page(ctx context.Context, path string, p schema.Page) ([]json.RawMessage, string, error) {
	limit := 100
	if p.Limit != nil {
		limit = *p.Limit
	}
	if limit < 1 || limit > 500 {
		return nil, "", errors.New("Page size must be 1..500.")
	}
	q := url.Values{"limit": {strconv.Itoa(limit)}}
	before := ""
	if p.Cursor != nil {
		before = *p.Cursor
		if before == "" || !cursorOK(before) {
			return nil, "", errors.New("Invalid pagination cursor.")
		}
		q.Set("cursor", before)
	}
	raw, additional, e := c.call(ctx, "GET", path, q, nil)
	if e != nil {
		return nil, "", e
	}
	var rows []json.RawMessage
	if decodeJSON(raw, &rows) != nil || rows == nil || len(rows) > limit {
		return nil, "", errors.New("Pipedrive returned an invalid collection.")
	}
	var meta map[string]json.RawMessage
	if decodeJSON(additional, &meta) != nil || len(meta["next_cursor"]) == 0 {
		return nil, "", errors.New("Pipedrive returned no pagination metadata.")
	}
	next := ""
	if string(meta["next_cursor"]) != "null" {
		if decodeJSON(meta["next_cursor"], &next) != nil || next == "" || next == before || !cursorOK(next) {
			return nil, "", errors.New("Pipedrive returned an invalid or repeated cursor.")
		}
	}
	if next != "" && len(rows) == 0 {
		return nil, "", errors.New("Pipedrive returned an empty page with more results.")
	}
	return rows, next, nil
}
func addresses(rows []schema.Address, email bool) error {
	if len(rows) > 100 {
		return errors.New("At most 100 email addresses or phone numbers are accepted.")
	}
	primary := 0
	for _, r := range rows {
		if !textOK(r.Value, 320) {
			return errors.New("Email addresses and phone numbers must be nonempty and at most 320 bytes.")
		}
		if email {
			a, e := mail.ParseAddress(r.Value)
			if e != nil || a.Address != r.Value {
				return errors.New("Provide a plain email address without a display name.")
			}
		}
		if r.Primary != nil && *r.Primary {
			primary++
		}
		if r.Label != nil && (len(*r.Label) > 128 || !utf8.ValidString(*r.Label)) {
			return errors.New("Address labels must be valid UTF-8 and at most 128 bytes.")
		}
	}
	if primary > 1 {
		return errors.New("Only one email or phone may be primary.")
	}
	return nil
}
func dealBody(title string, f schema.DealFields) (map[string]any, error) {
	v := map[string]any{}
	if title != "" {
		if !textOK(title, 1024) {
			return nil, errors.New("Deal title must be at most 1024 bytes and not blank.")
		}
		v["title"] = title
	}
	if f.Value != nil {
		if len(*f.Value) > 32 || !decimal.MatchString(*f.Value) {
			return nil, errors.New("Value must be a nonnegative decimal string of at most 32 bytes, without an exponent.")
		}
		v["value"] = json.Number(*f.Value)
	}
	if f.Currency != nil {
		if len(*f.Currency) != 3 || strings.IndexFunc(*f.Currency, func(r rune) bool { return r < 'A' || r > 'Z' }) >= 0 {
			return nil, errors.New("Currency must be a three-letter uppercase code.")
		}
		v["currency"] = *f.Currency
	}
	for k, id := range map[string]*string{"person_id": f.PersonID, "org_id": f.OrgID} {
		if id != nil {
			if e := checkID(*id); e != nil {
				return nil, e
			}
			v[k] = json.Number(*id)
		}
	}
	if f.Status != nil {
		switch *f.Status {
		case "open", "won", "lost":
			v["status"] = *f.Status
		default:
			return nil, errors.New("Deal status must be open, won or lost.")
		}
	}
	if len(v) == 0 {
		return nil, errors.New("Provide at least one field to update.")
	}
	return v, nil
}
