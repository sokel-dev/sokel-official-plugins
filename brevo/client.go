package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/sokel-dev/sokel-official-plugins/brevo/schema"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

type contactWire struct {
	ID           exactID        `json:"id"`
	Email        string         `json:"email"`
	EmailBlocked *bool          `json:"emailBlacklisted"`
	SMSBlocked   *bool          `json:"smsBlacklisted"`
	Lists        []exactID      `json:"listIds"`
	Attributes   map[string]any `json:"attributes"`
	Created      string         `json:"createdAt"`
	Modified     string         `json:"modifiedAt"`
}

func (w contactWire) output() (schema.Contact, error) {
	o := schema.Contact{ID: string(w.ID), Email: w.Email, ListIDs: []string{}, Attributes: w.Attributes, CreatedAt: w.Created, ModifiedAt: w.Modified}
	if w.ID == "" || w.EmailBlocked == nil || w.SMSBlocked == nil {
		return o, fmt.Errorf("Brevo contact is missing its ID or block status")
	}
	o.EmailBlacklisted = *w.EmailBlocked
	o.SmsBlacklisted = *w.SMSBlocked
	if o.Attributes == nil {
		o.Attributes = map[string]any{}
	}
	for _, v := range w.Lists {
		o.ListIDs = append(o.ListIDs, string(v))
	}
	return o, nil
}

type listWire struct {
	ID       exactID `json:"id"`
	Name     string  `json:"name"`
	FolderID exactID `json:"folderId"`
	Unique   *int64  `json:"uniqueSubscribers"`
}

func (w listWire) output() (schema.ContactList, error) {
	if w.ID == "" || w.Name == "" || w.FolderID == "" || w.Unique == nil || *w.Unique < 0 {
		return schema.ContactList{}, fmt.Errorf("Brevo returned an invalid contact list")
	}
	return schema.ContactList{ID: string(w.ID), Name: w.Name, FolderID: string(w.FolderID), UniqueSubscribers: *w.Unique}, nil
}
func (c *client) health_check(ctx context.Context, _ *HealthCheckIn) (*HealthCheckOut, error) {
	raw, e := c.request(ctx, "GET", "/v3/account", nil, nil, 200)
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	var w struct {
		Email string `json:"email"`
	}
	if decode(raw, &w) != nil || email(w.Email) != nil {
		return &HealthCheckOut{Message: "Brevo returned an invalid account response"}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Brevo account authentication succeeded; sending readiness was not tested"}, nil
}
func (c *client) list_contacts(ctx context.Context, in *ListContactsIn) (*ListContactsOut, error) {
	limit, offset, e := pagination(in.Page.Limit, in.Page.Offset, 1000)
	if e != nil {
		return nil, e
	}
	if e = timestamp(in.ModifiedSince); e != nil {
		return nil, e
	}
	q := url.Values{"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}, "sort": {"asc"}}
	if in.ModifiedSince != "" {
		q.Set("modifiedSince", in.ModifiedSince)
	}
	raw, e := c.request(ctx, "GET", "/v3/contacts", q, nil, 200)
	if e != nil {
		return nil, e
	}
	var w struct {
		Rows  []contactWire `json:"contacts"`
		Total *int          `json:"count"`
	}
	if decode(raw, &w) != nil || w.Rows == nil {
		return nil, fmt.Errorf("Brevo returned invalid contacts")
	}
	more, next, e := pageResult(w.Total, len(w.Rows), offset, limit)
	if e != nil {
		return nil, e
	}
	out := &ListContactsOut{Contacts: []schema.Contact{}, Total: *w.Total, HasMore: more, NextOffset: next}
	for _, v := range w.Rows {
		o, e := v.output()
		if e != nil {
			return nil, e
		}
		out.Contacts = append(out.Contacts, o)
	}
	return out, nil
}
func (c *client) get_contact(ctx context.Context, in *GetContactIn) (*GetContactOut, error) {
	if e := numericID(in.ContactID); e != nil {
		return nil, e
	}
	raw, e := c.request(ctx, "GET", "/v3/contacts/"+in.ContactID, url.Values{"identifierType": {"contact_id"}}, nil, 200)
	if e != nil {
		return nil, e
	}
	var w contactWire
	if decode(raw, &w) != nil || string(w.ID) != in.ContactID {
		return nil, fmt.Errorf("Brevo returned a mismatched contact")
	}
	o, e := w.output()
	if e != nil {
		return nil, e
	}
	return &GetContactOut{Contact: o}, nil
}
func attributes(v map[string]any) error {
	if len(v) > 200 {
		return fmt.Errorf("at most 200 contact attributes are supported")
	}
	for k, x := range v {
		if !regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`).MatchString(k) || k == "EMAIL" {
			return fmt.Errorf("attribute names must be uppercase; EMAIL updates are not supported")
		}
		switch a := x.(type) {
		case string:
			if len(a) > 32768 {
				return fmt.Errorf("attribute value is too long")
			}
		case bool:
		case json.Number:
			if _, e := a.Float64(); e != nil {
				return fmt.Errorf("invalid numeric attribute")
			}
		case float64:
			if math.IsNaN(a) || math.IsInf(a, 0) || math.Abs(a) > 9007199254740991 {
				return fmt.Errorf("numeric attributes must be finite and within the exact JSON integer range")
			}
		case int, int64:
		case []any:
			for _, z := range a {
				if _, ok := z.(string); !ok {
					return fmt.Errorf("attribute arrays must contain strings")
				}
			}
		case []string:
		default:
			return fmt.Errorf("attributes support only scalar values or arrays of strings")
		}
	}
	return nil
}
func contactBody(attrs map[string]any, ids []string) (map[string]any, error) {
	if e := attributes(attrs); e != nil {
		return nil, e
	}
	b := map[string]any{}
	if attrs != nil {
		b["attributes"] = attrs
	}
	if ids != nil {
		if len(ids) > 150 {
			return nil, fmt.Errorf("at most 150 list IDs are supported per request")
		}
		nums := []json.Number{}
		seen := map[string]bool{}
		for _, v := range ids {
			if e := numericID(v); e != nil {
				return nil, e
			}
			if seen[v] {
				return nil, fmt.Errorf("duplicate list ID")
			}
			seen[v] = true
			nums = append(nums, json.Number(v))
		}
		b["listIds"] = nums
	}
	return b, nil
}
func (c *client) create_contact(ctx context.Context, in *CreateContactIn) (*CreateContactOut, error) {
	if e := email(in.Email); e != nil {
		return nil, e
	}
	if in.EmailStatus != "blocked" && in.EmailStatus != "allowed" {
		return nil, fmt.Errorf("explicit new contact email_status must be blocked or allowed")
	}
	b, e := contactBody(in.Attributes, in.ListIDs)
	if e != nil {
		return nil, e
	}
	b["email"] = in.Email
	b["emailBlacklisted"] = in.EmailStatus == "blocked"
	raw, e := c.request(ctx, "POST", "/v3/contacts", nil, b, 201)
	if e != nil {
		return nil, e
	}
	var w struct {
		ID exactID `json:"id"`
	}
	if decode(raw, &w) != nil || w.ID == "" {
		return nil, fmt.Errorf("Brevo create response is missing a valid contact ID; verify before retrying")
	}
	return &CreateContactOut{ContactID: string(w.ID)}, nil
}
func (c *client) update_contact(ctx context.Context, in *UpdateContactIn) (*UpdateContactOut, error) {
	if e := numericID(in.ContactID); e != nil {
		return nil, e
	}
	b, e := contactBody(in.Attributes, in.ListIDs)
	if e != nil {
		return nil, e
	}
	if in.Options.EmailBlacklisted != nil {
		b["emailBlacklisted"] = *in.Options.EmailBlacklisted
	}
	if len(in.Attributes) == 0 && len(in.ListIDs) == 0 && in.Options.EmailBlacklisted == nil {
		return nil, fmt.Errorf("provide an attribute, list membership or explicit block status to update")
	}
	_, e = c.request(ctx, "PUT", "/v3/contacts/"+in.ContactID, url.Values{"identifierType": {"contact_id"}}, b, 204)
	if e != nil {
		return nil, e
	}
	return &UpdateContactOut{Accepted: true}, nil
}
func (c *client) list_lists(ctx context.Context, in *ListListsIn) (*ListListsOut, error) {
	limit, offset, e := pagination(in.Page.Limit, in.Page.Offset, 50)
	if e != nil {
		return nil, e
	}
	raw, e := c.request(ctx, "GET", "/v3/contacts/lists", url.Values{"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}, "sort": {"asc"}}, nil, 200)
	if e != nil {
		return nil, e
	}
	var w struct {
		Rows  []listWire `json:"lists"`
		Total *int       `json:"count"`
	}
	if decode(raw, &w) != nil || w.Rows == nil {
		return nil, fmt.Errorf("Brevo returned invalid lists")
	}
	more, next, e := pageResult(w.Total, len(w.Rows), offset, limit)
	if e != nil {
		return nil, e
	}
	out := &ListListsOut{Lists: []schema.ContactList{}, Total: *w.Total, HasMore: more, NextOffset: next}
	for _, v := range w.Rows {
		o, e := v.output()
		if e != nil {
			return nil, e
		}
		out.Lists = append(out.Lists, o)
	}
	return out, nil
}
func (c *client) get_list(ctx context.Context, in *GetListIn) (*GetListOut, error) {
	if e := numericID(in.ListID); e != nil {
		return nil, e
	}
	raw, e := c.request(ctx, "GET", "/v3/contacts/lists/"+in.ListID, nil, nil, 200)
	if e != nil {
		return nil, e
	}
	var w listWire
	if decode(raw, &w) != nil || string(w.ID) != in.ListID {
		return nil, fmt.Errorf("Brevo returned a mismatched list")
	}
	o, e := w.output()
	if e != nil {
		return nil, e
	}
	return &GetListOut{List: o}, nil
}
func (c *client) send_email(ctx context.Context, in *SendEmailIn) (*SendEmailOut, error) {
	if e := email(in.SenderEmail); e != nil {
		return nil, e
	}
	if len(in.To) < 1 || len(in.To) > 99 {
		return nil, fmt.Errorf("provide 1..99 recipients")
	}
	if strings.TrimSpace(in.Subject) == "" || len(in.Subject) > 998 || strings.ContainsAny(in.Subject, "\r\n\x00") {
		return nil, fmt.Errorf("subject must be 1..998 bytes without line breaks")
	}
	if in.Body == "" || len(in.Body) > 512*1024 {
		return nil, fmt.Errorf("body must be 1..524288 bytes")
	}
	if len(in.SenderName) > 256 || strings.ContainsAny(in.SenderName, "\r\n\x00") {
		return nil, fmt.Errorf("invalid sender display name")
	}
	sender := map[string]string{"email": in.SenderEmail}
	if in.SenderName != "" {
		sender["name"] = in.SenderName
	}
	recipients := []map[string]string{}
	seen := map[string]bool{}
	for _, r := range in.To {
		if e := email(r.Email); e != nil {
			return nil, e
		}
		if len(r.Name) > 256 || strings.ContainsAny(r.Name, "\r\n\x00") {
			return nil, fmt.Errorf("invalid recipient display name")
		}
		key := strings.ToLower(r.Email)
		if seen[key] {
			return nil, fmt.Errorf("duplicate recipient")
		}
		seen[key] = true
		item := map[string]string{"email": r.Email}
		if r.Name != "" {
			item["name"] = r.Name
		}
		recipients = append(recipients, item)
	}
	b := map[string]any{"sender": sender, "to": recipients, "subject": in.Subject}
	switch in.ContentType {
	case "text":
		b["textContent"] = in.Body
	case "html":
		b["htmlContent"] = in.Body
	default:
		return nil, fmt.Errorf("body format must be text or html")
	}
	raw, e := c.request(ctx, "POST", "/v3/smtp/email", nil, b, 201)
	if e != nil {
		return nil, e
	}
	var w struct {
		ID string `json:"messageId"`
	}
	if decode(raw, &w) != nil || strings.TrimSpace(w.ID) == "" || len(w.ID) > 512 || strings.ContainsAny(w.ID, "\r\n\x00") {
		return nil, fmt.Errorf("Brevo returned no valid message ID; inspect delivery logs before retrying")
	}
	return &SendEmailOut{MessageID: w.ID, Accepted: true}, nil
}
