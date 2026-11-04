package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sokel-dev/sokel-official-plugins/resend/schema"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var tagPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,256}$`)

func validID(id string) error {
	if !uuidPattern.MatchString(id) {
		return errors.New("Provide a Resend UUID, not a URL or path.")
	}
	return nil
}
func (c *client) authorization() (string, error) {
	if !safeToken(c.cred.APIKey) {
		return "", errors.New("Provide a valid Resend API key in credentials.")
	}
	return "Bearer " + c.cred.APIKey, nil
}
func address(s string) error {
	if !utf8.ValidString(s) || strings.ContainsAny(s, "\r\n\x00") || len(s) > 512 {
		return errors.New("Provide a valid single email address.")
	}
	if _, e := mail.ParseAddress(s); e != nil {
		return errors.New("Provide a valid single email address.")
	}
	return nil
}
func validateSend(in *SendEmailIn) error {
	e := in.Email
	if err := address(e.From); err != nil {
		return err
	}
	if len(e.To) < 1 || len(e.To) > 50 || len(e.CC) > 50 || len(e.BCC) > 50 || len(e.ReplyTo) > 50 {
		return errors.New("Provide 1 to 50 recipients and at most 50 addresses in each optional recipient field.")
	}
	for _, list := range [][]string{e.To, e.CC, e.BCC, e.ReplyTo} {
		for _, s := range list {
			if err := address(s); err != nil {
				return err
			}
		}
	}
	if !utf8.ValidString(e.Subject) || strings.ContainsAny(e.Subject, "\r\n\x00") || len(e.Subject) > 998 {
		return errors.New("Subject must be one UTF-8 line of at most 998 bytes.")
	}
	content := false
	for _, b := range []*string{e.HTML, e.Text} {
		if b != nil {
			if !utf8.ValidString(*b) || strings.ContainsRune(*b, 0) {
				return errors.New("Email content must be valid UTF-8 without NUL characters.")
			}
			content = content || *b != ""
		}
	}
	if !content {
		return errors.New("Provide a nonempty HTML or plain text body.")
	}
	if e.ScheduledAt != nil {
		if _, err := time.Parse(time.RFC3339, *e.ScheduledAt); err != nil {
			return errors.New("Scheduled time must use RFC3339 with an explicit timezone.")
		}
	}
	for _, tag := range e.Tags {
		if !tagPattern.MatchString(tag.Name) || !tagPattern.MatchString(tag.Value) {
			return errors.New("Tag names and values must contain 1 to 256 ASCII letters, digits, underscores or hyphens.")
		}
	}
	if in.IdempotencyKey != "" && (!safeToken(in.IdempotencyKey) || len(in.IdempotencyKey) > 256) {
		return errors.New("Idempotency key must contain 1 to 256 visible ASCII characters.")
	}
	return nil
}
func (c *client) sendEmail(ctx context.Context, in *SendEmailIn) (*SendEmailOut, error) {
	if e := validateSend(in); e != nil {
		return nil, e
	}
	b, e := c.request(ctx, "POST", "/emails", nil, in.Email, in.IdempotencyKey)
	if e != nil {
		return nil, e
	}
	var r struct {
		ID string `json:"id"`
	}
	if e = decodeJSON(b, &r); e != nil {
		return nil, e
	}
	if validID(r.ID) != nil {
		return nil, errors.New("Resend did not return a valid email ID. Inspect the send result before retrying.")
	}
	return &SendEmailOut{EmailID: r.ID}, nil
}
func checkEmail(id, from, created string, to []string) error {
	if validID(id) != nil || from == "" || created == "" || len(to) == 0 {
		return errors.New("Resend returned incomplete email metadata.")
	}
	return nil
}
func (c *client) getEmail(ctx context.Context, in *GetEmailIn) (*GetEmailOut, error) {
	if e := validID(in.EmailID); e != nil {
		return nil, e
	}
	var r schema.SentEmail
	if e := c.json(ctx, "GET", "/emails/"+in.EmailID, nil, nil, &r); e != nil {
		return nil, e
	}
	if r.ID != in.EmailID {
		return nil, errors.New("Resend returned a different email.")
	}
	if e := checkEmail(r.ID, r.From, r.CreatedAt, r.To); e != nil {
		return nil, e
	}
	return &GetEmailOut{Email: r}, nil
}
func (c *client) cancelEmail(ctx context.Context, in *CancelEmailIn) (*CancelEmailOut, error) {
	if e := validID(in.EmailID); e != nil {
		return nil, e
	}
	var r struct {
		ID string `json:"id"`
	}
	if e := c.json(ctx, "POST", "/emails/"+in.EmailID+"/cancel", nil, nil, &r); e != nil {
		return nil, e
	}
	if r.ID != in.EmailID {
		return nil, errors.New("Resend returned a different cancellation ID.")
	}
	return &CancelEmailOut{EmailID: r.ID}, nil
}
func (c *client) getReceivedEmail(ctx context.Context, in *GetReceivedEmailIn) (*GetReceivedEmailOut, error) {
	if e := validID(in.EmailID); e != nil {
		return nil, e
	}
	var r schema.ReceivedEmail
	if e := c.json(ctx, "GET", "/emails/receiving/"+in.EmailID, url.Values{"html_format": {"cid"}}, nil, &r); e != nil {
		return nil, e
	}
	if r.ID != in.EmailID {
		return nil, errors.New("Resend returned a different received email.")
	}
	if e := checkEmail(r.ID, r.From, r.CreatedAt, r.To); e != nil {
		return nil, e
	}
	if r.Attachments == nil {
		r.Attachments = []schema.Attachment{}
	}
	for _, a := range r.Attachments {
		if validID(a.ID) != nil || a.Size < 0 {
			return nil, errors.New("Resend returned invalid attachment metadata.")
		}
	}
	return &GetReceivedEmailOut{Email: r}, nil
}
func pageQuery(p schema.PageOptions) (url.Values, int, error) {
	n := 20
	if p.Limit != nil {
		n = *p.Limit
	}
	if n < 1 || n > 100 {
		return nil, 0, errors.New("Page size must be 1 to 100.")
	}
	q := url.Values{"limit": {strconv.Itoa(n)}}
	if p.After != nil {
		if e := validID(*p.After); e != nil {
			return nil, 0, e
		}
		q.Set("after", *p.After)
	}
	return q, n, nil
}

type pageEnvelope struct {
	Data    []json.RawMessage `json:"data"`
	HasMore *bool             `json:"has_more"`
}

func (c *client) page(ctx context.Context, path string, p schema.PageOptions) (pageEnvelope, error) {
	q, limit, e := pageQuery(p)
	if e != nil {
		return pageEnvelope{}, e
	}
	var r pageEnvelope
	if e = c.json(ctx, "GET", path, q, nil, &r); e != nil {
		return r, e
	}
	if r.Data == nil || r.HasMore == nil || len(r.Data) > limit || (*r.HasMore && len(r.Data) == 0) {
		return r, errors.New("Resend returned missing or inconsistent pagination metadata.")
	}
	return r, nil
}
func (c *client) emails(ctx context.Context, path string, p schema.PageOptions) ([]schema.EmailSummary, bool, string, error) {
	r, e := c.page(ctx, path, p)
	if e != nil {
		return nil, false, "", e
	}
	items := make([]schema.EmailSummary, 0, len(r.Data))
	for _, b := range r.Data {
		var x schema.EmailSummary
		if e := decodeJSON(b, &x); e != nil {
			return nil, false, "", e
		}
		if e := checkEmail(x.ID, x.From, x.CreatedAt, x.To); e != nil {
			return nil, false, "", e
		}
		items = append(items, x)
	}
	next := ""
	if *r.HasMore {
		next = items[len(items)-1].ID
		if p.After != nil && next == *p.After {
			return nil, false, "", errors.New("Resend pagination did not advance.")
		}
	}
	return items, *r.HasMore, next, nil
}
func (c *client) listEmails(ctx context.Context, in *ListEmailsIn) (*ListEmailsOut, error) {
	items, more, next, e := c.emails(ctx, "/emails", in.Page)
	if e != nil {
		return nil, e
	}
	return &ListEmailsOut{Emails: items, HasMore: more, NextAfter: next}, nil
}
func (c *client) listReceivedEmails(ctx context.Context, in *ListReceivedEmailsIn) (*ListReceivedEmailsOut, error) {
	items, more, next, e := c.emails(ctx, "/emails/receiving", in.Page)
	if e != nil {
		return nil, e
	}
	return &ListReceivedEmailsOut{Emails: items, HasMore: more, NextAfter: next}, nil
}
func checkDomain(x schema.Domain) error {
	if validID(x.ID) != nil || x.Name == "" || x.Status == "" {
		return errors.New("Resend returned incomplete domain metadata.")
	}
	return nil
}
func (c *client) getDomain(ctx context.Context, in *GetDomainIn) (*GetDomainOut, error) {
	if e := validID(in.DomainID); e != nil {
		return nil, e
	}
	var r struct {
		schema.Domain
		Records []schema.DNSRecord `json:"records"`
	}
	if e := c.json(ctx, "GET", "/domains/"+in.DomainID, nil, nil, &r); e != nil {
		return nil, e
	}
	if r.ID != in.DomainID {
		return nil, errors.New("Resend returned a different domain.")
	}
	if e := checkDomain(r.Domain); e != nil {
		return nil, e
	}
	if r.Records == nil {
		r.Records = []schema.DNSRecord{}
	}
	return &GetDomainOut{Domain: r.Domain, Records: r.Records}, nil
}
func (c *client) listDomains(ctx context.Context, in *ListDomainsIn) (*ListDomainsOut, error) {
	r, e := c.page(ctx, "/domains", in.Page)
	if e != nil {
		return nil, e
	}
	out := &ListDomainsOut{Domains: []schema.Domain{}, HasMore: *r.HasMore}
	for _, b := range r.Data {
		var d schema.Domain
		if e := decodeJSON(b, &d); e != nil {
			return nil, e
		}
		if e := checkDomain(d); e != nil {
			return nil, e
		}
		out.Domains = append(out.Domains, d)
	}
	if out.HasMore {
		out.NextAfter = out.Domains[len(out.Domains)-1].ID
		if in.Page.After != nil && out.NextAfter == *in.Page.After {
			return nil, errors.New("Resend pagination did not advance.")
		}
	}
	return out, nil
}
